package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"deepseek-harness-go/internal/llm"
)

// sseFrames 把对象列表编码为 Gemini alt=sse 响应。
func sseFrames(frames ...any) string {
	var b strings.Builder
	for _, f := range frames {
		j, _ := json.Marshal(f)
		b.WriteString("data: " + string(j) + "\n\n")
	}
	return b.String()
}

func mockGemini(t *testing.T, body string, capture *generateRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, ":generateContent") && !strings.Contains(r.URL.Path, ":streamGenerateContent") {
			http.Error(w, "not found", 404)
			return
		}
		var req generateRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if capture != nil {
			*capture = req
		}
		if r.URL.Query().Get("alt") == "sse" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(body))
			return
		}
		// 非流式请求：fixture 以 SSE 编码书写，这里剥出第一个
		// data 帧的 payload 作为纯 JSON 响应。
		w.Header().Set("Content-Type", "application/json")
		for _, line := range strings.Split(body, "\n") {
			if payload, ok := strings.CutPrefix(line, "data: "); ok && payload != "" {
				_, _ = w.Write([]byte(payload))
				return
			}
		}
		_, _ = w.Write([]byte(body))
	}))
}

func TestChatRoundTrip(t *testing.T) {
	var captured generateRequest
	srv := mockGemini(t, sseFrames(generateResponse{
		Candidates: []candidate{{
			Content:      content{Role: "model", Parts: []part{{Text: "hello from gemini"}}},
			FinishReason: "STOP",
		}},
		UsageMetadata: usageMetadata{PromptTokenCount: 9, CandidatesTokenCount: 4, TotalTokenCount: 13},
	}), &captured)
	defer srv.Close()

	c := NewClient(srv.URL, "key-1", "gemini-1.5-flash")
	resp, err := c.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "be brief"},
			{Role: llm.RoleUser, Content: "hi"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "hello from gemini" {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Fatalf("finish = %s", resp.Choices[0].FinishReason)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 13 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
	// system → systemInstruction；user → contents[0]
	if captured.SystemInstruction == nil || captured.SystemInstruction.Parts[0].Text != "be brief" {
		t.Fatalf("sys = %+v", captured.SystemInstruction)
	}
	if len(captured.Contents) != 1 || captured.Contents[0].Role != "user" {
		t.Fatalf("contents = %+v", captured.Contents)
	}
	if len(captured.Contents[0].Parts) != 1 || captured.Contents[0].Parts[0].Text != "hi" {
		t.Fatalf("parts = %+v", captured.Contents[0].Parts)
	}
}

func TestChatToolCallsRoundTrip(t *testing.T) {
	srv := mockGemini(t, sseFrames(generateResponse{
		Candidates: []candidate{{
			Content: content{Role: "model", Parts: []part{
				{FunctionCall: &functionCall{Name: "shell", Args: map[string]any{"cmd": "ls"}}},
			}},
			FinishReason: "STOP",
		}},
	}), nil)
	defer srv.Close()

	c := NewClient(srv.URL, "k", "gemini-1.5-pro")
	resp, err := c.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "run ls"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	tcs := resp.Choices[0].Message.ToolCalls
	if len(tcs) != 1 || tcs[0].Function.Name != "shell" {
		t.Fatalf("tool calls = %+v", tcs)
	}
	if tcs[0].Function.Arguments != `{"cmd":"ls"}` {
		t.Fatalf("args = %q", tcs[0].Function.Arguments)
	}
}

func TestStreamDeltaAccumulation(t *testing.T) {
	// Gemini SSE 流式帧携带累积文本：frame1 "Hello", frame2 "Hello world"。
	srv := mockGemini(t, sseFrames(
		generateResponse{Candidates: []candidate{{
			Content: content{Role: "model", Parts: []part{{Text: "Hello"}}},
		}}},
		generateResponse{Candidates: []candidate{{
			Content:      content{Role: "model", Parts: []part{{Text: "Hello world"}}},
			FinishReason: "STOP",
		}}},
		generateResponse{UsageMetadata: usageMetadata{PromptTokenCount: 3, CandidatesTokenCount: 2, TotalTokenCount: 5}},
	), nil)
	defer srv.Close()

	c := NewClient(srv.URL, "k", "gemini-1.5-flash")
	ch, errCh := c.ChatStream(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	var text strings.Builder
	var finish string
	var usage *llm.Usage
	for chunk := range ch {
		text.WriteString(chunk.Text)
		if chunk.Finish != "" {
			finish = chunk.Finish
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
	}
	if err := <-errCh; err != nil {
		t.Fatalf("stream err: %v", err)
	}
	// delta 累积 = "Hello" + " world"。
	if text.String() != "Hello world" {
		t.Fatalf("text = %q", text.String())
	}
	if finish != "stop" {
		t.Fatalf("finish = %q", finish)
	}
	if usage == nil || usage.TotalTokens != 5 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestStreamFunctionCalls(t *testing.T) {
	srv := mockGemini(t, sseFrames(generateResponse{
		Candidates: []candidate{{
			Content: content{Role: "model", Parts: []part{
				{FunctionCall: &functionCall{Name: "echo", Args: map[string]any{"text": "x"}}},
			}},
			FinishReason: "STOP",
		}},
	}), nil)
	defer srv.Close()

	c := NewClient(srv.URL, "k", "m")
	ch, errCh := c.ChatStream(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "u"}},
	})
	var calls []llm.ToolCall
	for chunk := range ch {
		calls = append(calls, chunk.ToolCalls...)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("stream err: %v", err)
	}
	if len(calls) != 1 || calls[0].Function.Name != "echo" {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestToolResponseMapping(t *testing.T) {
	var captured generateRequest
	srv := mockGemini(t, sseFrames(generateResponse{
		Candidates: []candidate{{Content: content{Role: "model", Parts: []part{{Text: "ok"}}}}},
	}), &captured)
	defer srv.Close()

	c := NewClient(srv.URL, "k", "m")
	_, err := c.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "u"},
			{Role: llm.RoleTool, Content: `{"out":"42"}`, Name: "calc", ToolCallID: "call_calc"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	fr := captured.Contents[1].Parts[0].FunctionResponse
	if fr == nil || fr.Name != "calc" {
		t.Fatalf("functionResponse = %+v", fr)
	}
	if fr.Response["out"] != "42" {
		t.Fatalf("response = %+v", fr.Response)
	}
}

func TestToolsMapping(t *testing.T) {
	var captured generateRequest
	srv := mockGemini(t, sseFrames(generateResponse{
		Candidates: []candidate{{Content: content{Role: "model", Parts: []part{{Text: "ok"}}}}},
	}), &captured)
	defer srv.Close()

	c := NewClient(srv.URL, "k", "m")
	_, err := c.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "u"}},
		Tools: []llm.ToolSpec{{
			Type:     "function",
			Function: llm.ToolSpecFunc{Name: "echo", Description: "d", Parameters: map[string]any{"type": "object"}},
		}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(captured.Tools) != 1 || len(captured.Tools[0].FunctionDeclarations) != 1 {
		t.Fatalf("tools = %+v", captured.Tools)
	}
	if captured.Tools[0].FunctionDeclarations[0].Name != "echo" {
		t.Fatalf("decl = %+v", captured.Tools[0].FunctionDeclarations[0])
	}
}

func TestAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"code":400,"message":"bad","status":"INVALID_ARGUMENT"}}`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "k", "m")
	_, err := c.Chat(context.Background(), llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "x"}}})
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("err = %v", err)
	}
}

func TestDefaultBaseURLAndKeyHeader(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-goog-api-key")
		w.Header().Set("Content-Type", "application/json")
		resp := generateResponse{
			Candidates: []candidate{{Content: content{Role: "model", Parts: []part{{Text: "x"}}}}},
		}
		j, _ := json.Marshal(resp)
		_, _ = w.Write(j)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "secret", "m")
	if _, err := c.Chat(context.Background(), llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "u"}}}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if gotKey != "secret" {
		t.Fatalf("api key header = %q", gotKey)
	}
	if NewClient("", "k", "m").BaseURL != defaultBaseURL {
		t.Fatal("default base url wrong")
	}
}

func TestFinishReasonMapping(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"STOP", "stop"},
		{"MAX_TOKENS", "length"},
		{"SAFETY", "content_filter"},
		{"", "stop"},
		{"UNSPECIFIED", "stop"},
		{"OTHER", "stop"},
	} {
		if got := finishReason(tc.in); got != tc.want {
			t.Fatalf("finishReason(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStreamAPIErrorFrame(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"error":{"code":429,"message":"rate limited","status":"RESOURCE_EXHAUSTED"}}` + "\n\n"))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "k", "m")
	ch, errCh := c.ChatStream(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "u"}},
	})
	for range ch {
	}
	if err := <-errCh; err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("err = %v", err)
	}
}

func TestStreamEmptyStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(""))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "k", "m")
	ch, errCh := c.ChatStream(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "u"}},
	})
	for range ch {
	}
	if err := <-errCh; err == nil {
		t.Fatal("expected empty stream error")
	}
}

func TestMultiSystemMergeAndRoleModel(t *testing.T) {
	var captured generateRequest
	srv := mockGemini(t, sseFrames(generateResponse{
		Candidates: []candidate{{Content: content{Role: "model", Parts: []part{{Text: "ok"}}}}},
	}), &captured)
	defer srv.Close()

	c := NewClient(srv.URL, "k", "m")
	// 历史中的 assistant 消息 → role=model；多条 system 合并。
	_, err := c.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "s1"},
			{Role: llm.RoleSystem, Content: "s2"},
			{Role: llm.RoleUser, Content: "u"},
			{Role: llm.RoleAssistant, Content: "a"},
			{Role: llm.RoleUser, Content: "u2"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if captured.SystemInstruction == nil || captured.SystemInstruction.Parts[0].Text != "s1\n\ns2" {
		t.Fatalf("sys merge = %+v", captured.SystemInstruction)
	}
	roles := []string{}
	for _, ct := range captured.Contents {
		roles = append(roles, ct.Role)
	}
	if strings.Join(roles, ",") != "user,model,user" {
		t.Fatalf("roles = %v", roles)
	}
}

func TestChatRequestModelOverride(t *testing.T) {
	// req.Model 覆盖端点中的 model（runner 运行时换模型）。
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		resp := generateResponse{Candidates: []candidate{{Content: content{Role: "model", Parts: []part{{Text: "x"}}}}}}
		j, _ := json.Marshal(resp)
		_, _ = w.Write(j)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "k", "base-model")
	_, err := c.Chat(context.Background(), llm.ChatRequest{
		Model:    "runtime-model",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "u"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if !strings.Contains(gotPath, "models/runtime-model:") {
		t.Fatalf("path = %q", gotPath)
	}
}

func TestToolResultNonJSONContent(t *testing.T) {
	var captured generateRequest
	srv := mockGemini(t, sseFrames(generateResponse{
		Candidates: []candidate{{Content: content{Role: "model", Parts: []part{{Text: "ok"}}}}},
	}), &captured)
	defer srv.Close()

	c := NewClient(srv.URL, "k", "m")
	// 非法 JSON 的 tool 结果 → 包在 {"result": ...} 里。
	_, err := c.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "u"},
			{Role: llm.RoleTool, Content: "plain text result", Name: "shell"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	fr := captured.Contents[1].Parts[0].FunctionResponse
	if fr == nil || fr.Response["result"] != "plain text result" {
		t.Fatalf("fr = %+v", fr)
	}
}
