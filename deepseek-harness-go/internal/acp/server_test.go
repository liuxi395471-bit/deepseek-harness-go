package acp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"deepseek-harness-go/internal/storage"
)

// fakeTaskSubmitter 是测试用 TaskSubmitter。
type fakeTaskSubmitter struct {
	mu       sync.Mutex
	tasks    map[string]taskHandle
	nextID   int
	submitOK bool
}

func newFakeTasks() *fakeTaskSubmitter {
	return &fakeTaskSubmitter{tasks: make(map[string]taskHandle), submitOK: true}
}

func (f *fakeTaskSubmitter) Submit(_ context.Context, input string) (taskHandle, error) {
	if !f.submitOK {
		return taskHandle{}, errors.New("submit disabled")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := "task-" + itoa(f.nextID)
	h := taskHandle{ID: id, State: "pending", Content: input}
	f.tasks[id] = h
	return h, nil
}

func (f *fakeTaskSubmitter) Get(_ context.Context, id string) (taskHandle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok {
		return taskHandle{}, errors.New("not found")
	}
	return t, nil
}

func (f *fakeTaskSubmitter) Cancel(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok {
		return errors.New("not found")
	}
	t.State = "canceled"
	f.tasks[id] = t
	return nil
}

func (f *fakeTaskSubmitter) List(_ context.Context) ([]taskHandle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]taskHandle, 0, len(f.tasks))
	for _, t := range f.tasks {
		out = append(out, t)
	}
	return out, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

func TestACP_CreateAndSendAndList(t *testing.T) {
	store := storage.NewMemoryStorage()
	tasks := newFakeTasks()
	srv := NewServer(tasks, store, "")

	sess, err := srv.CreateSession(context.Background(), CreateSessionRequest{Profile: "p"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sess.SessionID == "" {
		t.Fatal("empty session id")
	}

	send, err := srv.Send(context.Background(), SendRequest{
		SessionID: sess.SessionID,
		Content:   "hello world",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if send.TaskID == "" || send.State != "pending" {
		t.Errorf("send = %+v", send)
	}

	list, err := srv.List(context.Background(), ListRequest{SessionID: sess.SessionID})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Tasks) != 1 || list.Tasks[0].ID != send.TaskID {
		t.Errorf("list = %+v", list)
	}
}

func TestACP_Cancel(t *testing.T) {
	tasks := newFakeTasks()
	srv := NewServer(tasks, storage.NewMemoryStorage(), "")
	sess, _ := srv.CreateSession(context.Background(), CreateSessionRequest{})
	send, _ := srv.Send(context.Background(), SendRequest{SessionID: sess.SessionID, Content: "x"})

	c, err := srv.Cancel(context.Background(), CancelRequest{SessionID: sess.SessionID, TaskID: send.TaskID})
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if !c.OK || c.State != "canceled" {
		t.Errorf("cancel = %+v", c)
	}
}

func TestACP_HTTPHandler(t *testing.T) {
	tasks := newFakeTasks()
	srv := NewServer(tasks, storage.NewMemoryStorage(), "secret")
	h := srv.Handler()

	mkReq := func(method, path string, body any) *http.Request {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, strings.NewReader(string(b)))
		r.Header.Set("Content-Type", "application/json")
		return r
	}

	// unauthorized
	r1 := mkReq("POST", "/acp/session/create", CreateSessionRequest{})
	r1.Header.Set("Authorization", "Bearer wrong")
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, r1)
	if w1.Code != http.StatusUnauthorized {
		t.Errorf("unauth status = %d", w1.Code)
	}

	// authorized
	r2 := mkReq("POST", "/acp/session/create", CreateSessionRequest{Owner: "u"})
	r2.Header.Set("Authorization", "Bearer secret")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s", w2.Code, w2.Body.String())
	}
	var cr CreateSessionResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &cr); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// send
	r3 := mkReq("POST", "/acp/session/send", SendRequest{SessionID: cr.SessionID, Content: "hi"})
	r3.Header.Set("Authorization", "Bearer secret")
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, r3)
	if w3.Code != http.StatusOK {
		t.Errorf("send status = %d", w3.Code)
	}

	// list
	r4 := mkReq("POST", "/acp/session/list", ListRequest{SessionID: cr.SessionID})
	r4.Header.Set("Authorization", "Bearer secret")
	w4 := httptest.NewRecorder()
	h.ServeHTTP(w4, r4)
	if w4.Code != http.StatusOK {
		t.Errorf("list status = %d", w4.Code)
	}

	// permission
	r5 := mkReq("POST", "/acp/permission/decide", map[string]any{"id": "p1", "approve": true})
	r5.Header.Set("Authorization", "Bearer secret")
	w5 := httptest.NewRecorder()
	h.ServeHTTP(w5, r5)
	if w5.Code != http.StatusOK {
		t.Errorf("permission status = %d", w5.Code)
	}

	// unknown path
	r6 := mkReq("POST", "/unknown", nil)
	r6.Header.Set("Authorization", "Bearer secret")
	w6 := httptest.NewRecorder()
	h.ServeHTTP(w6, r6)
	if w6.Code != http.StatusNotFound {
		t.Errorf("unknown status = %d", w6.Code)
	}
}

func TestACP_SessionNotFound(t *testing.T) {
	srv := NewServer(newFakeTasks(), storage.NewMemoryStorage(), "")
	_, err := srv.Send(context.Background(), SendRequest{SessionID: "ghost", Content: "x"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v", err)
	}
	_, err = srv.List(context.Background(), ListRequest{SessionID: "ghost"})
	if err == nil {
		t.Error("expected error")
	}
}
