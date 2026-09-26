package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"deepseek-harness-go/internal/agent"
	"deepseek-harness-go/internal/llm"
	"deepseek-harness-go/internal/task"
)

func jsonRaw(s string) json.RawMessage { return json.RawMessage(s) }

func jsonDecodePayload(raw json.RawMessage, into any) error {
	return json.Unmarshal(raw, into)
}

// fakeRunnerTask 是 task 集成测试专用的 fakeRunner：
//
//   - 收到 RunStream 时立即完成，返回 agent.RunResult{StopReason:"no_tool_calls"} + 假 usage。
//   - prompt 含 "SLEEP" 时延迟 60ms 再返回，便于 cancel 测试。
type fakeRunnerTask struct {
	calls int
}

func (f *fakeRunnerTask) RunStream(ctx context.Context, prompt string, sid string) (<-chan agent.Event, <-chan agent.RunResult) {
	f.calls++
	ev := make(chan agent.Event, 4)
	res := make(chan agent.RunResult, 1)
	go func() {
		defer close(ev)
		ev <- agent.AssistantMessage{Content: "ok"}
		ev <- agent.LoopDone{Rounds: 1}
	}()
	if strings.Contains(prompt, "SLEEP") {
		go func() {
			select {
			case <-ctx.Done():
				res <- agent.RunResult{StopReason: "canceled", Error: ctx.Err()}
				return
			case <-time.After(60 * time.Millisecond):
			}
			res <- agent.RunResult{
				StopReason: "no_tool_calls",
				Usage:      llm.Usage{PromptTokens: 3, CompletionTokens: 5, TotalTokens: 8},
			}
		}()
	} else {
		res <- agent.RunResult{
			StopReason: "no_tool_calls",
			Usage:      llm.Usage{PromptTokens: 11, CompletionTokens: 7, TotalTokens: 18},
		}
	}
	return ev, res
}

// captureEvents 收集 handler 输出的全部 GatewayEvent。
func captureEvents(h SourceHandler, req GatewayRequest) ([]GatewayEvent, error) {
	out := make(chan GatewayEvent, 16)
	errCh := make(chan error, 1)
	go func() {
		errCh <- h(context.Background(), req, out)
		close(out)
	}()
	var evs []GatewayEvent
	for ev := range out {
		evs = append(evs, ev)
	}
	return evs, <-errCh
}

func newTaskHandlers(exec task.Executor) *GatewayHandlers {
	h := &GatewayHandlers{
		Runner:        nil,
		TasksExecutor: exec,
	}
	return h
}

func TestGateway_TaskHandlers_Lifecycle(t *testing.T) {
	store := task.NewMemoryStore()
	fake := &fakeRunnerTask{}
	exec := task.NewLoopExecutor(store, fake)
	defer exec.Close(context.Background())

	h := newTaskHandlers(exec)
	ctx := context.Background()

	// submit
	submitEvs, err := captureEvents(h.handleTaskSubmit, GatewayRequest{
		Source: "task.submit",
		Params: jsonRaw(`{"input":"hello","code":"T-1","owner":"alice"}`),
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if len(submitEvs) != 1 || submitEvs[0].Type != "final" {
		t.Fatalf("submit events = %+v", submitEvs)
	}
	var got struct {
		Task *task.Task `json:"task"`
	}
	if err := jsonDecodePayload(submitEvs[0].Payload, &got); err != nil {
		t.Fatalf("decode submit: %v", err)
	}
	if got.Task == nil || got.Task.ID == "" {
		t.Fatalf("submit payload missing task: %+v", got)
	}
	id := got.Task.ID

	// 等到完成
	deadline := time.Now().Add(500 * time.Millisecond)
	var final *task.Task
	for time.Now().Before(deadline) {
		got, _ := exec.Get(ctx, id)
		if got.State == task.StateCompleted {
			final = got
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if final == nil {
		t.Fatal("task did not complete")
	}
	if final.Usage.TotalTokens != 18 {
		t.Errorf("usage = %+v, want total=18", final.Usage)
	}

	// get
	getEvs, err := captureEvents(h.handleTaskGet, GatewayRequest{
		Source: "task.get",
		Params: jsonRaw(`{"id":"` + id + `"}`),
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(getEvs) != 1 || getEvs[0].Type != "final" {
		t.Errorf("get events = %+v", getEvs)
	}

	// get missing → error 帧
	missEvs, err := captureEvents(h.handleTaskGet, GatewayRequest{
		Source: "task.get",
		Params: jsonRaw(`{"id":"missing"}`),
	})
	if err != nil {
		t.Fatalf("get miss: %v", err)
	}
	if missEvs[0].Type != "error" {
		t.Errorf("missing → %+v, want error", missEvs[0])
	}

	// list（按 states）
	exec.Submit(ctx, task.SubmitRequest{Input: "second"})
	listEvs, err := captureEvents(h.handleTaskList, GatewayRequest{
		Source: "task.list",
		Params: jsonRaw(`{"owner":"alice"}`),
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listEvs) != 1 || listEvs[0].Type != "final" {
		t.Errorf("list events = %+v", listEvs)
	}
}

func TestGateway_TaskHandlers_NilExecutor(t *testing.T) {
	h := &GatewayHandlers{} // 无 executor
	_, err := captureEvents(h.handleTaskSubmit, GatewayRequest{
		Source: "task.submit",
		Params: jsonRaw(`{"input":"x"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "executor unavailable") {
		t.Errorf("nil exec submit → %v", err)
	}
}

func TestGateway_TaskHandlers_Cancel(t *testing.T) {
	store := task.NewMemoryStore()
	fake := &fakeRunnerTask{}
	exec := task.NewLoopExecutor(store, fake)
	defer exec.Close(context.Background())

	// 提交一个长任务
	sub, err := exec.Submit(context.Background(), task.SubmitRequest{Input: "SLEEP now"})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	// 切到 Running 后取消
	time.Sleep(5 * time.Millisecond)
	h := newTaskHandlers(exec)
	evs, err := captureEvents(h.handleTaskCancel, GatewayRequest{
		Source: "task.cancel",
		Params: jsonRaw(`{"id":"` + sub.ID + `"}`),
	})
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if evs[0].Type != "final" {
		t.Errorf("cancel events = %+v", evs)
	}

	// 等待变 Canceled
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		got, _ := exec.Get(context.Background(), sub.ID)
		if got.State == task.StateCanceled {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	got, _ := exec.Get(context.Background(), sub.ID)
	t.Errorf("task did not reach Canceled; state=%v", got.State)
}
