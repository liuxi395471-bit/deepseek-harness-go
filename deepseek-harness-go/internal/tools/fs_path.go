package tools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolveInWorkspace 对 p（相对 root 或绝对路径）做规范化，并在
// 符号链接解析之后验证结果位于 root 之内。如果文件尚不存在（写入
// 场景），解析出的父目录也必须位于 root 之内。
//
// 所有路径均以绝对、规范化后的形式返回。
func resolveInWorkspace(root, p string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("workspace root not configured")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("workspace root: %w", err)
	}
	absRoot = filepath.Clean(absRoot)

	candidate := p
	if candidate == "" {
		return "", errors.New("path is required")
	}
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(absRoot, candidate)
	}
	clean := filepath.Clean(candidate)

	// EvalSymlinks 在路径不存在时返回 ErrNotExist；这对写入路径来说是
	// 可接受的。任何其他错误都会向上传播。
	real, err := filepath.EvalSymlinks(clean)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("resolve %q: %w", p, err)
	}
	if err != nil {
		real = clean
	}

	// 若 real 不存在则向上回溯：对最长的已存在祖先重新做符号链接
	// 解析，再拼接剩余后缀。这样可以捕捉一种情况：工作区内路径
	// 经由某个更深层级确实存在的中间符号链接指向工作区之外。
	if real == clean {
		if parent, tail := existingParent(clean); parent != "" {
			resolvedParent, perr := filepath.EvalSymlinks(parent)
			if perr == nil {
				real = filepath.Join(resolvedParent, tail)
			}
		}
	}

	rel, err := filepath.Rel(absRoot, real)
	if err != nil {
		return "", fmt.Errorf("path escapes workspace: %s", p)
	}
	// 拒绝以下情况：
	//   rel == ".."            → 在父目录层级之外
	//   rel == "."             → real == root，对工具而言视为外部（我们不希望把根目录本身当作文件写入）
	//   strings.HasPrefix(rel, ".."+sep)  → 经由更深的路径逃逸
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace: %s", p)
	}
	return real, nil
}

// existingParent 返回 path 最长的已存在祖先目录及剩余后缀。
// 若没有任何祖先存在，则返回 "" 和完整路径。
func existingParent(path string) (string, string) {
	cur := path
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", path
		}
		if _, err := os.Stat(parent); err == nil {
			rel, _ := filepath.Rel(parent, path)
			return parent, rel
		}
		cur = parent
	}
}
