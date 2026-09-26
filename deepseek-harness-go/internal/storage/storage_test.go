package storage

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestMemory_CRUD(t *testing.T) {
	m := NewMemoryStorage()

	if _, ok, _ := m.Get("ns", "k"); ok {
		t.Error("empty memory has k")
	}
	if err := m.Set("ns", "k", []byte("v1")); err != nil {
		t.Fatalf("set: %v", err)
	}
	v, ok, _ := m.Get("ns", "k")
	if !ok || string(v) != "v1" {
		t.Errorf("get = %q, %v", v, ok)
	}

	if err := m.Set("ns", "k", []byte("v2")); err != nil {
		t.Fatalf("set 2: %v", err)
	}
	v, _, _ = m.Get("ns", "k")
	if string(v) != "v2" {
		t.Errorf("get 2 = %q", v)
	}

	if err := m.Delete("ns", "k"); err != nil {
		t.Fatalf("del: %v", err)
	}
	if _, ok, _ := m.Get("ns", "k"); ok {
		t.Error("after delete, k should be gone")
	}
}

func TestMemory_Namespaces(t *testing.T) {
	m := NewMemoryStorage()
	m.Set("a", "1", []byte("x"))
	m.Set("a", "2", []byte("y"))
	m.Set("b", "1", []byte("z"))
	ns := m.Namespaces()
	sort.Strings(ns)
	if !reflect.DeepEqual(ns, []string{"a", "b"}) {
		t.Errorf("ns = %v", ns)
	}
	list, _ := m.List("a")
	if !reflect.DeepEqual(list, []string{"1", "2"}) {
		t.Errorf("a keys = %v", list)
	}
}

func TestFile_CRUD(t *testing.T) {
	dir, err := os.MkdirTemp("", "storage-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	f, err := NewFileStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Set("ns", "k", []byte("hello")); err != nil {
		t.Fatalf("set: %v", err)
	}
	// 文件已落地
	if _, err := os.Stat(filepath.Join(dir, "ns.json")); err != nil {
		t.Fatalf("file not created: %v", err)
	}

	// 新实例读取
	f2, err := NewFileStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	v, ok, _ := f2.Get("ns", "k")
	if !ok || string(v) != "hello" {
		t.Errorf("reload = %q, %v", v, ok)
	}

	// 删除
	if err := f2.Delete("ns", "k"); err != nil {
		t.Fatalf("del: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ns.json")); !os.IsNotExist(err) {
		t.Errorf("file should be removed")
	}
}

func TestChained_Routing(t *testing.T) {
	mem := NewMemoryStorage()
	dir, _ := os.MkdirTemp("", "storage-chain-")
	defer os.RemoveAll(dir)
	file, _ := NewFileStorage(dir)

	c := NewChainedStorage(mem, file)
	if err := c.Set("ns", "k", []byte("v")); err != nil {
		t.Fatalf("set: %v", err)
	}

	// L1 没数据，L2 有
	if _, ok, _ := mem.Get("ns", "k"); ok {
		t.Error("L1 should not have k after set")
	}
	v, ok, _ := c.Get("ns", "k")
	if !ok || string(v) != "v" {
		t.Errorf("chain get = %q, %v", v, ok)
	}
	// 仍然从 L2 读
	v, ok, _ = file.Get("ns", "k")
	if !ok || string(v) != "v" {
		t.Errorf("L2 get = %q, %v", v, ok)
	}

	// Delete 全层
	if err := c.Delete("ns", "k"); err != nil {
		t.Fatalf("del: %v", err)
	}
	if _, ok, _ := c.Get("ns", "k"); ok {
		t.Error("after chain delete, key should be gone")
	}

	// Empty backends
	empty := NewChainedStorage()
	if err := empty.Set("ns", "k", []byte("v")); err == nil {
		t.Error("empty chain should error on Set")
	}
}

func TestJSONHelpers(t *testing.T) {
	m := NewMemoryStorage()
	type sample struct {
		Name string `json:"name"`
		N    int    `json:"n"`
	}
	in := sample{Name: "alice", N: 7}
	if err := JSONSet(m, "ns", "k", in); err != nil {
		t.Fatalf("JSONSet: %v", err)
	}
	var out sample
	if err := JSONGet(m, "ns", "k", &out); err != nil {
		t.Fatalf("JSONGet: %v", err)
	}
	if out != in {
		t.Errorf("roundtrip = %+v, want %+v", out, in)
	}
	// missing
	var dummy sample
	if err := JSONGet(m, "ns", "missing", &dummy); err != ErrNotFound {
		t.Errorf("JSONGet missing = %v, want ErrNotFound", err)
	}
}
