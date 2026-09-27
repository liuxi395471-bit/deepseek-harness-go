package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSignAndVerify(t *testing.T) {
	body := []byte(`{"hello":"world"}`)
	secret := "test-secret-key"
	got := SignPayload(secret, body)
	// 独立计算正确的签名
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	if got != want {
		t.Fatalf("signature: got %s want %s", got, want)
	}
	if !VerifySignature(secret, body, "sha256="+got) {
		t.Fatal("verify with sha256= prefix failed")
	}
	if !VerifySignature(secret, body, got) {
		t.Fatal("verify without prefix failed")
	}
	if VerifySignature("wrong", body, got) {
		t.Fatal("wrong secret should fail")
	}
}

func TestDispatcherCRUD(t *testing.T) {
	d := NewDispatcher()
	defer d.Close()

	h, err := d.Create("my-hook", "https://example.com/hook", "secret-abc", true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if h.ID == "" || h.Secret != "secret-abc" {
		t.Fatalf("bad create: %+v", h)
	}
	// List 应隐藏 secret
	list := d.List()
	if len(list) != 1 {
		t.Fatalf("list: %d", len(list))
	}
	if list[0].Secret != "" {
		t.Fatalf("List should not expose secret, got %q", list[0].Secret)
	}

	// Update name + enabled=false
	newName := "renamed"
	h2, err := d.Update(h.ID, newName, "", "", boolPtr(false))
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if h2.Name != newName || h2.Enabled {
		t.Fatalf("update result wrong: %+v", h2)
	}

	if err := d.Delete(h.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := d.Get(h.ID); err == nil {
		t.Fatal("get after delete should fail")
	}
}

func TestDispatchWithHTTPServer(t *testing.T) {
	// 假目标服务器：检查 status / 签名
	var recvBody []byte
	var recvSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recvBody = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(recvBody)
		recvSig = r.Header.Get("X-DSH-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewDispatcher()
	defer d.Close()
	h, err := d.Create("h", srv.URL, "my-secret", true)
	if err != nil {
		t.Fatal(err)
	}

	body := `{"event":"test","ts":1}`
	del, _ := d.Dispatch(context.Background(), h.ID, body)
	if !del.OK || del.StatusCode != 200 {
		t.Fatalf("dispatch result: %+v", del)
	}
	if !VerifySignature("my-secret", []byte(body), recvSig) {
		t.Fatalf("signature header missing or wrong: %q", recvSig)
	}
}

func TestDispatchBadURL(t *testing.T) {
	d := NewDispatcher()
	defer d.Close()
	h, err := d.Create("h", "http://127.0.0.1:1/dead", "", true)
	if err != nil {
		t.Fatal(err)
	}
	del, err := d.Dispatch(context.Background(), h.ID, `{}`)
	if err != nil {
		t.Fatalf("dispatch err: %v", err)
	}
	if del.OK {
		t.Fatal("dead URL should fail")
	}
	if del.StatusCode != 0 {
		t.Fatalf("status = %d", del.StatusCode)
	}
}

func TestListDeliveries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	d := NewDispatcher()
	defer d.Close()
	h, _ := d.Create("h", srv.URL, "", true)
	for i := 0; i < 3; i++ {
		d.Dispatch(context.Background(), h.ID, `{"i":1}`)
	}
	dels, err := d.ListDeliveries(h.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(dels) != 3 {
		t.Fatalf("want 3, got %d", len(dels))
	}
	for _, d := range dels {
		if !d.OK || d.Attempt != 1 {
			t.Fatalf("delivery wrong: %+v", d)
		}
	}
}

func TestWorkerRetryDisabled(t *testing.T) {
	d := NewDispatcher()
	defer d.Close()
	h, _ := d.Create("h", "http://127.0.0.1:1/x", "", false) // disabled
	d.queue <- deliveryTask{webhookID: h.ID, payload: "{}", attempt: 1}
	// 等 50ms；disabled 时 worker 直接 return；不 panic。
	time.Sleep(50 * time.Millisecond)
	dels, _ := d.ListDeliveries(h.ID, 50)
	if len(dels) != 0 {
		t.Fatalf("disabled hook should not record delivery, got %d", len(dels))
	}
}

func boolPtr(b bool) *bool { return &b }
