// Package credentials — 凭据抽象（v5 P5-4）。
//
// credentials.go 把 v4 的"env 单源"扩展为可插入式 Provider。
// 一个 Ref 描述一个凭据条目（name + env + file）；调用方按 Ref
// 顺序在 Provider 链上查询。
//
// 设计要点：
//   - 接口：Provider + Ref + Chained；
//   - EnvProvider：仅查环境变量；
//   - FileProvider：JSON 文件 {"<name>":"<value>", ...}；
//   - Chained：按顺序尝试，前者优先；
//   - 向后兼容：env 单源仍可通过 EnvProvider 实现；
//   - 加密 yml / KMS 留给 v6+。
package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
)

// ErrNotFound 在所有 Provider 都查不到时由 Chained.Resolve 返回。
var ErrNotFound = errors.New("credentials: not found")

// Ref 描述一个凭据条目的查询方式。
//
// Name 是人类可读标识（如 "deepseek-api-key"）；
// Env 是环境变量名（如 "DEEPSEEK_API_KEY"）；优先看 Env；
// File 是 JSON 文件路径（按 Name 取 val 字段）；FileProvider 使用。
type Ref struct {
	Name string
	Env  string
	File string
}

// Provider 是凭据来源的契约。
type Provider interface {
	// Resolve 根据 ref 返回明文凭据；找不到返回 ErrNotFound。
	Resolve(ctx context.Context, ref Ref) (string, error)
}

// EnvProvider 只查环境变量。
type EnvProvider struct{}

// NewEnvProvider 构造 EnvProvider。
func NewEnvProvider() *EnvProvider { return &EnvProvider{} }

// Resolve 看 ref.Env 环境变量；空则按 ref.Name 拼接 DSH_<NAME>。
func (e *EnvProvider) Resolve(_ context.Context, ref Ref) (string, error) {
	key := ref.Env
	if key == "" && ref.Name != "" {
		key = "DSH_" + toUpperSnake(ref.Name)
	}
	if key == "" {
		return "", ErrNotFound
	}
	v := os.Getenv(key)
	if v == "" {
		return "", ErrNotFound
	}
	return v, nil
}

// FileProvider 从 JSON 文件读取 {"<name>": "<value>"}。
type FileProvider struct {
	Path string

	mu   sync.RWMutex
	data map[string]string
}

// NewFileProvider 构造 FileProvider，立即读取路径（不存在时 data=nil，
// 所有 Resolve 返回 ErrNotFound）。
func NewFileProvider(path string) (*FileProvider, error) {
	fp := &FileProvider{Path: path, data: make(map[string]string)}
	if err := fp.load(); err != nil {
		return nil, fmt.Errorf("credentials: file %s: %w", path, err)
	}
	return fp, nil
}

// load 重新从 Path 读取 JSON。文件不存在视为合法（data 留空）。
func (f *FileProvider) load() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, err := os.ReadFile(f.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			f.data = nil
			return nil
		}
		return err
	}
	m := make(map[string]string)
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("credentials: parse json: %w", err)
	}
	f.data = m
	return nil
}

// Reload 重新读取文件，便于运行时刷新凭据。
func (f *FileProvider) Reload() error { return f.load() }

// Resolve 按 ref.Name 查表。
func (f *FileProvider) Resolve(_ context.Context, ref Ref) (string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.data == nil || ref.Name == "" {
		return "", ErrNotFound
	}
	v, ok := f.data[ref.Name]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

// Chained 按顺序尝试一组 Provider；前者命中即返回。
type Chained struct {
	Providers []Provider
}

// NewChained 构造 Chained。
func NewChained(providers ...Provider) *Chained {
	return &Chained{Providers: providers}
}

// Resolve 按顺序尝试；全部失败返回 ErrNotFound。
func (c *Chained) Resolve(ctx context.Context, ref Ref) (string, error) {
	for _, p := range c.Providers {
		v, err := p.Resolve(ctx, ref)
		if err == nil {
			return v, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return "", err
		}
	}
	return "", ErrNotFound
}

// toUpperSnake 把 camelCase / kebab-case 转成 UPPER_SNAKE_CASE。
// 例：deepseek-api-key → DEEPSEEK_API_KEY。
func toUpperSnake(s string) string {
	out := make([]byte, 0, len(s)+4)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '-':
			out = append(out, '_')
		case c >= 'a' && c <= 'z':
			out = append(out, c-32)
		case c >= 'A' && c <= 'Z':
			out = append(out, c)
			if i+1 < len(s) && s[i+1] >= 'a' && s[i+1] <= 'z' {
				out = append(out, '_')
			}
		default:
			out = append(out, c)
		}
	}
	return string(out)
}
