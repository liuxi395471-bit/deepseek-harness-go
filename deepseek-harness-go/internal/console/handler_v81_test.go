package console

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"deepseek-harness-go/internal/auth"
)

// stubAuth is an in-memory AuthBackend for handler tests.
type stubAuth struct {
	store *auth.Store
	sec   []byte
}

func newStubAuth(t *testing.T) *stubAuth {
	t.Helper()
	st, err := auth.NewStore(":memory:")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	h, _ := auth.HashPassword("pw-1")
	if _, err := st.Create(auth.User{Username: "alice", PasswordHash: h, Role: "admin"}); err != nil {
		t.Fatalf("create alice: %v", err)
	}
	h2, _ := auth.HashPassword("pw-2")
	if _, err := st.Create(auth.User{Username: "bob", PasswordHash: h2, Role: "user"}); err != nil {
		t.Fatalf("create bob: %v", err)
	}
	return &stubAuth{store: st, sec: []byte("0123456789abcdef0123456789abcdef")}
}

func (s *stubAuth) asAdapter() *AuthAdapter {
	return NewAuthAdapter(s.store, s.sec, time.Hour)
}

func (s *stubAuth) backend() AuthBackend { return s.asAdapter() }

func (s *stubAuth) blacklist() *Blacklist { return NewBlacklist() }

func (s *stubAuth) deps() Deps {
	return Deps{Auth: s.backend()}
}

// TestLoginMeLogout verifies login + me + logout flow.
func TestLoginMeLogout(t *testing.T) {
	stub := newStubAuth(t)
	srv := New(Config{}, stub.deps())
	srv.SetSPAFS(emptyFS{})
	srv.Blacklist = stub.blacklist()
	mux := srv.Handler().(*http.ServeMux)

	// 1) login as alice
	body, _ := json.Marshal(LoginRequest{Username: "alice", Password: "pw-1"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/console/auth/login", bytes.NewReader(body))
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login alice: code=%d body=%s", rec.Code, rec.Body.String())
	}
	var lr LoginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &lr); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if lr.Token == "" || lr.User.Username != "alice" {
		t.Fatalf("bad login response: %+v", lr)
	}
	if lr.User.Role != "admin" {
		t.Fatalf("alice should be admin: %+v", lr.User)
	}

	// 2) me with token
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/console/auth/me", nil)
	req2.Header.Set("Authorization", "Bearer "+lr.Token)
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("me: code=%d body=%s", rec2.Code, rec2.Body.String())
	}

	// 3) logout
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/console/auth/logout", nil)
	req3.Header.Set("Authorization", "Bearer "+lr.Token)
	mux.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("logout: code=%d", rec3.Code)
	}

	// 4) me with revoked token
	rec4 := httptest.NewRecorder()
	req4 := httptest.NewRequest(http.MethodGet, "/api/v1/console/auth/me", nil)
	req4.Header.Set("Authorization", "Bearer "+lr.Token)
	mux.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusUnauthorized {
		t.Fatalf("revoked me should be 401, got %d", rec4.Code)
	}
}

func TestLoginBadPassword(t *testing.T) {
	stub := newStubAuth(t)
	srv := New(Config{}, stub.deps())
	srv.SetSPAFS(emptyFS{})
	mux := srv.Handler().(*http.ServeMux)
	body, _ := json.Marshal(LoginRequest{Username: "alice", Password: "wrong"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/console/auth/login", bytes.NewReader(body))
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad password: code=%d", rec.Code)
	}
}

func TestLoginUnknownUser(t *testing.T) {
	stub := newStubAuth(t)
	srv := New(Config{}, stub.deps())
	srv.SetSPAFS(emptyFS{})
	mux := srv.Handler().(*http.ServeMux)
	body, _ := json.Marshal(LoginRequest{Username: "ghost", Password: "x"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/console/auth/login", bytes.NewReader(body))
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown user: code=%d", rec.Code)
	}
}

func TestCreateAndListUsers_AdminOnly(t *testing.T) {
	stub := newStubAuth(t)
	srv := New(Config{}, stub.deps())
	srv.SetSPAFS(emptyFS{})
	mux := srv.Handler().(*http.ServeMux)

	// Login as admin alice
	body, _ := json.Marshal(LoginRequest{Username: "alice", Password: "pw-1"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/console/auth/login", bytes.NewReader(body)))
	var lr LoginResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &lr)

	// List users (admin)
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/console/auth/users", nil)
	req2.Header.Set("Authorization", "Bearer "+lr.Token)
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("list users: code=%d body=%s", rec2.Code, rec2.Body.String())
	}
	var users []auth.User
	_ = json.Unmarshal(rec2.Body.Bytes(), &users)
	if len(users) < 2 {
		t.Fatalf("want >=2 users, got %d", len(users))
	}

	// Create new user
	body2, _ := json.Marshal(CreateUserRequest{Username: "carol", Password: "pw-3", Role: "user"})
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPut, "/api/v1/console/auth/users", bytes.NewReader(body2))
	req3.Header.Set("Authorization", "Bearer "+lr.Token)
	mux.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusCreated {
		t.Fatalf("create user: code=%d body=%s", rec3.Code, rec3.Body.String())
	}

	// Login as non-admin bob → list should be 403
	body4, _ := json.Marshal(LoginRequest{Username: "bob", Password: "pw-2"})
	rec4 := httptest.NewRecorder()
	mux.ServeHTTP(rec4, httptest.NewRequest(http.MethodPost, "/api/v1/console/auth/login", bytes.NewReader(body4)))
	var lrBob LoginResponse
	_ = json.Unmarshal(rec4.Body.Bytes(), &lrBob)

	rec5 := httptest.NewRecorder()
	req5 := httptest.NewRequest(http.MethodGet, "/api/v1/console/auth/users", nil)
	req5.Header.Set("Authorization", "Bearer "+lrBob.Token)
	mux.ServeHTTP(rec5, req5)
	if rec5.Code != http.StatusForbidden {
		t.Fatalf("non-admin list users: code=%d", rec5.Code)
	}
}

func TestCreateUserDuplicate(t *testing.T) {
	stub := newStubAuth(t)
	srv := New(Config{}, stub.deps())
	srv.SetSPAFS(emptyFS{})
	mux := srv.Handler().(*http.ServeMux)

	body, _ := json.Marshal(LoginRequest{Username: "alice", Password: "pw-1"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/console/auth/login", bytes.NewReader(body)))
	var lr LoginResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &lr)

	body2, _ := json.Marshal(CreateUserRequest{Username: "alice", Password: "x", Role: "user"})
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPut, "/api/v1/console/auth/users", bytes.NewReader(body2))
	req2.Header.Set("Authorization", "Bearer "+lr.Token)
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("duplicate user: code=%d body=%s", rec2.Code, rec2.Body.String())
	}
}

func TestJWTFallbackToStaticToken(t *testing.T) {
	// Console with only static token (no JWT secret / no Auth backend)
	// should still accept the static token.
	srv := New(Config{AuthToken: "static-secret"}, Deps{})
	srv.SetSPAFS(emptyFS{})
	mux := srv.Handler().(*http.ServeMux)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/console/sessions", nil)
	req.Header.Set("Authorization", "Bearer static-secret")
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("static token should be accepted, got 401")
	}
}

func TestHealthBypass(t *testing.T) {
	srv := New(Config{}, Deps{})
	srv.SetSPAFS(emptyFS{})
	mux := srv.Handler().(*http.ServeMux)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/console/health", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health: code=%d", rec.Code)
	}
}

func TestExportAuditCSVFormat(t *testing.T) {
	ma := &mockAuditBackend{}
	srv := New(Config{AuthToken: "tok"}, Deps{Audit: ma})
	srv.SetSPAFS(emptyFS{})
	mux := srv.Handler().(*http.ServeMux)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/console/audit/export?format=csv&limit=10", nil)
	req.Header.Set("Authorization", "Bearer tok")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("csv export: code=%d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("content-type = %s, want text/csv", ct)
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.HasPrefix(string(body), "ts,") {
		t.Fatalf("expected csv header, got %q", string(body))
	}
}

// TestJWTRequiredAfterConfig verifies request without bearer is rejected when auth is configured.
func TestBearerRequired(t *testing.T) {
	stub := newStubAuth(t)
	srv := New(Config{}, stub.deps())
	srv.SetSPAFS(emptyFS{})
	mux := srv.Handler().(*http.ServeMux)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/console/sessions", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: code=%d", rec.Code)
	}
}

// TestJWTMalformed verifies garbage token is rejected.
func TestJWTMalformed(t *testing.T) {
	stub := newStubAuth(t)
	srv := New(Config{}, stub.deps())
	srv.SetSPAFS(emptyFS{})
	mux := srv.Handler().(*http.ServeMux)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/console/sessions", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("garbage token: code=%d", rec.Code)
	}
}

// context import is used by mockAuditBackend; ensure it isn't unused.
var _ = context.Background
