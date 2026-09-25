package llm

// sseFrame 表示一个已解码的 SSE data 行。StreamChunk 是调用方
// 消费的已解码输出；本类型为内部类型。
//
// err 非 nil 时表示扫描器或读取器出现不可恢复错误（如单行超过
// scanner.Buffer 上限），调用方应将其视为致命流错误。
type sseFrame struct {
	parsed SSEFrame
	err    error
}

// Chunk 返回指定 choice 下标对应的已解码 StreamChunk。
func (f sseFrame) Chunk(idx int) StreamChunk {
	sc := StreamChunk{Index: idx}
	for _, c := range f.parsed.Choices {
		if c.Delta.Content != "" {
			sc.Text = c.Delta.Content
		}
		if len(c.Delta.ToolCalls) > 0 {
			for _, tc := range c.Delta.ToolCalls {
				sc.ToolCalls = append(sc.ToolCalls, ToolCall{
					ID:   tc.ID,
					Type: tc.Type,
					Function: ToolCallFunc{
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

