package anthropic

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// 我们关注的 Anthropic SSE 事件类型。
const (
	evMessageStart      = "message_start"
	evContentBlockStart = "content_block_start"
	evContentBlockDelta = "content_block_delta"
	evContentBlockStop  = "content_block_stop"
	evMessageDelta      = "message_delta"
	evMessageStop       = "message_stop"
	evPing              = "ping"
	evError             = "error"
)

// Event 是一个已解码的 Anthropic SSE 事件。我们只携带需要的
// 字段；未知事件类型会产生 Event{Type: <未知>}，由调用方决定
// 是忽略还是上报。
type Event struct {
	Type string
	// Raw 是原始 JSON 载荷，便于测试与调试。
	Raw json.RawMessage

	// message_start 字段
	MessageID  string `json:"-"`
	InputTok   int    `json:"-"`

	// content_block_delta 字段
	DeltaType string `json:"-"` // "text_delta" | "input_json_delta" | ...
	DeltaText string `json:"-"`

	// message_delta 字段
	StopReason   string `json:"-"`
	OutputTok    int    `json:"-"`

	// error 字段
	ErrMessage string `json:"-"`
}

// Feed 读取 Anthropic SSE 流（event:/data: 行对，以空行分隔）
// 并发出已解码的 Event。它在以下情况返回：
//   - 流结束（底层 reader 遇到 EOF），
//   - ctx 被取消，
//   - 观察到致命的 error 事件（发出该 Event 后 channel 会被
//     关闭，调用方能以干净的方式看到流结束）。
//
// 以 ':' 开头的行是 SSE 注释，会被忽略；上述已知类型之外的
// 事件类型同样会被忽略。
func Feed(ctx context.Context, r io.Reader) <-chan Event {
	ch := make(chan Event, 4)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)

		var (
			eventType string
			dataLines []string
		)
		flush := func() {
			if eventType == "" && len(dataLines) == 0 {
				return
			}
			ev, perr := parseEvent(eventType, strings.Join(dataLines, "\n"))
			eventType = ""
			dataLines = dataLines[:0]
			if perr != nil {
				return // 跳过格式错误的帧
			}
			select {
			case <-ctx.Done():
				return
			case ch <- ev:
			}
			// 收到显式的 error 事件时提前终止，
			// 避免调用方一直等待 EOF。
			if ev.Type == evError {
				return
			}
		}

		for scanner.Scan() {
			if err := ctx.Err(); err != nil {
				return
			}
			line := scanner.Text()
			switch {
			case line == "":
				flush()
			case line[0] == ':':
				// SSE 注释 / 保活帧；忽略。
				continue
			case strings.HasPrefix(line, "event: "):
				eventType = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
			default:
				// 未知字段（id:、retry: 等）——忽略。
			}
		}
		// 最后一帧的兜底刷新：防止流在没有结尾空行的情况下结束。
		flush()
	}()
	return ch
}

// parseEvent 将一个 Anthropic 事件载荷解码为我们带类型的 Event。
func parseEvent(eventType, data string) (Event, error) {
	ev := Event{Type: eventType, Raw: json.RawMessage(data)}
	if data == "" {
		return ev, nil
	}
	switch eventType {
	case evMessageStart:
		var p struct {
			Message struct {
				ID    string `json:"id"`
				Usage struct {
					InputTokens int `json:"input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(data), &p); err != nil {
			return ev, fmt.Errorf("anthropic: parse message_start: %w", err)
		}
		ev.MessageID = p.Message.ID
		ev.InputTok = p.Message.Usage.InputTokens
	case evContentBlockDelta:
		var p struct {
			Index int `json:"index"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text,omitempty"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &p); err != nil {
			return ev, fmt.Errorf("anthropic: parse content_block_delta: %w", err)
		}
		ev.DeltaType = p.Delta.Type
		ev.DeltaText = p.Delta.Text
	case evMessageDelta:
		var p struct {
			Delta struct {
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
			Usage struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &p); err != nil {
			return ev, fmt.Errorf("anthropic: parse message_delta: %w", err)
		}
		ev.StopReason = p.Delta.StopReason
		ev.OutputTok = p.Usage.OutputTokens
	case evError:
		var p struct {
			Error struct {
				Message string `json:"message"`
				Type    string `json:"type"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &p); err != nil {
			return ev, fmt.Errorf("anthropic: parse error: %w", err)
		}
		ev.ErrMessage = p.Error.Message
	case evPing, evContentBlockStart, evContentBlockStop, evMessageStop:
		// 无需处理的事件；保留原始载荷以便调试。
	default:
		// 未知事件类型：仍会发出，供调用方记录日志。
	}
	return ev, nil
}

// ErrStreamEnded 由检测到 message_stop 的辅助函数返回。
var ErrStreamEnded = errors.New("anthropic: stream ended")
