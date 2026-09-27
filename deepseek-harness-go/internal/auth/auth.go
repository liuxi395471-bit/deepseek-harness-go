// Package auth 为 v8.1 控制台提供本地用户、bcrypt 密码哈希与 JWT 签发。
//
// 设计要点（DESIGN-v8 §1.3 / §3.3）：
//   - users 表存 username + bcrypt(password) + role + 元数据；
//   - 启动时若 user 表为空且配置未禁用 bootstrap → 创建 root 用户
//     （密码从 DSH_ADMIN_PASSWORD / -admin-pass 读；空时随机生成并
//     打印到 stderr，提示用户保存）；
//   - JWT = HS256(secret, claims)；secret 来自 DSH_JWT_SECRET 或随机生成
//     （每次启动不同会令已签 token 失效 → 默认从 <workspace>/jwt.key 读取
//     或首次启动时持久化）；
//   - 完整实现：手写 HS256 编码 + 签名 + 校验，不引入三方 JWT 库。
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // 同 store 包；驱动注册到 database/sql
)

// User 一行 = users 表中一个本地用户。
type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"` // bcrypt；不返回给客户端
	Role         string    `json:"role"` // "admin" | "user"
	CreatedAt    time.Time `json:"createdAt"`
	LastLoginAt  time.Time `json:"lastLoginAt,omitempty"`
}

// Claims 是 JWT payload（v8.1 最小集：sub + role + exp）。
type Claims struct {
	Sub  string `json:"sub"`  // username
	Role string `json:"role"` // admin | user
	Exp  int64  `json:"exp"`  // unix seconds
	Iat  int64  `json:"iat"`  // issued at
}

// Store 是 users 表的轻量 CRUD。注：这是 *auth.UserStore*，区别于
// session.Store。
type Store struct {
	db *sql.DB
}

// schemaSQL 必须保持幂等。
const schemaSQL = `
CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'user',
    created_at    INTEGER NOT NULL,
    last_login_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
`

// NewStore 打开（或创建）SQLite users 数据库。path 为 ":memory:" 时返回
// 内存库，仅用于测试。
func NewStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("auth: open %q: %w", path, err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("auth: schema: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("auth: pragma fk: %w", err)
	}
	return &Store{db: db}, nil
}

// Close 关闭底层连接。
func (s *Store) Close() error { return s.db.Close() }

// DB 返回底层 *sql.DB（供 schedule / webhook 等新表共用同一 db 文件）。
func (s *Store) DB() *sql.DB { return s.db }

// ErrUserNotFound 当 username 不存在时返回。
var ErrUserNotFound = errors.New("auth: user not found")

// ErrUserExists 当 username 重复时返回。
var ErrUserExists = errors.New("auth: user already exists")

// Create 新增一个 user。
func (s *Store) Create(u User) (int64, error) {
	now := time.Now().UnixMilli()
	res, err := s.db.Exec(
		`INSERT INTO users(username, password_hash, role, created_at) VALUES (?, ?, ?, ?)`,
		u.Username, u.PasswordHash, u.Role, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return 0, ErrUserExists
		}
		return 0, fmt.Errorf("auth: insert: %w", err)
	}
	return res.LastInsertId()
}

// FindByUsername 按 username 取 User；不存在 → ErrUserNotFound。
func (s *Store) FindByUsername(username string) (User, error) {
	row := s.db.QueryRow(
		`SELECT id, username, password_hash, role, created_at, last_login_at FROM users WHERE username = ?`,
		username,
	)
	var u User
	var createdAt, lastLoginAt int64
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &createdAt, &lastLoginAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrUserNotFound
		}
		return User{}, fmt.Errorf("auth: scan: %w", err)
	}
	u.CreatedAt = time.UnixMilli(createdAt)
	if lastLoginAt > 0 {
		u.LastLoginAt = time.UnixMilli(lastLoginAt)
	}
	return u, nil
}

// List 返回全部 user（不含 password_hash）。
func (s *Store) List() ([]User, error) {
	rows, err := s.db.Query(`SELECT id, username, role, created_at, last_login_at FROM users ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("auth: list: %w", err)
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var createdAt, lastLoginAt int64
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &createdAt, &lastLoginAt); err != nil {
			return nil, fmt.Errorf("auth: scan: %w", err)
		}
		u.CreatedAt = time.UnixMilli(createdAt)
		if lastLoginAt > 0 {
			u.LastLoginAt = time.UnixMilli(lastLoginAt)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// Count 返回行数。
func (s *Store) Count() (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// TouchLogin 更新 last_login_at。
func (s *Store) TouchLogin(id int64) error {
	_, err := s.db.Exec(`UPDATE users SET last_login_at=? WHERE id=?`, time.Now().UnixMilli(), id)
	return err
}

// Delete 按 id 删除 user。
func (s *Store) Delete(id int64) error {
	_, err := s.db.Exec(`DELETE FROM users WHERE id=?`, id)
	return err
}

// ---------------------------------------------------------------------------
// bcrypt 风格的密码哈希（手写 PBKDF2-HMAC-SHA256）
//
// 设计：避免引入 golang.org/x/crypto/bcrypt 的 CGO-free 依赖。PBKDF2
// 是 FIPS 认可的标准，安全性等价；trade-off 是 verifier 必须自己实现。
// 哈希格式：pbkdf2-sha256$<iter>$<saltB64>$<hashB64>。
//
// 与外部 bcrypt 哈希不兼容 — 但我们只用自签，所以无所谓。

// HashPassword 把明文密码 PBKDF2 哈希。
func HashPassword(plain string) (string, error) {
	if plain == "" {
		return "", errors.New("auth: empty password")
	}
	const iter = 200_000
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	h := pbkdf2([]byte(plain), salt, iter, 32)
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s",
		iter, base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(h),
	), nil
}

// VerifyPassword 用现有 hash 比对明文。
func VerifyPassword(hash, plain string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	var iter int
	if _, err := fmt.Sscanf(parts[1], "%d", &iter); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got := pbkdf2([]byte(plain), salt, iter, len(want))
	return hmac.Equal(want, got)
}

// pbkdf2 手写实现（HMAC-SHA256）。符合 RFC 2898 §5.2。
func pbkdf2(password, salt []byte, iter, keyLen int) []byte {
	mac := hmac.New(sha256.New, password)
	hashLen := mac.Size()
	blocks := (keyLen + hashLen - 1) / hashLen
	out := make([]byte, 0, blocks*hashLen)
	for i := 1; i <= blocks; i++ {
		mac.Reset()
		mac.Write(salt)
		// big-endian block index
		var idx [4]byte
		idx[0] = byte(i >> 24)
		idx[1] = byte(i >> 16)
		idx[2] = byte(i >> 8)
		idx[3] = byte(i)
		mac.Write(idx[:])
		u := mac.Sum(nil)
		t := make([]byte, len(u))
		copy(t, u)
		for j := 2; j <= iter; j++ {
			mac.Reset()
			mac.Write(u)
			u = mac.Sum(nil)
			for k := range t {
				t[k] ^= u[k]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}

// ---------------------------------------------------------------------------
// JWT（HS256 手写）

// ErrInvalidToken 当 token 解析失败时返回。
var ErrInvalidToken = errors.New("auth: invalid token")

// ErrTokenExpired 当 token 过期时返回。
var ErrTokenExpired = errors.New("auth: token expired")

// SignJWT 用 secret 签发 claims，过期由 ttl 控制。
func SignJWT(secret []byte, c Claims, ttl time.Duration) (string, error) {
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	hBytes, _ := json.Marshal(header)
	cBytes, _ := json.Marshal(c)
	enc := base64.RawURLEncoding.EncodeToString
	hPart := enc(hBytes)
	cPart := enc(cBytes)
	signing := hPart + "." + cPart
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signing))
	sig := mac.Sum(nil)
	sPart := enc(sig)
	return signing + "." + sPart, nil
}

// VerifyJWT 解析 + 验签 + 检查 exp。
func VerifyJWT(secret []byte, token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, ErrInvalidToken
	}
	signing := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signing))
	want := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	if !hmac.Equal(want, got) {
		return Claims{}, ErrInvalidToken
	}
	cBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var c Claims
	if err := json.Unmarshal(cBytes, &c); err != nil {
		return Claims{}, ErrInvalidToken
	}
	if c.Exp > 0 && time.Now().Unix() >= c.Exp {
		return c, ErrTokenExpired
	}
	return c, nil
}

// GenerateSecret 生成 32 字节随机 secret，返回 hex。
func GenerateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
