package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const sseDataPrefix = "data: "

func sseFeed(ctx context.Context, r io.Reader) <-chan sseFrame {
	ch := make(chan sseFrame, 4)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
		// multi-line accumulator
		var dataLines []string
		flush := func() {
			if len(dataLines) == 0 {
				return
			}
			payload := strings.Join(dataLines, "\n")
			dataLines = dataLines[:0]
			if payload == "[DONE]" {
				return
			}
			var sse SSEFrame
			if err := json.Unmarshal([]byte(payload), &sse); err != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case ch <- sseFrame{parsed: sse}:
			}
		}
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				flush()
				continue
			}
			if line[0] == ':' {
				continue
			}
			if len(line) < len(sseDataPrefix) || line[:len(sseDataPrefix)] != sseDataPrefix {
				continue
			}
			dataLines = append(dataLines, line[len(sseDataPrefix):])
		}
		flush()
		if err := scanner.Err(); err != nil {
			select {
			case <-ctx.Done():
			case ch <- sseFrame{err: fmt.Errorf("sse: scan: %w", err)}:
			}
		}
	}()
	return ch
}

