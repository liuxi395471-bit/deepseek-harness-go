package console

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// approvalQueue 是 v8 控制台"待审批"内存队列。
//
// 设计：v8.0 不持久化；重启后队列清空。Runner 通过 Enqueue 把
// PolicyAsk 的工具调用推入队列；前端通过 List 拉取并 Decide。
// Decide 之后 result channel 收到 "allow" / "deny"，让 HTTPPollApprover
// resolver 唤醒。
//
// 线程安全：mu 保护 items；result channel 由 Enqueue 时分配并随 item
// 一起存。
type approvalQueue struct {
	mu    sync.Mutex
	items map[string]*pendingApproval
}

type pendingApproval struct {
	ID         string
	Item       ApprovalItem
	Result     chan string // capacity 1
	CreatedAt  time.Time
}

// newApprovalQueue 构造空队列。
func newApprovalQueue() *approvalQueue {
	return &approvalQueue{items: make(map[string]*pendingApproval)}
}

// NewApprovalQueue 导出版本，供 cmd/dsh 装配用。
func NewApprovalQueue() *approvalQueue { return newApprovalQueue() }

// Enqueue 入队并返回 id + result channel。
func (q *approvalQueue) Enqueue(item ApprovalItem) (string, <-chan string, error) {
	id, err := newID()
	if err != nil {
		return "", nil, err
	}
	result := make(chan string, 1)
	p := &pendingApproval{ID: id, Item: item, Result: result, CreatedAt: time.Now()}
	q.mu.Lock()
	q.items[id] = p
	q.mu.Unlock()
	return id, result, nil
}

// List 复制当前快照（按 createdAt 升序）。
func (q *approvalQueue) List() []ApprovalItem {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]ApprovalItem, 0, len(q.items))
	for _, p := range q.items {
		p.Item.CreatedAt = p.CreatedAt
		out = append(out, p.Item)
	}
	return out
}

// Decide 把决策写到 result channel 并移除条目；id 不存在返回错误。
func (q *approvalQueue) Decide(id, decision string) error {
	q.mu.Lock()
	p, ok := q.items[id]
	if !ok {
		q.mu.Unlock()
		return ErrApprovalNotFound
	}
	delete(q.items, id)
	q.mu.Unlock()
	if decision == "" {
		decision = "deny"
	}
	// 非阻塞写入（容量 1）
	select {
	case p.Result <- decision:
	default:
	}
	close(p.Result)
	return nil
}

// DecideSync 等到决策被写入或 ctx 取消（给 resolver 风格的等待用）。
func (q *approvalQueue) DecideSync(ctx context.Context, id, decision string) error {
	q.mu.Lock()
	p, ok := q.items[id]
	if !ok {
		q.mu.Unlock()
		return ErrApprovalNotFound
	}
	delete(q.items, id)
	q.mu.Unlock()
	if decision == "" {
		decision = "deny"
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case p.Result <- decision:
	}
	close(p.Result)
	return nil
}

// Resolver 返回一个把 id 映射到该队列 decision channel 的 Resolver 函数。
//
// 使用方式（main.go 装配）：
//
//   q := newApprovalQueue()
//   httpPoll := &approval.HTTPPollApprover{
//       ID:       sid,
//       Resolver: q.Resolver(),
//   }
//
// HTTPPollApprover 会轮询 Resolver.Resolve(ctx, id)，pending 时返回
// approval.PendingError()；本 resolver 等待队列 Decide 或 ctx 取消。
func (q *approvalQueue) Resolver() approvalResolver {
	return func(ctx context.Context, id string) (string, error) {
		q.mu.Lock()
		p, ok := q.items[id]
		if !ok {
			q.mu.Unlock()
			// 已决策或从未存在：返回 "deny"（最保守）
			return "deny", nil
		}
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return "deny", ctx.Err()
		case dec := <-p.Result:
			return dec, nil
		}
	}
}

// approvalResolver 是 Resolver 的极简适配（避免 import 循环）。
type approvalResolver func(ctx context.Context, id string) (string, error)

// --- helpers ---

func newID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errors.New("console: rand failed")
	}
	return hex.EncodeToString(b[:]), nil
}
