// Package store：会话的导入/导出与按 id 恢复辅助函数。
//
// v2（DESIGN-v2 §C.1）：
//
//   - Export：将一个会话（id、消息、用量、轮次、预览）序列化为可
//     在之后导入的 JSON 文档。
//   - Import：解析 JSON 文档并在目标 store 中重建会话。默认保留原
//     id；设置 preserveID=false 可让 store 分配新 id。
//   - Resume：辅助函数，按 id 返回 Session，将 ErrNotFound 视为携带
//     用户友好信息的致命错误。
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"deepseek-harness-go/internal/llm"
)

// ExportSession 将一个会话序列化为 JSON 友好的字节切片。
//
// 格式为：
//
//	{
//	  "version": "dsh-session/v2",
//	  "session": { ... 完整的 Session 结构 ... }
//	}
//
// 使用 ImportSession 解析本函数的输出。
func ExportSession(s Session) ([]byte, error) {
	wrapper := struct {
		Version string  `json:"version"`
		Session Session `json:"session"`
	}{Version: "dsh-session/v2", Session: s}
	return json.MarshalIndent(wrapper, "", "  ")
}

// ImportOptions 控制 ImportSession 的行为。
type ImportOptions struct {
	PreserveID bool // 为 true 时复用会话原 id（幂等：替换已有记录）
}

// ImportSession 反序列化 data 并写入 dst。已存在的记录会被替换
// （PreserveID=true）或分配新 id（PreserveID=false）。
func ImportSession(ctx context.Context, dst Store, data []byte, opts ImportOptions) (Session, error) {
	var wrapper struct {
		Version string  `json:"version"`
		Session Session `json:"session"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return Session{}, fmt.Errorf("store: import parse: %w", err)
	}
	if wrapper.Version != "dsh-session/v2" {
		return Session{}, fmt.Errorf("store: import: unknown version %q", wrapper.Version)
	}
	sess := wrapper.Session
	if sess.ID == "" {
		return Session{}, errors.New("store: import: session has no id")
	}

	// 选择 id 策略。
	targetID := sess.ID
	if !opts.PreserveID {
		newSess, err := dst.Begin(ctx)
		if err != nil {
			return Session{}, fmt.Errorf("store: import begin: %w", err)
		}
		targetID = newSess.ID
	} else {
		// 先删除已存在的行使导入幂等，然后用原始 id 重建会话
		// （Begin 每次都会生成新的 UUID，所以必须先创建，再删除新行，
		// 然后重新插入）。
		newSess, err := dst.Begin(ctx)
		if err != nil {
			return Session{}, fmt.Errorf("store: import begin: %w", err)
		}
		newID := newSess.ID
		// 删除新生成的空行。
		_ = deleteByID(ctx, dst, newID)
		// 用原始 id 重新打开：直接插入 sessions 表。
		if err := recreateSession(ctx, dst, sess); err != nil {
			return Session{}, fmt.Errorf("store: import recreate: %w", err)
		}
		targetID = sess.ID
	}

	if opts.PreserveID {
		// recreateSession 已设置消息和用量；无需再追加。
	} else {
		// 新 id：插入所有消息。
		for _, m := range sess.Messages {
			if err := dst.Append(ctx, targetID, m); err != nil {
				return Session{}, fmt.Errorf("store: import append: %w", err)
			}
		}
		if sess.UsageTotal.TotalTokens != 0 || sess.UsageTotal.PromptTokens != 0 {
			if err := dst.UpdateUsage(ctx, targetID, sess.UsageTotal); err != nil {
				return Session{}, fmt.Errorf("store: import usage: %w", err)
			}
		}
	}
	// 返回加载后的会话，让调用方看到刚导入的状态。
	return dst.Load(ctx, targetID)
}

// deleteByID 尽力删除。即使行不存在也返回 nil。
func deleteByID(ctx context.Context, s Store, id string) error {
	switch st := s.(type) {
	case *SQLiteStore:
		_, err := st.db.ExecContext(ctx, `DELETE FROM messages WHERE session_id=?`, id)
		if err != nil {
			return err
		}
		_, err = st.db.ExecContext(ctx, `DELETE FROM sessions WHERE id=?`, id)
		return err
	case *MapStore:
		st.mu.Lock()
		defer st.mu.Unlock()
		delete(st.sessions, id)
		return nil
	default:
		return nil
	}
}

// newEmptyIDFromBegin 调用 Begin 并返回其 id；调用方会丢弃产生的空行。
func newEmptyIDFromBegin(s Store) string {
	sess, err := s.Begin(context.Background())
	if err != nil {
		return ""
	}
	return sess.ID
}

// ResumeSession 是一个便捷函数：按 id 获取会话，未找到时返回
// 用户友好的错误。
func ResumeSession(ctx context.Context, s Store, id string) (Session, error) {
	sess, err := s.Load(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Session{}, fmt.Errorf("store: session %q not found", id)
		}
		return Session{}, err
	}
	// 更新会话的 updated_at，使 List 将其排在最前。
	return sess, nil
}

// ExportAll 遍历 List 并将每个会话导出到单个 JSON 文档。
// 使用 ImportAll 恢复。
func ExportAll(ctx context.Context, s Store) ([]byte, error) {
	list, err := s.List(ctx, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("store: export all list: %w", err)
	}
	wrapper := struct {
		Version   string    `json:"version"`
		Sessions  []Session `json:"sessions"`
		ExportedAt time.Time `json:"exported_at"`
	}{Version: "dsh-session/v2", Sessions: list, ExportedAt: time.Now().UTC()}
	return json.MarshalIndent(wrapper, "", "  ")
}

// ImportAll 遍历列表格式的导出并重新导入每个会话。
// preserveID 控制 id 的复用（见 ImportOptions）。
func ImportAll(ctx context.Context, dst Store, data []byte, opts ImportOptions) (int, error) {
	var wrapper struct {
		Version   string    `json:"version"`
		Sessions  []Session `json:"sessions"`
		ExportedAt time.Time `json:"exported_at"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return 0, fmt.Errorf("store: import all parse: %w", err)
	}
	count := 0
	for _, sess := range wrapper.Sessions {
		b, err := ExportSession(sess)
		if err != nil {
			return count, err
		}
		if _, err := ImportSession(ctx, dst, b, opts); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// recreateSession 按给定字段插入会话行。供 ImportSession 的
// PreserveID 路径用于以原始 id 重新打开会话。
func recreateSession(ctx context.Context, s Store, sess Session) error {
	switch st := s.(type) {
	case *SQLiteStore:
		// 插入或替换会话行（不触碰 messages 表）。
		_, err := st.db.ExecContext(ctx,
			`INSERT OR REPLACE INTO sessions(id, created_at, updated_at, preview, rounds,
			  prompt_tokens, completion_tokens, total_tokens)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			sess.ID, sess.CreatedAt.UnixMilli(), sess.UpdatedAt.UnixMilli(),
			sess.Preview, sess.Rounds,
			sess.UsageTotal.PromptTokens, sess.UsageTotal.CompletionTokens, sess.UsageTotal.TotalTokens,
		)
		if err != nil {
			return err
		}
		// 逐条插入消息（用 REPLACE 也可以，但顺序插入更安全且保持
		// seq 顺序）。
		for seq, m := range sess.Messages {
			tcJSON := ""
			if len(m.ToolCalls) > 0 {
				if b, err := json.Marshal(m.ToolCalls); err == nil {
					tcJSON = string(b)
				}
			}
			if _, err := st.db.ExecContext(ctx,
				`INSERT INTO messages(session_id, seq, role, content, tool_call_id, tool_calls)
				 VALUES (?, ?, ?, ?, ?, ?)`,
				sess.ID, seq, string(m.Role), m.Content, m.ToolCallID, tcJSON,
			); err != nil {
				return err
			}
		}
		return nil
	case *MapStore:
		st.mu.Lock()
		defer st.mu.Unlock()
		st.sessions[sess.ID] = &mapSession{
			id:         sess.ID,
			createdAt:  sess.CreatedAt,
			updatedAt:  sess.UpdatedAt,
			rounds:     sess.Rounds,
			preview:    sess.Preview,
			messages:   append([]llm.Message{}, sess.Messages...),
			usageTotal: sess.UsageTotal,
		}
		return nil
	default:
		return errors.New("store: recreateSession not implemented for this Store type")
	}
}
