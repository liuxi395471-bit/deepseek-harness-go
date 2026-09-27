package auth

import (
	"strings"
	"testing"
	"time"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	const pw = "CorrectHorseBatteryStaple-42"
	h, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !strings.HasPrefix(h, "pbkdf2-sha256$") {
		t.Fatalf("unexpected hash prefix: %s", h)
	}
	if !VerifyPassword(h, pw) {
		t.Fatal("verify correct password failed")
	}
	if VerifyPassword(h, "wrong") {
		t.Fatal("verify wrong password should fail")
	}
}

func TestPBKDF2Deterministic(t *testing.T) {
	salt := []byte("0123456789abcdef")
	h1 := pbkdf2([]byte("test"), salt, 1000, 32)
	h2 := pbkdf2([]byte("test"), salt, 1000, 32)
	if string(h1) != string(h2) {
		t.Fatal("PBKDF2 should be deterministic")
	}
	h3 := pbkdf2([]byte("test"), salt, 1000, 33)
	if len(h3) != 33 {
		t.Fatalf("PBKDF2 keyLen ignored: got len=%d", len(h3))
	}
}

func TestJWTSignAndVerify(t *testing.T) {
	secret := []byte("test-secret-key-32-bytes-long-xxxxx")
	claims := Claims{
		Sub:  "alice",
		Role: "admin",
		Exp:  time.Now().Add(1 * time.Hour).Unix(),
		Iat:  time.Now().Unix(),
	}
	tok, err := SignJWT(secret, claims, time.Hour)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if strings.Count(tok, ".") != 2 {
		t.Fatalf("malformed token: %s", tok)
	}
	got, err := VerifyJWT(secret, tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.Sub != "alice" || got.Role != "admin" {
		t.Fatalf("claims mismatch: %+v", got)
	}
}

func TestJWTExpired(t *testing.T) {
	secret := []byte("test-secret-key-32-bytes-long-xxxxx")
	claims := Claims{
		Sub: "alice", Role: "user",
		Exp: time.Now().Add(-1 * time.Second).Unix(),
		Iat: time.Now().Add(-1 * time.Hour).Unix(),
	}
	tok, err := SignJWT(secret, claims, -time.Second)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := VerifyJWT(secret, tok); err != ErrTokenExpired {
		t.Fatalf("want ErrTokenExpired, got %v", err)
	}
}

func TestJWTTamperedSignature(t *testing.T) {
	secret := []byte("test-secret-key-32-bytes-long-xxxxx")
	tok, _ := SignJWT(secret, Claims{Sub: "alice", Role: "user", Exp: time.Now().Add(time.Hour).Unix()}, time.Hour)
	// Flip a char in the signature segment (last third).
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatal("malformed token")
	}
	sig := parts[2]
	flipped := sig
	if sig[0] == 'A' {
		flipped = "B" + sig[1:]
	} else {
		flipped = "A" + sig[1:]
	}
	tampered := parts[0] + "." + parts[1] + "." + flipped
	if _, err := VerifyJWT(secret, tampered); err != ErrInvalidToken {
		t.Fatalf("tampered should be invalid, got %v", err)
	}
}

func TestJWTDifferentSecret(t *testing.T) {
	tok, _ := SignJWT([]byte("secret-a-32-bytes-paddingxxxxxx"), Claims{Sub: "x", Exp: time.Now().Add(time.Hour).Unix()}, time.Hour)
	if _, err := VerifyJWT([]byte("secret-b-32-bytes-paddingxxxxxx"), tok); err != ErrInvalidToken {
		t.Fatalf("wrong secret should fail, got %v", err)
	}
}

func TestStoreCRUD(t *testing.T) {
	st, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer st.Close()

	h, _ := HashPassword("hunter2")
	id, err := st.Create(User{Username: "alice", PasswordHash: h, Role: "admin"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == 0 {
		t.Fatal("id must be nonzero")
	}

	if _, err := st.Create(User{Username: "alice", PasswordHash: h, Role: "user"}); err != ErrUserExists {
		t.Fatalf("want ErrUserExists, got %v", err)
	}

	u, err := st.FindByUsername("alice")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if u.Role != "admin" || u.PasswordHash == "" {
		t.Fatalf("unexpected user: %+v", u)
	}

	if err := st.TouchLogin(id); err != nil {
		t.Fatalf("touch: %v", err)
	}
	u, _ = st.FindByUsername("alice")
	if u.LastLoginAt.IsZero() {
		t.Fatal("last_login_at should be set")
	}

	n, err := st.Count()
	if err != nil || n != 1 {
		t.Fatalf("count: %d err=%v", n, err)
	}

	users, err := st.List()
	if err != nil || len(users) != 1 || users[0].Username != "alice" {
		t.Fatalf("list: %+v err=%v", users, err)
	}

	if err := st.Delete(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st.FindByUsername("alice"); err != ErrUserNotFound {
		t.Fatalf("want ErrUserNotFound, got %v", err)
	}
}

func TestGenerateSecret(t *testing.T) {
	s1, err := GenerateSecret()
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	if len(s1) != 64 {
		t.Fatalf("hex of 32 bytes = 64 chars, got %d", len(s1))
	}
	s2, _ := GenerateSecret()
	if s1 == s2 {
		t.Fatal("two random secrets collided")
	}
}
