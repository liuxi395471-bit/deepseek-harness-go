package console

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"deepseek-harness-go/internal/auth"
)

// contextKey 是中间件注入 user 信息的 ctx key（避免与其它包冲突）。
type contextKey string

const (
	ctxUserKey    contextKey = "console.user"
	ctxAuthMethod contextKey = "console.auth" // "jwt" | "token"
)

// AuthBackend 是 ConsoleServer 鉴权后端的抽象。Console 不直接依赖
// internal/auth.Store，方便测试时替换为内存实现。
type AuthBackend interface {
	// VerifyPassword 校验用户名/密码；返回 user。
	VerifyPassword(ctx context.Context, username, password string) (auth.User, error)
	// GetUser 取 user（不含 password_hash）。
	GetUser(ctx context.Context, username string) (auth.User, error)
	// IssueToken 签发 JWT（subject = username, role=user.Role, ttl=24h）。
	IssueToken(ctx context.Context, username string) (token string, expiresAt time.Time, err error)
	// IssueTokenWithTTL 与 IssueToken 同，但允许调用方指定 ttl。
	IssueTokenWithTTL(ctx context.Context, username string, ttl time.Duration) (string, time.Time, error)
	// VerifyToken 解析 + 验签 JWT；返回 claims。
	VerifyToken(ctx context.Context, token string) (auth.Claims, error)
	// TouchLogin 更新 last_login_at。
	TouchLogin(ctx context.Context, id int64) error
	// ListUsers 列全部 user。
	ListUsers(ctx context.Context) ([]auth.User, error)
	// CreateUser 新增 user（admin only）。
	CreateUser(ctx context.Context, username, password, role string) (auth.User, error)
	// DeleteUser 按 id 删除。
	DeleteUser(ctx context.Context, id int64) error
}

// Blacklist 是已注销 / 被禁用的 token 集合（内存 LRU；TTL 到期自动剔除）。
type Blacklist struct {
	mu    sync.Mutex
	items map[string]time.Time // tokenSig (hex) → expiry
}

func NewBlacklist() *Blacklist {
	return &Blacklist{items: make(map[string]time.Time)}
}

// Revoke 把 token 的签名（claims.exp 用作 expiry）加入黑名单。
func (b *Blacklist) Revoke(token string, exp time.Time) {
	if token == "" {
		return
	}
	sig := tokenSig(token)
	b.mu.Lock()
	defer b.mu.Unlock()
	// 清理过期
	for k, t := range b.items {
		if time.Now().After(t) {
			delete(b.items, k)
		}
	}
	b.items[sig] = exp
}

// IsRevoked 判断 token 是否在黑名单。
func (b *Blacklist) IsRevoked(token string) bool {
	sig := tokenSig(token)
	b.mu.Lock()
	defer b.mu.Unlock()
	exp, ok := b.items[sig]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(b.items, sig)
		return false
	}
	return true
}

// tokenSig 返回 token 末 16 字节 hex 作为签名（避免存全 token）。
func tokenSig(t string) string {
	if len(t) < 16 {
		return t
	}
	return t[len(t)-16:]
}

// AuthAdapter 是 AuthBackend 基于 internal/auth.Store 的实现。
type AuthAdapter struct {
	Store       *auth.Store
	JWTSecret   []byte
	TokenTTL    time.Duration // 默认 24h
}

// NewAuthAdapter 构造。
func NewAuthAdapter(store *auth.Store, secret []byte, ttl time.Duration) *AuthAdapter {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &AuthAdapter{Store: store, JWTSecret: secret, TokenTTL: ttl}
}

// VerifyPassword 实现 AuthBackend。
func (a *AuthAdapter) VerifyPassword(_ context.Context, username, password string) (auth.User, error) {
	u, err := a.Store.FindByUsername(username)
	if err != nil {
		// 用户不存在也返回通用错误，避免用户名枚举
		return auth.User{}, errors.New("invalid credentials")
	}
	if !auth.VerifyPassword(u.PasswordHash, password) {
		return auth.User{}, errors.New("invalid credentials")
	}
	u.PasswordHash = ""
	return u, nil
}

// GetUser 实现 AuthBackend。
func (a *AuthAdapter) GetUser(_ context.Context, username string) (auth.User, error) {
	u, err := a.Store.FindByUsername(username)
	if err != nil {
		return auth.User{}, err
	}
	u.PasswordHash = ""
	return u, nil
}

// IssueToken 实现 AuthBackend。
func (a *AuthAdapter) IssueToken(_ context.Context, username string) (string, time.Time, error) {
	return a.IssueTokenWithTTL(context.Background(), username, a.TokenTTL)
}

// IssueTokenWithTTL 签发 JWT，subject = username。
func (a *AuthAdapter) IssueTokenWithTTL(_ context.Context, username string, ttl time.Duration) (string, time.Time, error) {
	u, err := a.Store.FindByUsername(username)
	if err != nil {
		return "", time.Time{}, err
	}
	exp := time.Now().Add(ttl)
	tok, err := auth.SignJWT(a.JWTSecret, auth.Claims{
		Sub:  u.Username,
		Role: u.Role,
		Exp:  exp.Unix(),
		Iat:  time.Now().Unix(),
	}, ttl)
	if err != nil {
		return "", time.Time{}, err
	}
	_ = a.Store.TouchLogin(u.ID)
	return tok, exp, nil
}

// VerifyToken 实现 AuthBackend。
func (a *AuthAdapter) VerifyToken(_ context.Context, token string) (auth.Claims, error) {
	return auth.VerifyJWT(a.JWTSecret, token)
}

// TouchLogin 实现 AuthBackend。
func (a *AuthAdapter) TouchLogin(_ context.Context, id int64) error {
	return a.Store.TouchLogin(id)
}

// ListUsers 实现 AuthBackend。
func (a *AuthAdapter) ListUsers(_ context.Context) ([]auth.User, error) {
	us, err := a.Store.List()
	if err != nil {
		return nil, err
	}
	for i := range us {
		us[i].PasswordHash = ""
	}
	return us, nil
}

// CreateUser 实现 AuthBackend。
func (a *AuthAdapter) CreateUser(_ context.Context, username, password, role string) (auth.User, error) {
	if username == "" || password == "" {
		return auth.User{}, errors.New("username and password required")
	}
	if role == "" {
		role = "user"
	}
	if role != "admin" && role != "user" {
		return auth.User{}, errors.New("role must be admin or user")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return auth.User{}, err
	}
	if _, err := a.Store.Create(auth.User{Username: username, PasswordHash: hash, Role: role}); err != nil {
		return auth.User{}, err
	}
	return a.Store.FindByUsername(username)
}

// DeleteUser 实现 AuthBackend。
func (a *AuthAdapter) DeleteUser(_ context.Context, id int64) error {
	return a.Store.Delete(id)
}

// ---------------------------------------------------------------------------
// HTTP handlers

// LoginRequest 是 POST /auth/login body。
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse 是 POST /auth/login 200。
type LoginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
	User      auth.User `json:"user"`
}

// handleLogin 校验用户密码并签发 JWT。
func (s *ConsoleServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.Deps.Auth == nil {
		writeError(w, http.StatusServiceUnavailable, "auth backend not configured")
		return
	}
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password required")
		return
	}
	u, err := s.Deps.Auth.VerifyPassword(r.Context(), req.Username, req.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	tok, exp, err := s.Deps.Auth.IssueToken(r.Context(), u.Username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "issue token: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, LoginResponse{Token: tok, ExpiresAt: exp, User: u})
}

// handleLogout 把当前 token 加入黑名单。
func (s *ConsoleServer) handleLogout(w http.ResponseWriter, r *http.Request) {
	if s.Deps.Auth == nil {
		writeError(w, http.StatusServiceUnavailable, "auth backend not configured")
		return
	}
	if s.Blacklist == nil {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		tok := strings.TrimPrefix(h, "Bearer ")
		// 解 token 取 exp
		claims, err := s.Deps.Auth.VerifyToken(r.Context(), tok)
		if err == nil {
			exp := time.Unix(claims.Exp, 0)
			s.Blacklist.Revoke(tok, exp)
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleMe 返回当前用户信息。
func (s *ConsoleServer) handleMe(w http.ResponseWriter, r *http.Request) {
	if s.Deps.Auth == nil {
		writeError(w, http.StatusServiceUnavailable, "auth backend not configured")
		return
	}
	claims := claimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "no user context")
		return
	}
	u, err := s.Deps.Auth.GetUser(r.Context(), claims.Sub)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// CreateUserRequest 是 PUT /auth/users body。
type CreateUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// handleCreateUser 新增 user（admin only）。
func (s *ConsoleServer) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if s.Deps.Auth == nil {
		writeError(w, http.StatusServiceUnavailable, "auth backend not configured")
		return
	}
	if !isAdmin(r) {
		writeError(w, http.StatusForbidden, "admin required")
		return
	}
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	u, err := s.Deps.Auth.CreateUser(r.Context(), req.Username, req.Password, req.Role)
	if err != nil {
		if errors.Is(err, auth.ErrUserExists) {
			writeError(w, http.StatusConflict, "user already exists")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

// handleListUsers 列全部 user（admin only）。
func (s *ConsoleServer) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if s.Deps.Auth == nil {
		writeError(w, http.StatusServiceUnavailable, "auth backend not configured")
		return
	}
	if !isAdmin(r) {
		writeError(w, http.StatusForbidden, "admin required")
		return
	}
	us, err := s.Deps.Auth.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, us)
}

// handleDeleteUser 删除 user（admin only）。id 通过 query 参数 ?id=N 传入。
func (s *ConsoleServer) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if s.Deps.Auth == nil {
		writeError(w, http.StatusServiceUnavailable, "auth backend not configured")
		return
	}
	if !isAdmin(r) {
		writeError(w, http.StatusForbidden, "admin required")
		return
	}
	q := r.URL.Query().Get("id")
	if q == "" {
		writeError(w, http.StatusBadRequest, "id required (?id=N)")
		return
	}
	id, err := strconv.ParseInt(q, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "id must be positive integer")
		return
	}
	if err := s.Deps.Auth.DeleteUser(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------------------
// 中间件改造：bearerAuth 优先尝试 JWT，fallback 到 static token。

// authedHandler 包装 next handler，注入 user context。
func (s *ConsoleServer) authedHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if i := strings.Index(path, "/api/v1/console"); i == 0 {
			path = strings.TrimPrefix(path, "/api/v1/console")
			if path == "" {
				path = "/"
			}
		}
		// /health 永远 bypass
		if strings.HasPrefix(path, "/health") {
			next.ServeHTTP(w, r)
			return
		}
		// /auth/login 永远 bypass（用密码换 token）
		if path == "/auth/login" {
			next.ServeHTTP(w, r)
			return
		}

		// v8.0 兼容：AuthToken 与 JWTSecret 都为空 → 503（防止误启动无鉴权）
		if s.cfg.AuthToken == "" && len(s.cfg.JWTSecret) == 0 && s.Deps.Auth == nil {
			writeError(w, http.StatusServiceUnavailable, "console: auth not configured")
			return
		}

		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		tok := strings.TrimPrefix(h, "Bearer ")

		// 1) JWT 路径
		if s.Deps.Auth != nil {
			if s.Blacklist != nil && s.Blacklist.IsRevoked(tok) {
				writeError(w, http.StatusUnauthorized, "token revoked")
				return
			}
			claims, err := s.Deps.Auth.VerifyToken(r.Context(), tok)
			if err == nil {
				ctx := r.Context()
				ctx = context.WithValue(ctx, ctxUserKey, claims)
				ctx = context.WithValue(ctx, ctxAuthMethod, "jwt")
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			if errors.Is(err, auth.ErrTokenExpired) {
				writeError(w, http.StatusUnauthorized, "token expired")
				return
			}
			// JWT 解析失败 → 尝试 static token fallback
		}

		// 2) Static token fallback（兼容 v8.0 DSH_SERVER_AUTH_TOKEN）
		if s.cfg.AuthToken != "" {
			if subtle.ConstantTimeCompare([]byte(tok), []byte(s.cfg.AuthToken)) == 1 {
				ctx := r.Context()
				ctx = context.WithValue(ctx, ctxAuthMethod, "token")
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		writeError(w, http.StatusUnauthorized, "invalid bearer token")
	})
}

// claimsFromContext 从 ctx 取 claims。
func claimsFromContext(ctx context.Context) *auth.Claims {
	v, _ := ctx.Value(ctxUserKey).(auth.Claims)
	if v.Sub == "" {
		return nil
	}
	return &v
}

// isAdmin 判定当前 ctx 用户是否 admin（static token 也算 admin）。
func isAdmin(r *http.Request) bool {
	if m, _ := r.Context().Value(ctxAuthMethod).(string); m == "token" {
		return true
	}
	c := claimsFromContext(r.Context())
	if c == nil {
		return false
	}
	return c.Role == "admin"
}
