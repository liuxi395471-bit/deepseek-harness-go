//go:build !linux

package sandbox

// checkUnixSensitive 在非 linux 平台为空操作：以 / 开头的路径会被
// filepath.Abs 加上当前盘符，unix 敏感路径语义不适用。
func checkUnixSensitive(string) error { return nil }
