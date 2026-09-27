package node

import (
	"strings"
	"testing"
)

// 仅跑不需要 Node 的部分（安全校验）。
// 真正的 roundtrip 由 debug_bridge 工具手动验证；
// 集成测试在 CI 装 Node 后再补。

func TestLoad_RejectsEscapingMain(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	_, err := Load(root, outside)
	if err == nil {
		t.Fatal("expected error for pkg outside installRoot")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("err = %v", err)
	}
}

func TestLoad_NonExistentPkg(t *testing.T) {
	root := t.TempDir()
	_, err := Load(root, root+"/no-such-pkg")
	if err == nil {
		t.Fatal("expected error for nonexistent pkg dir")
	}
}
