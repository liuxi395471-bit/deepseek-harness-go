package schedule

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestParseCronAll(t *testing.T) {
	f, err := ParseCron("* * * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for i := 0; i < 5; i++ {
		if f[i] == nil {
			t.Fatalf("field %d nil", i)
		}
	}
}

func TestParseCronSpecific(t *testing.T) {
	f, err := ParseCron("0 12 * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !f[0].Matches(0) || f[0].Matches(1) {
		t.Fatalf("minute wrong")
	}
	if !f[1].Matches(12) || f[1].Matches(11) {
		t.Fatalf("hour wrong")
	}
}

func TestParseCronBadInput(t *testing.T) {
	cases := []string{
		"",                              // empty
		"* * * *",                       // only 4
		"* * * * * *",                   // 6
		"60 * * * *",                    // minute > 59
		"* 24 * * *",                    // hour > 23
		"5-2 * * * *",                   // range reverse
		"a * * * *",                     // non-numeric
	}
	for _, c := range cases {
		_, err := ParseCron(c)
		if err == nil {
			t.Fatalf("expected error for %q", c)
		}
	}
}

func TestParseCronStep(t *testing.T) {
	// 每 5 分钟：*/5
	f, err := ParseCron("*/5 * * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, v := range []int{0, 5, 10, 15, 55} {
		if !f[0].Matches(v) {
			t.Fatalf("minute %d should match", v)
		}
	}
	if f[0].Matches(1) {
		t.Fatal("minute 1 should not match")
	}
}

func TestNextFire(t *testing.T) {
	// 10:30 → next "30 * * * *" = 10:30 if now <= 10:30, else 11:30
	now := time.Date(2026, 1, 1, 10, 29, 0, 0, time.UTC)
	got := nextFire("30 * * * *", now)
	want := time.Date(2026, 1, 1, 10, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	// 跨小时
	now2 := time.Date(2026, 1, 1, 10, 31, 0, 0, time.UTC)
	got2 := nextFire("30 * * * *", now2)
	want2 := time.Date(2026, 1, 1, 11, 30, 0, 0, time.UTC)
	if !got2.Equal(want2) {
		t.Fatalf("got %v want %v", got2, want2)
	}
}

func TestStoreCRUD(t *testing.T) {
	st := NewStore()
	s, err := st.Create("my-job", "*/5 * * * *", `{"type":"agent_run","prompt":"hi"}`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if s.ID == "" {
		t.Fatal("id empty")
	}
	if !s.Enabled {
		t.Fatal("should be enabled by default")
	}
	if s.NextRunAt.IsZero() {
		t.Fatal("nextRunAt should be set")
	}
	list := st.List()
	if len(list) != 1 {
		t.Fatalf("list len = %d", len(list))
	}
	got, err := st.Get(s.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "my-job" {
		t.Fatalf("name: %s", got.Name)
	}

	bad, err := st.Create("x", "bad-cron", "{}")
	if err == nil {
		t.Fatalf("expected error for bad cron, got %+v", bad)
	}

	enabled := false
	updated, err := st.Update(s.ID, "renamed", "", "", &enabled)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "renamed" {
		t.Fatalf("rename failed")
	}
	if updated.Enabled {
		t.Fatal("should be disabled")
	}

	if err := st.Delete(s.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st.Get(s.ID); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestWorkerTriggers(t *testing.T) {
	st := NewStore()
	triggered := make(chan string, 1)
	handler := func(ctx context.Context, action string) error {
		triggered <- action
		return nil
	}
	w := NewWorker(st, handler)
	s, _ := st.Create("tick", "* * * * *", `{"type":"agent_run","prompt":"x"}`)

	// 强制 NextRunAt = now
	now := time.Now()
	st.items[s.ID].NextRunAt = now.Add(-time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.tick(ctx)

	select {
	case a := <-triggered:
		if !strings.Contains(a, "agent_run") {
			t.Fatalf("unexpected action: %s", a)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not trigger")
	}
}
