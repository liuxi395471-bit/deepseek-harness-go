package store

import "unicode/utf8"

// utf8RuneCount 返回 s 中的 rune 数。薄封装以保持导入局部化。
func utf8RuneCount(s string) int { return utf8.RuneCountInString(s) }

// truncateRunes 返回 s 的前 n 个 rune。若 s 不足 n 个 rune 则原样返回。
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}
