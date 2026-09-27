package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFrame_RoundTrip(t *testing.T) {
	f := Frame{
		Kind:    KindEvent,
		Source:  "events.subscribe",
		Phase:   "delta",
		Payload: map[string]any{"text": "hello"},
	}
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"kind":"event"`) {
		t.Errorf("marshal missing kind=event: %s", b)
	}
	var got Frame
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Kind != KindEvent || got.Source != "events.subscribe" || got.Phase != "delta" {
		t.Errorf("roundtrip = %+v", got)
	}
}

func TestToolCall_RoundTrip(t *testing.T) {
	tc := ToolCall{ID: "1", Name: "echo", Arguments: `{"x":1}`}
	b, _ := json.Marshal(tc)
	var got ToolCall
	_ = json.Unmarshal(b, &got)
	if got != tc {
		t.Errorf("roundtrip = %+v, want %+v", got, tc)
	}
}

func TestPermissionDecision(t *testing.T) {
	d := PermissionDecision{ID: "abc", Approve: true, Reason: "looks fine"}
	b, _ := json.Marshal(d)
	if !strings.Contains(string(b), `"approve":true`) {
		t.Errorf("marshal = %s", b)
	}
}
