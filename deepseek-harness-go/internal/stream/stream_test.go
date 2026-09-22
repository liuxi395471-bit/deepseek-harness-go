package stream

import (
	"context"
	"strings"
	"testing"
	"time"

	"deepseek-harness-go/internal/llm"
)

// Helper: feed an SSE payload to SSEFeed and collect frames until channel closes.
func feedAll(t *testing.T, payload string) []Frame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch := SSEFeed(ctx, strings.NewReader(payload))
	var frames []Frame
	for f := range ch {
		frames = append(frames, f)
	}
	return frames
}

// §A.1.6 row 1: 单 frame 多 chunk delta text accumulates into one Content.
func TestRecombiner_MultiFrameText(t *testing.T) {
	r := NewRecombiner()
	r.Append(llm.StreamChunk{Text: "Hello, "})
	r.Append(llm.StreamChunk{Text: "world"})
	r.Append(llm.StreamChunk{Text: "!"})
	r.Append(llm.StreamChunk{Finish: "stop"})

	if got := r.Final().Content; got != "Hello, world!" {
		t.Fatalf("want %q got %q", "Hello, world!", got)
	}
}

// §A.1.6 row 2: empty stream (server closes immediately) — Final returns
// an empty message without panic.
func TestRecombiner_EmptyStream(t *testing.T) {
	r := NewRecombiner()
	if got := r.Final().Content; got != "" {
		t.Fatalf("want empty content got %q", got)
	}
	if r.Finished() {
		t.Fatalf("empty stream should not be Finished")
	}
}

// §A.1.6 row 3: keep-alive `:ping` comments do not surface as frames.
func TestParser_IgnoreKeepAlive(t *testing.T) {
	payload := ": ping\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"
	frames := feedAll(t, payload)
	if len(frames) != 1 {
		t.Fatalf("want 1 frame got %d", len(frames))
	}
	got := frames[0].Chunk(0).Text
	if got != "hi" {
		t.Fatalf("want %q got %q", "hi", got)
	}
}

// §A.1.6 row 4: terminal frame carries only finish_reason; chunk is appended.
func TestParser_TerminalFinishOnly(t *testing.T) {
	payload := "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"
	frames := feedAll(t, payload)
	if len(frames) != 1 {
		t.Fatalf("want 1 frame got %d", len(frames))
	}
	c := frames[0].Chunk(0)
	if c.Finish != "stop" {
		t.Fatalf("want finish=stop got %q", c.Finish)
	}
	if c.Text != "" {
		t.Fatalf("want empty text got %q", c.Text)
	}
}

// §A.1.6 row 5: ctx cancel mid-stream closes the channel; partial frames
// collected so far are still delivered.
func TestParser_CtxCancelClosesChannel(t *testing.T) {
	payload := "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n\n"
	ctx, cancel := context.WithCancel(context.Background())
	ch := SSEFeed(ctx, strings.NewReader(payload))
	first := <-ch
	if first.Chunk(0).Text != "a" {
		t.Fatalf("first chunk want a got %q", first.Chunk(0).Text)
	}
	cancel()
	// Allow goroutine to exit; verify channel is closed.
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatalf("channel not closed after ctx cancel")
		}
	}
}

// §A.1.6 row 6: 429 + retry scenario is a higher-level integration test
// (covered in T2 with HTTP client). At the stream layer we assert that
// malformed JSON is silently dropped.
func TestParser_MalformedFrameDropped(t *testing.T) {
	payload := "data: this is not json\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"
	frames := feedAll(t, payload)
	if len(frames) != 1 {
		t.Fatalf("want 1 frame got %d", len(frames))
	}
	if got := frames[0].Chunk(0).Text; got != "ok" {
		t.Fatalf("want ok got %q", got)
	}
}

// §A.1.6 row 7: tool_calls split across three frames reassemble by ID.
func TestRecombiner_ToolCallsAcrossFrames(t *testing.T) {
	r := NewRecombiner()
	r.Append(llm.StreamChunk{
		ToolCalls: []llm.ToolCall{{
			ID:   "call_1",
			Type: "function",
			Function: llm.ToolCallFunc{Name: "greet", Arguments: `{"name"`},
		}},
	})
	r.Append(llm.StreamChunk{
		ToolCalls: []llm.ToolCall{{
			ID:       "call_1",
			Function: llm.ToolCallFunc{Arguments: `:"Ada"}`},
		}},
	})
	r.Append(llm.StreamChunk{
		ToolCalls: []llm.ToolCall{{
			ID:   "call_1",
			Function: llm.ToolCallFunc{Arguments: ``},
		}},
		Finish: "tool_calls",
	})

	msg := r.Final()
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("want 1 tool call got %d", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_1" {
		t.Fatalf("want id=call_1 got %q", tc.ID)
	}
	if tc.Function.Name != "greet" {
		t.Fatalf("want name=greet got %q", tc.Function.Name)
	}
	if tc.Function.Arguments != `{"name":"Ada"}` {
		t.Fatalf("want {\"name\":\"Ada\"} got %q", tc.Function.Arguments)
	}
	if r.FinishReason() != "tool_calls" {
		t.Fatalf("want finish=tool_calls got %q", r.FinishReason())
	}
}

// §A.1.6 row 8: not exercised here (server returns 400 on first frame);
// handled at HTTP client layer in T2.
func TestRecombiner_AfterFinishedIsIdempotent(t *testing.T) {
	r := NewRecombiner()
	r.Append(llm.StreamChunk{Text: "x", Finish: "stop"})
	r.Append(llm.StreamChunk{Text: "should be dropped"})
	if got := r.Final().Content; got != "x" {
		t.Fatalf("want x got %q", got)
	}
}
