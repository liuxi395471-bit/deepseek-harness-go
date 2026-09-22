package stream

import (
	"bufio"
	"context"
	"encoding/json"
	"io"

	"deepseek-harness-go/internal/llm"
)

const dataPrefix = "data: "

// Frame 是一条解码后的 SSE data 行。
type Frame struct {
	parsed llm.SSEFrame
}

// Chunk 投影出指定 choice 索引的解码后 StreamChunk。
func (f Frame) Chunk(idx int) llm.StreamChunk {
	sc := llm.StreamChunk{Index: idx}
	for _, c := range f.parsed.Choices {
		if c.Delta.Content != "" {
			sc.Text = c.Delta.Content
		}
		if len(c.Delta.ToolCalls) > 0 {
			for _, tc := range c.Delta.ToolCalls {
				sc.ToolCalls = append(sc.ToolCalls, llm.ToolCall{
					ID:   tc.ID,
					Type: tc.Type,
					Function: llm.ToolCallFunc{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				})
			}
		}
		if c.FinishReason != "" {
			sc.Finish = c.FinishReason
		}
	}
	return sc
}

// SSEFeed 从 r 读取 `data: ...\n\n` 行并发出解码后的 Frame。
func SSEFeed(ctx context.Context, r io.Reader) <-chan Frame {
	ch := make(chan Frame, 4)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
		for scanner.Scan() {
			if err := ctx.Err(); err != nil {
				return
			}
			line := scanner.Text()
			if line == "" || line[0] == ':' {
				continue
			}
			if len(line) < len(dataPrefix) || line[:len(dataPrefix)] != dataPrefix {
				continue
			}
			payload := line[len(dataPrefix):]
			if payload == "[DONE]" {
				return
			}
			var sse llm.SSEFrame
			if err := json.Unmarshal([]byte(payload), &sse); err != nil {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case ch <- Frame{parsed: sse}:
			}
		}
	}()
	return ch
}
