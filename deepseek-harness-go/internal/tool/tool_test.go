package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

type stubTool struct {
	name, desc string
	params     any
	out        Result
	outErr     error
	gotArgs    json.RawMessage
	calls      int
}

func (s *stubTool) Name() string        { return s.name }
func (s *stubTool) Description() string { return s.desc }
func (s *stubTool) Parameters() any     { return s.params }
func (s *stubTool) Execute(_ context.Context, args json.RawMessage) (Result, error) {
	s.gotArgs = args
	s.calls++
	return s.out, s.outErr
}

func TestRegister_Get_Names(t *testing.T) {
	r := NewRegistry()
	a := &stubTool{name: "a", desc: "A"}
	b := &stubTool{name: "b", desc: "B"}

	if err := r.Register(a); err != nil {
		t.Fatalf("register a: %v", err)
	}
	if err := r.Register(b); err != nil {
		t.Fatalf("register b: %v", err)
	}

	if got, ok := r.Get("a"); !ok || got != a {
		t.Errorf("Get(a) = %v, %v; want a, true", got, ok)
	}
	if _, ok := r.Get("missing"); ok {
		t.Errorf("Get(missing) ok = true, want false")
	}
	if names := r.Names(); len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Errorf("Names = %v", names)
	}
	if r.Len() != 2 {
		t.Errorf("Len = %d", r.Len())
	}
}

func TestRegister_Duplicate(t *testing.T) {
	r := NewRegistry()
	a := &stubTool{name: "a", desc: "A"}
	_ = r.Register(a)
	err := r.Register(a)
	if !errors.Is(err, ErrDuplicate) {
		t.Errorf("err = %v, want ErrDuplicate", err)
	}
}

func TestRegister_EmptyName(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&stubTool{name: ""}); err == nil {
		t.Errorf("expected error for empty name")
	}
}

func TestRegister_NilTool(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(nil); err == nil {
		t.Errorf("expected error for nil tool")
	}
}

func TestSpecs_PreservesOrderAndIsCopy(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&stubTool{name: "b", desc: "B"})
	_ = r.Register(&stubTool{name: "a", desc: "A"})

	specs := r.Specs()
	if len(specs) != 2 {
		t.Fatalf("Specs len = %d", len(specs))
	}
	if specs[0].Function.Name != "b" || specs[1].Function.Name != "a" {
		t.Errorf("Specs order = %v, %v; want b, a", specs[0].Function.Name, specs[1].Function.Name)
	}

	// 修改返回的切片不得影响注册表。
	specs[0].Function.Name = "MUTATED"
	again := r.Specs()
	if again[0].Function.Name == "MUTATED" {
		t.Errorf("Specs() should return a defensive copy")
	}
}

func TestMustRegister_PanicsOnDuplicate(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&stubTool{name: "a"})
	defer func() {
		if recover() == nil {
			t.Errorf("expected panic on duplicate MustRegister")
		}
	}()
	r.MustRegister(&stubTool{name: "a"})
}

func TestRegistry_ConcurrentReads(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&stubTool{name: "x", desc: "X"})

	const N = 100
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			_, _ = r.Get("x")
			_ = r.Names()
			_ = r.Specs()
		}()
	}
	wg.Wait()
}

func TestExecute_ArgsPropagated(t *testing.T) {
	a := &stubTool{name: "a", out: Ok("done")}
	_, _ = a.Execute(context.Background(), json.RawMessage(`{"x":1}`))
	if !strings.Contains(string(a.gotArgs), `"x":1`) {
		t.Errorf("args = %q", a.gotArgs)
	}
	if a.calls != 1 {
		t.Errorf("calls = %d", a.calls)
	}
}

func TestSpec_Converts(t *testing.T) {
	a := &stubTool{
		name:   "greet",
		desc:   "say hi",
		params: map[string]any{"type": "object"},
	}
	s := Spec(a)
	if s.Type != "function" {
		t.Errorf("Type = %q", s.Type)
	}
	if s.Function.Name != "greet" || s.Function.Description != "say hi" {
		t.Errorf("Function = %+v", s.Function)
	}
	if s.Function.Parameters == nil {
		t.Errorf("Parameters nil")
	}
}
