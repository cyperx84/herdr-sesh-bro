package main

import (
	"testing"
	"time"

	herdr "github.com/cyperx84/herdr-api"

	"github.com/cyperx84/herdr-sesh-bro/internal/attention"
)

// The recorded history wins when there is one: `last` means the workspace you
// were in, not the highest-numbered other one.
func TestMRUWorkspacePrefersRecordedHistory(t *testing.T) {
	ws := []herdr.Workspace{{ID: "w1", Number: 1}, {ID: "w2", Number: 2}, {ID: "w9", Number: 9}}
	state := attention.New()
	state.ApplyFocus("w9", time.Now())
	state.ApplyFocus("w1", time.Now()) // most recent
	state.ApplyFocus("w2", time.Now()) // ...then this, the current one

	got, ok := mruWorkspace(state, ws, "w2")
	if !ok {
		t.Fatal("no MRU workspace found")
	}
	if got != "w1" {
		t.Errorf("last = %q, want w1 — the one actually visited before w2", got)
	}
}

// A workspace that has since been closed must not make `last` fail when there
// is a perfectly good older one behind it.
func TestMRUWorkspaceSkipsClosedWorkspaces(t *testing.T) {
	ws := []herdr.Workspace{{ID: "w1", Number: 1}, {ID: "w2", Number: 2}}
	state := attention.New()
	state.ApplyFocus("w1", time.Now())
	state.ApplyFocus("wGONE", time.Now())
	state.ApplyFocus("w2", time.Now())

	got, ok := mruWorkspace(state, ws, "w2")
	if !ok || got != "w1" {
		t.Errorf("last = %q ok=%v, want w1", got, ok)
	}
}

// With nothing recorded the caller falls back to the pre-0.4.0 rule, so this
// must report "no answer" rather than inventing one.
func TestMRUWorkspaceEmptyHistory(t *testing.T) {
	ws := []herdr.Workspace{{ID: "w1", Number: 1}}
	if _, ok := mruWorkspace(attention.New(), ws, "w1"); ok {
		t.Error("ok = true with no recorded history, want false so the caller can fall back")
	}
}

// Only the current workspace in history is not an answer either — returning it
// would re-focus where you already are.
func TestMRUWorkspaceOnlyCurrent(t *testing.T) {
	ws := []herdr.Workspace{{ID: "w1", Number: 1}}
	state := attention.New()
	state.ApplyFocus("w1", time.Now())
	if _, ok := mruWorkspace(state, ws, "w1"); ok {
		t.Error("ok = true when only the current workspace is recorded")
	}
}
