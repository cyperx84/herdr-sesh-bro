package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/cyperx84/herdr-sesh-bro/internal/attention"
)

// withRecordedState swaps the state store for an in-memory one and returns a
// pointer to what the hook wrote.
func withRecordedState(t *testing.T) *attention.State {
	t.Helper()
	held := attention.New()
	origLoad, origSave := attentionLoad, attentionSave
	attentionLoad = func(string) attention.State { return held }
	attentionSave = func(_ string, s attention.State) error { held = s; return nil }
	t.Cleanup(func() { attentionLoad, attentionSave = origLoad, origSave })
	return &held
}

func runHook(t *testing.T, eventJSON string) int {
	t.Helper()
	var stdout, stderr bytes.Buffer
	return run([]string{"record-event"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(map[string]string{
		"HERDR_PLUGIN_EVENT_JSON": eventJSON,
		"HERDR_PLUGIN_STATE_DIR":  t.TempDir(),
	}))
}

func TestRecordEventStoresStatusChange(t *testing.T) {
	held := withRecordedState(t)
	code := runHook(t, `{"event":"pane_agent_status_changed","data":{"type":"pane_agent_status_changed","pane_id":"w1:p2","workspace_id":"w1","agent_status":"blocked","agent":"claude"}}`)
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	ps, ok := held.Panes["w1:p2"]
	if !ok {
		t.Fatalf("nothing recorded: %+v", held)
	}
	if ps.Status != "blocked" || ps.WorkspaceID != "w1" {
		t.Errorf("recorded %+v, want blocked in w1", ps)
	}
	if _, ok := attention.Since(*held, "w1:p2", "blocked", time.Now()); !ok {
		t.Error("recorded entry has no usable start time")
	}
}

func TestRecordEventStoresWorkspaceFocus(t *testing.T) {
	held := withRecordedState(t)
	runHook(t, `{"event":"workspace_focused","data":{"type":"workspace_focused","workspace_id":"w7"}}`)
	if len(held.MRUWorkspaces) != 1 || held.MRUWorkspaces[0] != "w7" {
		t.Errorf("mru = %v, want [w7]", held.MRUWorkspaces)
	}
}

// A hook that exits non-zero fills herdr's plugin log with noise about a
// feature whose whole value is a small grey badge, and the user can do nothing
// about it. Every malformed input degrades to "recorded nothing".
func TestRecordEventNeverFails(t *testing.T) {
	for name, payload := range map[string]string{
		"empty":               "",
		"not json":            "{{{",
		"no data":             `{"event":"x"}`,
		"unrelated event":     `{"event":"tab_created","data":{"type":"tab_created","tab_id":"w1:t1"}}`,
		"status without pane": `{"event":"pane_agent_status_changed","data":{"agent_status":"blocked"}}`,
	} {
		held := withRecordedState(t)
		if code := runHook(t, payload); code != 0 {
			t.Errorf("%s: code = %d, want 0 — a hook must never fail", name, code)
		}
		if len(held.Panes) != 0 || len(held.MRUWorkspaces) != 0 {
			t.Errorf("%s: recorded %+v, want nothing", name, held)
		}
	}
}
