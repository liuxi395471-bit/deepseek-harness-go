//go:build !embed_dsh
// +build !embed_dsh

package main

// embeddedDSH 空字节数组；不带 -tags embed_dsh 时不嵌入二进制。
// 启动器优先用同目录 dsh.exe 或 -dsh 指定的路径。
var embeddedDSH []byte
