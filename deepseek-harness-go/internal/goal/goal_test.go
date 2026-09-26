package goal

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestStore_CreateGoalAndPlanAndTodo(t *testing.T) {
	s := NewStore()
	ctx := context.Background()

	g, err := s.CreateGoal(ctx, "v6", "implement 6 sub-phases")
	if err != nil {
		t.Fatalf("goal: %v", err)
	}
	if g.ID == "" {
		t.Error("goal id empty")
	}

	p, err := s.CreatePlan(ctx, g.ID, "workflow + jobs")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if p.GoalID != g.ID {
		t.Errorf("plan goal = %s, want %s", p.GoalID, g.ID)
	}

	t1, _ := s.AddTodo(ctx, p.ID, "write TC-v6-0001")
	t2, _ := s.AddTodo(ctx, p.ID, "write TC-v6-0002")
	if t1.Order != 0 || t2.Order != 1 {
		t.Errorf("orders = %d, %d; want 0, 1", t1.Order, t2.Order)
	}

	if err := s.UpdateTodoState(ctx, t1.ID, StateDone); err != nil {
		t.Errorf("update: %v", err)
	}

	list := s.ListTodosForPlan(ctx, p.ID)
	if len(list) != 2 {
		t.Errorf("list len = %d, want 2", len(list))
	}
	if list[0].State != StateDone {
		t.Errorf("list[0].state = %v, want done", list[0].State)
	}
}

func TestStore_Errors(t *testing.T) {
	s := NewStore()
	ctx := context.Background()

	if _, err := s.CreateGoal(ctx, "", ""); err == nil {
		t.Error("empty title should error")
	}
	if _, err := s.CreatePlan(ctx, "999", "p"); err != ErrGoalNotFound {
		t.Errorf("plan on missing goal = %v, want ErrGoalNotFound", err)
	}
	if _, err := s.AddTodo(ctx, "999", "t"); err != ErrPlanNotFound {
		t.Errorf("todo on missing plan = %v, want ErrPlanNotFound", err)
	}
	if err := s.UpdateTodoState(ctx, "999", StateDone); err != ErrTodoNotFound {
		t.Errorf("update missing = %v, want ErrTodoNotFound", err)
	}
	if err := s.DeleteTodo(ctx, "999"); err != ErrTodoNotFound {
		t.Errorf("delete missing = %v, want ErrTodoNotFound", err)
	}
}

func TestStore_Render(t *testing.T) {
	s := NewStore()
	ctx := context.Background()
	g, _ := s.CreateGoal(ctx, "v6", "ship the package")
	p, _ := s.CreatePlan(ctx, g.ID, "workflow")
	t1, _ := s.AddTodo(ctx, p.ID, "TC-v6-0004")
	_ = s.UpdateTodoState(ctx, t1.ID, StateInProgress)
	_, _ = s.AddTodo(ctx, p.ID, "TC-v6-0005")

	md := s.Snapshot().Render()
	for _, want := range []string{
		"## 当前 Goal",
		"**Goal**",
		"v6",
		"**Plan**",
		"workflow",
		"[~]", // in_progress marker
		"[ ]", // pending marker
		"TC-v6-0004",
		"TC-v6-0005",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("render missing %q; full:\n%s", want, md)
		}
	}
}

func TestStore_StateJSON(t *testing.T) {
	for _, c := range []struct {
		in   State
		want string
	}{
		{StatePending, `"pending"`},
		{StateDone, `"done"`},
		{StateBlocked, `"blocked"`},
	} {
		b, err := json.Marshal(c.in)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if string(b) != c.want {
			t.Errorf("marshal(%v) = %s, want %s", c.in, b, c.want)
		}
		var got State
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got != c.in {
			t.Errorf("roundtrip = %v, want %v", got, c.in)
		}
	}
	if _, err := json.Marshal(State(99)); err != nil {
		t.Errorf("marshal unknown state errored: %v", err)
	}
}
