package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	herdr "github.com/cyperx84/herdr-api"

	"github.com/cyperx84/herdr-sesh-bro/internal/attention"
	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx/herdrtest"
	"github.com/cyperx84/herdr-sesh-bro/internal/picker"
)

func withFzfFeatures(t *testing.T, f picker.Features) {
	t.Helper()
	orig := detectFzf
	detectFzf = func() picker.Features { return f }
	t.Cleanup(func() { detectFzf = orig })
}

// startup's output is what `herdr plugin log list` shows, so it is where
// someone debugging "why isn't my picker live?" looks first. Reporting only
// "deps ok" left that question unanswerable without reading the source.
func TestStartupReportsFzfFeatures(t *testing.T) {
	withFzfFeatures(t, picker.Features{Version: "0.74.3", Listen: true, TrackID: true, Footer: true})
	s := herdrtest.Start(t)
	s.Handle("workspace.list", func(json.RawMessage) (any, error) {
		return map[string]any{"workspaces": []map[string]any{}}, nil
	})
	s.Handle("session.snapshot", func(json.RawMessage) (any, error) {
		return json.RawMessage(herdrtest.SnapshotFixture{}.SnapshotJSON()), nil
	})

	var stdout, stderr bytes.Buffer
	if code := run([]string{"startup"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s))); code != 0 {
		t.Fatalf("code = %d (stderr %q)", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "0.74.3") || !strings.Contains(out, "live updates: yes") {
		t.Errorf("startup output does not report fzf features:\n%s", out)
	}
}

// An fzf too old to push updates is exactly the case worth naming out loud,
// and it must not be an error — the picker still works.
func TestStartupReportsOldFzfWithoutFailing(t *testing.T) {
	withFzfFeatures(t, picker.Features{Version: "0.65.0"})
	s := herdrtest.Start(t)
	s.Handle("workspace.list", func(json.RawMessage) (any, error) {
		return map[string]any{"workspaces": []map[string]any{}}, nil
	})
	s.Handle("session.snapshot", func(json.RawMessage) (any, error) {
		return json.RawMessage(herdrtest.SnapshotFixture{}.SnapshotJSON()), nil
	})

	var stdout, stderr bytes.Buffer
	if code := run([]string{"startup"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s))); code != 0 {
		t.Fatalf("an old fzf must not fail startup: code %d, stderr %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "live updates: no") {
		t.Errorf("old fzf not reported as lacking live updates:\n%s", stdout.String())
	}
}

// Nothing else ever removes recorded panes — the event hook only adds — so
// without a sweep the file grows an entry for every pane the machine has ever
// run an agent in.
func TestStartupPrunesDeadPanes(t *testing.T) {
	withFzfFeatures(t, picker.Features{Version: "0.74.3", Listen: true})

	held := attention.New()
	held.ApplyStatus("w1:p1", "w1", "blocked", time.Unix(1_700_000_000, 0))
	held.ApplyStatus("wGONE:p9", "wGONE", "idle", time.Unix(1_700_000_000, 0))
	held.ApplyFocus("w1", time.Unix(1_700_000_000, 0))
	held.ApplyFocus("wGONE", time.Unix(1_700_000_000, 0))

	origUpdate := attentionUpdate
	attentionUpdate = func(_ string, mutate func(*attention.State)) error {
		mutate(&held)
		return nil
	}
	t.Cleanup(func() { attentionUpdate = origUpdate })

	s := herdrtest.Start(t)
	s.Handle("workspace.list", func(json.RawMessage) (any, error) {
		return map[string]any{"workspaces": []map[string]any{}}, nil
	})
	s.Handle("session.snapshot", func(json.RawMessage) (any, error) {
		return json.RawMessage(herdrtest.SnapshotFixture{
			Workspaces: []herdr.Workspace{{ID: "w1", Number: 1, Label: "alpha"}},
			Agents: []herdrtest.SnapshotFixtureAgent{
				{Agent: herdr.Agent{PaneID: "w1:p1", WorkspaceID: "w1", Status: herdr.StatusBlocked}},
			},
		}.SnapshotJSON()), nil
	})

	var stdout, stderr bytes.Buffer
	if code := run([]string{"startup"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s))); code != 0 {
		t.Fatalf("code = %d (stderr %q)", code, stderr.String())
	}
	if _, ok := held.Panes["wGONE:p9"]; ok {
		t.Error("a pane that no longer exists survived the sweep")
	}
	if _, ok := held.Panes["w1:p1"]; !ok {
		t.Error("sweep removed a live pane")
	}
	for _, ws := range held.MRUWorkspaces {
		if ws == "wGONE" {
			t.Error("a closed workspace survived in the MRU list")
		}
	}
}
