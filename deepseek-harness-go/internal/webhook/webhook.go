// Package webhook 提供 v8.1 Webhook 通知（CRUD + 异步 dispatch + HMAC 签名）。
//
// 设计要点：
//   - 内存存储（webhooks + deliveries 两个 map）；
//   - Dispatcher 维护一个 5 goroutine 的 worker 池 + retry 队列；
//   - 失败指数退避：1s / 5s / 30s / 300s / 1800s（最多 5 次）；
//   - 签名：HMAC-SHA256(secret, payload) → header `X-DSH-Signature: sha256=<hex>`。
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Webhook 是 webhooks 表的一行（v8.1）。
type Webhook struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	URL            string    `json:"url"`
	Secret         string    `json:"secret,omitempty"` // 仅创建/更新时返回
	Enabled        bool      `json:"enabled"`
	CreatedAt      time.Time `json:"createdAt"`
	LastStatus     int       `json:"lastStatus,omitempty"`
	LastError      string    `json:"lastError,omitempty"`
	LastDeliveredAt time.Time `json:"lastDeliveredAt,omitempty"`
}

// Delivery 是 webhook_deliveries 表的一行（v8.1）。
type Delivery struct {
	ID          string    `json:"id"`
	WebhookID   string    `json:"webhookId"`
	Payload     string    `json:"payload"`
	StatusCode  int       `json:"statusCode"`
	Attempt     int       `json:"attempt"`
	OK          bool      `json:"ok"`
	DeliveredAt time.Time `json:"deliveredAt"`
	Error       string    `json:"error,omitempty"`
}

// ErrNotFound 是查询 / 更新 / 删除命中未知 id。
var ErrNotFound = errors.New("webhook: not found")

// ErrEmptyNameOrURL 是创建/更新缺少 name 或 url。
var ErrEmptyNameOrURL = errors.New("webhook: name and url required")

// Dispatcher 维护 webhooks 与 deliveries；提供同步 Dispatch（用于测试 / 手动 trigger）。
type Dispatcher struct {
	mu        sync.Mutex
	hooks     map[string]*Webhook
	deliveries map[string][]*Delivery // webhook id → 历史
	next      int

	// HTTPClient 用于发送；测试时可替换。
	HTTPClient *http.Client
	// Workers 是 dispatch worker 数（默认 5）。
	Workers int
	// Backoff 是失败重试退避序列；index = attempt - 1。
	Backoff []time.Duration
	// MaxAttempts 是最大尝试次数（默认 5）。
	MaxAttempts int

	queue chan deliveryTask
	stop  chan struct{}
}

type deliveryTask struct {
	webhookID string
	payload   string
	attempt   int
}

// NewDispatcher 构造并启动 worker pool。
func NewDispatcher() *Dispatcher {
	d := &Dispatcher{
		hooks:       make(map[string]*Webhook),
		deliveries: make(map[string][]*Delivery),
		next:        1,
		HTTPClient:  &http.Client{Timeout: 15 * time.Second},
		Workers:     5,
		Backoff:     []time.Duration{1 * time.Second, 5 * time.Second, 30 * time.Second, 300 * time.Second, 1800 * time.Second},
		MaxAttempts: 5,
	}
	d.queue = make(chan deliveryTask, 1024)
	d.stop = make(chan struct{})
	for i := 0; i < d.Workers; i++ {
		go d.worker()
	}
	return d
}

// Close 停止 worker pool（清空 channel）。
func (d *Dispatcher) Close() {
	close(d.stop)
}

// ---------------------------------------------------------------------------
// CRUD

// Get 按 id 取 webhook（含 secret，未 redact）。
func (d *Dispatcher) Get(id string) (Webhook, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	h, ok := d.hooks[id]
	if !ok {
		return Webhook{}, ErrNotFound
	}
	return *h, nil
}

// List 列出全部 webhook（隐藏 secret）。
func (d *Dispatcher) List() []Webhook {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Webhook, 0, len(d.hooks))
	for _, h := range d.hooks {
		out = append(out, redact(*h))
	}
	return out
}

// Create 新增 webhook。
func (d *Dispatcher) Create(name, url, secret string, enabled bool) (Webhook, error) {
	if name == "" || url == "" {
		return Webhook{}, ErrEmptyNameOrURL
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	id := fmt.Sprintf("wh-%d-%d", time.Now().UnixNano(), d.next)
	d.next++
	h := &Webhook{
		ID:        id,
		Name:      name,
		URL:       url,
		Secret:    secret,
		Enabled:   enabled,
		CreatedAt: time.Now(),
	}
	d.hooks[id] = h
	return *h, nil
}

// Update 按 id 更新；空字段保留；secret 为空时不变；secret 为 "__clear__" 时清空。
func (d *Dispatcher) Update(id, name, url, secret string, enabled *bool) (Webhook, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	h, ok := d.hooks[id]
	if !ok {
		return Webhook{}, ErrNotFound
	}
	if name != "" {
		h.Name = name
	}
	if url != "" {
		h.URL = url
	}
	if secret == "__clear__" {
		h.Secret = ""
	} else if secret != "" {
		h.Secret = secret
	}
	if enabled != nil {
		h.Enabled = *enabled
	}
	return *h, nil
}

// Delete 按 id 删除。
func (d *Dispatcher) Delete(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.hooks[id]; !ok {
		return ErrNotFound
	}
	delete(d.hooks, id)
	delete(d.deliveries, id)
	return nil
}

// Dispatch 同步触发一次 webhook；返回最新一条 delivery（不入 retry 队列）。
//
// v8.1 起：用户主动 test / schedule 触发 → 立即发；首次失败 → 也入 retry 队列。
func (d *Dispatcher) Dispatch(ctx context.Context, id, payload string) (Delivery, error) {
	d.mu.Lock()
	h, ok := d.hooks[id]
	if !ok {
		d.mu.Unlock()
		return Delivery{}, ErrNotFound
	}
	secret := h.Secret
	url := h.URL
	d.mu.Unlock()
	del, _ := d.send(ctx, id, url, secret, payload, 1)
	return del, nil
}

// ListDeliveries 按 webhook id 取最近 limit 条（默认 50）。
func (d *Dispatcher) ListDeliveries(id string, limit int) ([]Delivery, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.hooks[id]; !ok {
		return nil, ErrNotFound
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	all := d.deliveries[id]
	if len(all) > limit {
		all = all[len(all)-limit:]
	}
	out := make([]Delivery, len(all))
	for i, x := range all {
		out[i] = *x
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// 内部 worker

func (d *Dispatcher) worker() {
	for {
		select {
		case <-d.stop:
			return
		case task := <-d.queue:
			d.handleRetry(task)
		}
	}
}

func (d *Dispatcher) handleRetry(task deliveryTask) {
	d.mu.Lock()
	h, ok := d.hooks[task.webhookID]
	if !ok {
		d.mu.Unlock()
		return
	}
	url := h.URL
	secret := h.Secret
	enabled := h.Enabled
	d.mu.Unlock()
	if !enabled {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	del, ok := d.send(ctx, task.webhookID, url, secret, task.payload, task.attempt)
	if !ok && task.attempt < d.MaxAttempts {
		// schedule retry
		idx := task.attempt - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(d.Backoff) {
			idx = len(d.Backoff) - 1
		}
		wait := d.Backoff[idx]
		go func(t deliveryTask, w time.Duration) {
			time.Sleep(w)
			d.queue <- t
		}(deliveryTask{webhookID: task.webhookID, payload: task.payload, attempt: task.attempt + 1}, wait)
	}
	_ = del
}

// send 执行一次 HTTP POST + 签名 + 记录 delivery；返回 (delivery, ok)。
func (d *Dispatcher) send(ctx context.Context, hookID, url, secret, payload string, attempt int) (Delivery, bool) {
	body := []byte(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return d.record(hookID, payload, 0, attempt, false, "build req: "+err.Error()), false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "deepseek-harness-webhook/1.0")
	if secret != "" {
		sig := SignPayload(secret, body)
		req.Header.Set("X-DSH-Signature", "sha256="+sig)
	}

	resp, err := d.HTTPClient.Do(req)
	if err != nil {
		return d.record(hookID, payload, 0, attempt, false, "do: "+err.Error()), false
	}
	defer resp.Body.Close()
	// drain body to allow keep-alive
	_, _ = io.Copy(io.Discard, resp.Body)
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	errStr := ""
	if !ok {
		errStr = "status " + strconv.Itoa(resp.StatusCode)
	}
	return d.record(hookID, payload, resp.StatusCode, attempt, ok, errStr), ok
}

func (d *Dispatcher) record(hookID, payload string, status int, attempt int, ok bool, errStr string) Delivery {
	d.mu.Lock()
	defer d.mu.Unlock()
	id := fmt.Sprintf("dlv-%d-%d", time.Now().UnixNano(), d.next)
	d.next++
	del := &Delivery{
		ID:          id,
		WebhookID:   hookID,
		Payload:     payload,
		StatusCode:  status,
		Attempt:     attempt,
		OK:          ok,
		DeliveredAt: time.Now(),
		Error:       errStr,
	}
	d.deliveries[hookID] = append(d.deliveries[hookID], del)
	// 限制每 hook 最多 500 条
	if len(d.deliveries[hookID]) > 500 {
		d.deliveries[hookID] = d.deliveries[hookID][len(d.deliveries[hookID])-500:]
	}
	// 更新 webhook 元数据
	if h, ok := d.hooks[hookID]; ok {
		h.LastStatus = status
		h.LastError = errStr
		h.LastDeliveredAt = del.DeliveredAt
	}
	return *del
}

// redact 复制 webhook 去掉 secret（用于 list 等响应）。
func redact(h Webhook) Webhook {
	h.Secret = ""
	return h
}

// ---------------------------------------------------------------------------
// 签名工具

// SignPayload 返回 base64-free hex 签名（便于 header 传输）。
func SignPayload(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature 用同样 secret + body 验签（给接收方使用）。
func VerifySignature(secret string, body []byte, headerVal string) bool {
	// 支持两种 header 格式：纯 hex / "sha256=hex"
	v := strings.TrimPrefix(headerVal, "sha256=")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(v))
}

// ---------------------------------------------------------------------------
// 序列化为 JSON（worker 在 retry 时 payload 是 string，不需要 marshalling）

// DeliveryJSON 是 Delivery 的 JSON 投影（用于外部测试）。
func DeliveryJSON(d Delivery) string {
	b, _ := json.Marshal(d)
	return string(b)
}
