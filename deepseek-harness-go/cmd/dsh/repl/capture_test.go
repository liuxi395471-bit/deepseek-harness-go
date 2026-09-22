package repl

import (
	"bytes"
	"io"
	"os"
	"sync"
	"testing"
)

// captureStd 在执行 fn 期间替换 os.Stdout 和 os.Stderr，并返回此期间
// 写入的内容。用于断言 REPL 的输出，同时避免其泄漏到测试运行器。
//
// 顺序很重要：
//
//	先运行 fn()，再关闭 writer，让读取 goroutine 能排空各自管道。
//	读取者通过 WaitGroup 通知完成。之后才恢复 os.Stdout/Stderr。
//
// 如果 fn 死锁，测试将通过 -timeout 超时。
func captureStd(t *testing.T, fn func()) (string, string) {
	t.Helper()

	origOut, origErr := os.Stdout, os.Stderr

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	os.Stdout = outW
	os.Stderr = errW

	var outBuf, errBuf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&outBuf, outR)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&errBuf, errR)
	}()

	// 在替换后的文件描述符下运行函数。
	fn()

	// 关闭 writer，让读取 goroutine 看到 EOF 并退出。
	_ = outW.Close()
	_ = errW.Close()

	// 等待两个读取者都排空完毕。
	wg.Wait()

	// 恢复原始的 stdout/stderr。
	os.Stdout = origOut
	os.Stderr = origErr
	_ = outR.Close()
	_ = errR.Close()

	return outBuf.String(), errBuf.String()
}
