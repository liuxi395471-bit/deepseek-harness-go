package installer

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestScanner_AllFormats(t *testing.T) {
	root, _ := os.MkdirTemp("", "inst-scan-")
	defer os.RemoveAll(root)

	// 三个子目录 + 一个隐藏子目录（应被跳过）
	os.MkdirAll(filepath.Join(root, "alpha"), 0o755)
	os.WriteFile(filepath.Join(root, "alpha", "plugin.json"), []byte(`{"version":"0.1","main":"alpha.wasm"}`), 0o644)

	os.MkdirAll(filepath.Join(root, "beta"), 0o755)
	os.WriteFile(filepath.Join(root, "beta", "package.json"), []byte(`{"version":"0.2","main":"dist/index.js"}`), 0o644)

	os.MkdirAll(filepath.Join(root, "gamma"), 0o755)
	os.WriteFile(filepath.Join(root, "gamma", "gamma.jar"), []byte("jar"), 0o644)

	os.MkdirAll(filepath.Join(root, ".hidden"), 0o755)
	os.WriteFile(filepath.Join(root, ".hidden", "plugin.json"), []byte("{}"), 0o644)

	// 无 plugin manifest 的子目录，应被忽略
	os.MkdirAll(filepath.Join(root, "empty"), 0o755)

	s := NewScanner()
	entries, err := s.Scan(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3; got=%v", len(entries), namesOf(entries))
	}
	want := map[string]Format{"alpha": FormatNative, "beta": FormatNode, "gamma": FormatJAR}
	for _, e := range entries {
		if want[e.Name] != e.Format {
			t.Errorf("%s format = %s, want %s", e.Name, e.Format, want[e.Name])
		}
	}
}

func TestScanner_NonExistentRoot(t *testing.T) {
	s := NewScanner()
	entries, err := s.Scan("/no/such/dir/please")
	if err != nil {
		t.Fatalf("scan nonexistent: %v", err)
	}
	if entries != nil {
		t.Errorf("entries = %v, want nil", entries)
	}
}

func TestStatusStore_RoundTrip(t *testing.T) {
	dir, _ := os.MkdirTemp("", "inst-stat-")
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "plugin_status.json")

	store, err := NewStatusStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(&Status{Name: "alpha", State: StateLoaded, Message: "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(&Status{Name: "beta", State: StateFailed, Message: "boom"}); err != nil {
		t.Fatal(err)
	}

	// 重新加载
	store2, err := NewStatusStore(path)
	if err != nil {
		t.Fatal(err)
	}
	all := store2.All()
	if len(all) != 2 {
		t.Fatalf("all len = %d", len(all))
	}
	got := map[string]State{}
	for _, s := range all {
		got[s.Name] = s.State
	}
	if got["alpha"] != StateLoaded || got["beta"] != StateFailed {
		t.Errorf("states = %+v", got)
	}
}

func TestReconcile(t *testing.T) {
	dir, _ := os.MkdirTemp("", "inst-reconcile-")
	defer os.RemoveAll(dir)
	store, _ := NewStatusStore(filepath.Join(dir, "s.json"))
	store.Set(&Status{Name: "alpha", State: StateLoaded})
	store.Set(&Status{Name: "ghost", State: StateLoaded})

	entries := []*Entry{
		{Name: "alpha"},
		{Name: "delta"}, // drift
	}
	r := Reconcile(entries, store)
	sort.Strings(r.Healthy)
	if !reflect.DeepEqual(r.Healthy, []string{"alpha"}) {
		t.Errorf("healthy = %v", r.Healthy)
	}
	if len(r.Drift) != 1 || r.Drift[0].Name != "delta" {
		t.Errorf("drift = %v", r.Drift)
	}
	if len(r.Orphans) != 1 || r.Orphans[0].Name != "ghost" {
		t.Errorf("orphans = %v", r.Orphans)
	}
}

func namesOf(es []*Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Name
	}
	return out
}
