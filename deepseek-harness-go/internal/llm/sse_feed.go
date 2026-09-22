package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
)

const sseDataPrefix = "data: "

func sseFeed(ctx context.Context, r io.Reader) <-chan sseFrame {
	ch := make(chan sseFrame, 4)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" || line[0] == ':' {
				continue
			}
			if len(line) < len(sseDataPrefix) || line[:len(sseDataPrefix)] != sseDataPrefix {
				continue
			}
			payload := line[len(sseDataPrefix):]
			if payload == "[DONE]" {
				return
			}
			var sse SSEFrame
			if err := json.Unmarshal([]byte(payload), &sse); err != nil {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case ch <- sseFrame{parsed: sse}:
			}
		}
	}()
	return ch
}

