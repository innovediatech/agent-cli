package envelope

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestLiveOmitsSyncedAt(t *testing.T) {
	env := Live(map[string]string{"id": "x"})
	out, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "synced_at") {
		t.Fatalf("live envelope should omit synced_at, got %s", out)
	}
	if !strings.Contains(string(out), `"source":"live"`) {
		t.Fatalf("expected source=live, got %s", out)
	}
}

func TestLocalIncludesSyncedAt(t *testing.T) {
	ts := time.Date(2026, 5, 11, 18, 30, 0, 0, time.UTC)
	env := Local([]string{"a", "b"}, ts, "live API unreachable")
	out, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `"source":"local"`) {
		t.Fatalf("expected source=local, got %s", s)
	}
	if !strings.Contains(s, `"synced_at":"2026-05-11T18:30:00Z"`) {
		t.Fatalf("synced_at missing or wrong, got %s", s)
	}
	if !strings.Contains(s, `"reason":"live API unreachable"`) {
		t.Fatalf("reason missing, got %s", s)
	}
}

func TestLocalZeroSyncedAtOmitted(t *testing.T) {
	env := Local("x", time.Time{}, "")
	out, _ := json.Marshal(env)
	if strings.Contains(string(out), "synced_at") {
		t.Fatalf("zero time should omit synced_at, got %s", out)
	}
}

func TestWithRequestID(t *testing.T) {
	env := Live("data").WithRequestID("req_abc123")
	out, _ := json.Marshal(env)
	if !strings.Contains(string(out), `"request_id":"req_abc123"`) {
		t.Fatalf("request_id missing, got %s", out)
	}
}

func TestWithNextCursor(t *testing.T) {
	env := Live([]int{1, 2, 3}).WithNextCursor("cD0yMDI2LTA1LTA2")
	out, _ := json.Marshal(env)
	if !strings.Contains(string(out), `"next_cursor":"cD0yMDI2LTA1LTA2"`) {
		t.Fatalf("next_cursor missing, got %s", out)
	}
}

func TestEmptyNextCursorOmitted(t *testing.T) {
	env := Live("x").WithNextCursor("")
	out, _ := json.Marshal(env)
	if strings.Contains(string(out), "next_cursor") {
		t.Fatalf("empty next_cursor should be omitted, got %s", out)
	}
}

func TestRoundtripStructure(t *testing.T) {
	src := Live(map[string]any{"id": 7, "name": "alice"})
	b, _ := json.Marshal(src)
	var got struct {
		Meta    map[string]any `json:"meta"`
		Results map[string]any `json:"results"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Meta["source"] != "live" {
		t.Fatalf("source not preserved: %v", got.Meta)
	}
	if got.Results["name"] != "alice" {
		t.Fatalf("results not preserved: %v", got.Results)
	}
}
