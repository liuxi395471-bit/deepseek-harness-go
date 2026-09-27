package a2a

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"deepseek-harness-go/internal/storage"
)

func echoAgent(name string) *Agent {
	return &Agent{
		Name: name, Capability: "echo",
		Handle: func(ctx context.Context, t Task) (Result, error) {
			return Result{TaskID: t.ID, Agent: name, Output: "echo:" + t.Input, Updates: map[string]any{"echoed": t.Input}}, nil
		},
	}
}

func researchAgent(name string) *Agent {
	return &Agent{
		Name: name, Capability: "research",
		Handle: func(ctx context.Context, t Task) (Result, error) {
			return Result{TaskID: t.ID, Agent: name, Output: "research:" + t.Input, Updates: map[string]any{"research_result": "found-it"}}, nil
		},
	}
}

func failingAgent(name string) *Agent {
	return &Agent{
		Name: name, Capability: "fail",
		Handle: func(ctx context.Context, t Task) (Result, error) {
			return Result{}, errors.New("agent failed")
		},
	}
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(echoAgent("e1")); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := r.Register(echoAgent("e1")); err == nil {
		t.Error("duplicate should fail")
	}
	got, ok := r.Lookup("echo")
	if !ok || got.Name != "e1" {
		t.Errorf("lookup = %+v, %v", got, ok)
	}
	r.Unregister("e1")
	if _, ok := r.Lookup("echo"); ok {
		t.Error("unregister should remove")
	}
}

func TestRegistry_InvalidAgent(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(nil); err == nil {
		t.Error("nil agent should error")
	}
	if err := r.Register(&Agent{Name: "x"}); err == nil {
		t.Error("missing capability should error")
	}
	if err := r.Register(&Agent{Name: "x", Capability: "y"}); err == nil {
		t.Error("nil handle should error")
	}
}

func TestOrchestrator_RunAndShareState(t *testing.T) {
	store := storage.NewMemoryStorage()
	reg := NewRegistry()
	reg.Register(researchAgent("r1"))
	reg.Register(echoAgent("e1"))

	orch := NewOrchestrator(reg, store)
	res, err := orch.Run(context.Background(), OrchestrationRequest{
		Steps: []Step{
			{Capability: "research", Input: "topic A", SharedKey: "topic"},
			{Capability: "echo", Input: "write about the prior result"},
			{Capability: "echo", Input: "another echo"},
		},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(res.Outputs) != 3 {
		t.Errorf("outputs = %d", len(res.Outputs))
	}
	if !strings.Contains(res.Outputs[2].Output, "echo:") {
		t.Errorf("echo saw: %q", res.Outputs[2].Output)
	}
	// Shared state 持久化（每个 echo 都更新 echoed 键 → 最后一次胜出）
	shared := orch.SharedState()
	if shared["echoed"] != "another echo" {
		t.Errorf("shared[echoed] = %v", shared["echoed"])
	}
	if shared["research_result"] != "found-it" {
		t.Errorf("shared[research_result] = %v", shared["research_result"])
	}
}

func TestOrchestrator_FailFast(t *testing.T) {
	reg := NewRegistry()
	reg.Register(echoAgent("e1"))
	reg.Register(failingAgent("f1"))
	orch := NewOrchestrator(reg, storage.NewMemoryStorage())
	_, err := orch.Run(context.Background(), OrchestrationRequest{
		Steps: []Step{
			{Capability: "echo", Input: "hi"},
			{Capability: "fail", Input: "boom"},
		},
	})
	if err == nil {
		t.Fatal("expected error from failing step")
	}
	if !strings.Contains(err.Error(), "agent failed") {
		t.Errorf("err = %v", err)
	}
}

func TestOrchestrator_MissingCapability(t *testing.T) {
	orch := NewOrchestrator(NewRegistry(), nil)
	_, err := orch.Run(context.Background(), OrchestrationRequest{
		Steps: []Step{{Capability: "nope", Input: "x"}},
	})
	if err == nil || !strings.Contains(err.Error(), "no agent for capability") {
		t.Errorf("err = %v", err)
	}
}

func TestOrchestrator_EmptySteps(t *testing.T) {
	orch := NewOrchestrator(NewRegistry(), nil)
	_, err := orch.Run(context.Background(), OrchestrationRequest{})
	if err == nil || !strings.Contains(err.Error(), "empty steps") {
		t.Errorf("err = %v", err)
	}
}

// 跨 agent 并发调用（虽然 v7.0 顺序执行；这里验证并发时 mutex 安全）。
func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	r.Register(echoAgent("e1"))
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = r.Lookup("echo")
		}()
	}
	wg.Wait()
}
