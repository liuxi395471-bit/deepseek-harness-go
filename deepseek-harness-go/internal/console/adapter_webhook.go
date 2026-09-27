package console

import (
	"context"

	"deepseek-harness-go/internal/webhook"
)

// WebhookAdapter 把 internal/webhook.Dispatcher 适配成 console.WebhookBackend。
type WebhookAdapter struct {
	Dispatcher *webhook.Dispatcher
}

// NewWebhookAdapter 构造。
func NewWebhookAdapter(d *webhook.Dispatcher) *WebhookAdapter { return &WebhookAdapter{Dispatcher: d} }

// List 实现 WebhookBackend。
func (a *WebhookAdapter) List(_ context.Context) ([]WebhookItem, error) {
	if a.Dispatcher == nil {
		return nil, ErrBackendMissing
	}
	srcs := a.Dispatcher.List()
	out := make([]WebhookItem, len(srcs))
	for i, h := range srcs {
		out[i] = webhookToItem(h)
	}
	return out, nil
}

// Create 实现 WebhookBackend。
func (a *WebhookAdapter) Create(_ context.Context, item WebhookItem) (WebhookItem, error) {
	if a.Dispatcher == nil {
		return WebhookItem{}, ErrBackendMissing
	}
	h, err := a.Dispatcher.Create(item.Name, item.URL, item.Secret, item.Enabled)
	if err != nil {
		return WebhookItem{}, err
	}
	return webhookToItem(h), nil
}

// Update 实现 WebhookBackend。
func (a *WebhookAdapter) Update(_ context.Context, id string, item WebhookItem) (WebhookItem, error) {
	if a.Dispatcher == nil {
		return WebhookItem{}, ErrBackendMissing
	}
	enabled := item.Enabled
	h, err := a.Dispatcher.Update(id, item.Name, item.URL, item.Secret, &enabled)
	if err != nil {
		return WebhookItem{}, err
	}
	return webhookToItem(h), nil
}

// Delete 实现 WebhookBackend。
func (a *WebhookAdapter) Delete(_ context.Context, id string) error {
	if a.Dispatcher == nil {
		return ErrBackendMissing
	}
	return a.Dispatcher.Delete(id)
}

// Dispatch 实现 WebhookBackend。
func (a *WebhookAdapter) Dispatch(ctx context.Context, id, payload string) (WebhookDelivery, error) {
	if a.Dispatcher == nil {
		return WebhookDelivery{}, ErrBackendMissing
	}
	d, err := a.Dispatcher.Dispatch(ctx, id, payload)
	if err != nil {
		return WebhookDelivery{}, err
	}
	return deliveryToItem(d), nil
}

// ListDeliveries 实现 WebhookBackend。
func (a *WebhookAdapter) ListDeliveries(_ context.Context, id string, limit int) ([]WebhookDelivery, error) {
	if a.Dispatcher == nil {
		return nil, ErrBackendMissing
	}
	srcs, err := a.Dispatcher.ListDeliveries(id, limit)
	if err != nil {
		return nil, err
	}
	out := make([]WebhookDelivery, len(srcs))
	for i, d := range srcs {
		out[i] = deliveryToItem(d)
	}
	return out, nil
}

func webhookToItem(h webhook.Webhook) WebhookItem {
	return WebhookItem{
		ID:              h.ID,
		Name:            h.Name,
		URL:             h.URL,
		Secret:          h.Secret, // 注意：item 字段 `json:"secret,omitempty"`；空时不序列化
		Enabled:         h.Enabled,
		CreatedAt:       h.CreatedAt,
		LastStatus:      h.LastStatus,
		LastError:       h.LastError,
		LastDeliveredAt: h.LastDeliveredAt,
	}
}

func deliveryToItem(d webhook.Delivery) WebhookDelivery {
	return WebhookDelivery{
		ID:          d.ID,
		WebhookID:   d.WebhookID,
		Payload:     d.Payload,
		StatusCode:  d.StatusCode,
		Attempt:     d.Attempt,
		OK:          d.OK,
		DeliveredAt: d.DeliveredAt,
		Error:       d.Error,
	}
}
