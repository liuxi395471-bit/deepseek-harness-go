package console

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"time"

	"deepseek-harness-go/internal/audit"
)

// AuditAdapter 从 v3 audit.FileLogger 输出的 JSONL 文件读取历史。
//
// 设计：审计日志本身是 append-only JSONL；读取时按行反序列化为
// audit.Event，再投影到 console.AuditRecord。
//
// 限制：
//   - 仅返回最近 limit 条（按文件倒序扫描；适合中小体量）；
//   - Export 流式输出同样数据到 w；写大文件时可考虑后续接 mmap。
type AuditAdapter struct {
	Path string
}

// NewAuditAdapter 构造；path 为 audit.JSONL 文件路径，空时禁用。
func NewAuditAdapter(path string) *AuditAdapter {
	return &AuditAdapter{Path: path}
}

// Query 返回最近 limit 条审计记录（按时间倒序）。
func (a *AuditAdapter) Query(_ context.Context, limit int) ([]AuditRecord, error) {
	if a.Path == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 200
	}
	all, err := readAllJSONL(a.Path)
	if err != nil {
		return nil, err
	}
	// 倒序：最新在前
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	if limit > 0 && limit < len(all) {
		all = all[:limit]
	}
	return auditEventsToRecords(all), nil
}

// Export 把最近 exportLimit 条 JSONL 流式写到 w。
func (a *AuditAdapter) Export(ctx context.Context, w io.Writer, exportLimit int) error {
	if a.Path == "" {
		return nil
	}
	if exportLimit <= 0 {
		exportLimit = 1000
	}
	f, err := os.Open(a.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	// 先读出全部到内存（一次性 JSONL 文件通常 ≤ MB 级）。
	all, err := readAllJSONL(a.Path)
	if err != nil {
		return err
	}
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	if exportLimit > 0 && exportLimit < len(all) {
		all = all[:exportLimit]
	}
	bw := bufio.NewWriter(w)
	defer bw.Flush()
	for _, ev := range all {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		b, err := json.Marshal(ev)
		if err != nil {
			continue
		}
		if _, err := bw.Write(b); err != nil {
			return err
		}
		if _, err := bw.WriteString("\n"); err != nil {
			return err
		}
	}
	return nil
}

// readAllJSONL 逐行解析 JSONL 到 audit.Event 切片。
func readAllJSONL(path string) ([]audit.Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	// 单行上限拉高（工具调用 args_raw 可能较长）。
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var out []audit.Event
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev audit.Event
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		out = append(out, ev)
	}
	return out, sc.Err()
}

// auditEventsToRecords 把 []audit.Event 投影为 []AuditRecord。
func auditEventsToRecords(in []audit.Event) []AuditRecord {
	out := make([]AuditRecord, len(in))
	for i, ev := range in {
		ts := ev.TS
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		out[i] = AuditRecord{
			TS:        ts,
			SessionID: ev.SessionID,
			Event:     ev.Event,
			Round:     ev.Round,
			Tool:      ev.Tool,
			ArgsHash:  ev.ArgsHash,
			ArgsRaw:   ev.ArgsRaw,
			Decision:  ev.Decision,
			Source:    ev.Source,
			Model:     ev.Model,
			Method:    ev.Method,
			Path:      ev.Path,
			Status:    ev.Status,
			DurMS:     ev.DurMS,
		}
	}
	return out
}
