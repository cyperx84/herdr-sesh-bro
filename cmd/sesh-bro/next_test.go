package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	herdr "github.com/cyperx84/herdr-api"

	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx/herdrtest"
	"github.com/cyperx84/herdr-sesh-bro/internal/output"
)

// nextFixture registers everything `next` calls: the liveness probe, the
// snapshot, the toast, and the focus.
func nextFixture(t *testing.T, focusedPane string, agents []herdrtest.SnapshotFixtureAgent) *herdrtest.Server {
	t.Helper()
	s := herdrtest.Start(t)
	s.Handle("workspace.list", func(json.RawMessage) (any, error) {
		return map[string]any{"workspaces": []map[string]any{}}, nil
	})
	s.Handle("session.snapshot", func(json.RawMessage) (any, error) {
		// SnapshotJSON already carries the {"snapshot": …} envelope the
		// client decodes, so splice the focused pane INSIDE it rather than
		// wrapping again. The fixture models a focused workspace, not a
		// focused pane, and NextAttention keys on exactly that field.
		raw := herdrtest.SnapshotFixture{Agents: agents}.SnapshotJSON()
		var envelope map[string]any
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Fatalf("fixture: %v", err)
		}
		if focusedPane != "" {
			inner, _ := envelope["snapshot"].(map[string]any)
			if inner == nil {
				t.Fatalf("fixture envelope has no snapshot object: %s", raw)
			}
			inner["focused_pane_id"] = focusedPane
		}
		return envelope, nil
	})
	s.Handle("notification.show", func(json.RawMessage) (any, error) {
		return map[string]any{"shown": true, "reason": "shown"}, nil
	})
	s.Handle("agent.focus", func(json.RawMessage) (any, error) {
		return map[string]any{}, nil
	})
	return s
}

func fixtureAgent(name, pane string, status herdr.AgentStatus, seq uint64) herdrtest.SnapshotFixtureAgent {
	kind := "claude"
	return herdrtest.SnapshotFixtureAgent{
		Agent: herdr.Agent{
			Name: name, PaneID: pane, WorkspaceID: "w1",
			Status: status, Agent: &kind, CWD: "/tmp/" + name,
		},
		StateChangeSeq: seq,
	}
}

// The whole point: one keypress lands on the blocked agent, not the idle one,
// and not the pane you are already on.
func TestNextFocusesTheBlockedAgent(t *testing.T) {
	s := nextFixture(t, "w1:p9", []herdrtest.SnapshotFixtureAgent{
		fixtureAgent("calm", "w1:p1", herdr.StatusIdle, 90),
		fixtureAgent("stuck", "w1:p2", herdr.StatusBlocked, 10),
	})

	var stdout, stderr bytes.Buffer
	if code := run([]string{"next"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s))); code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	focus := s.Calls("agent.focus")
	if len(focus) != 1 {
		t.Fatalf("agent.focus calls = %d, want 1", len(focus))
	}
	if !strings.Contains(string(focus[0].Params), "w1:p2") {
		t.Errorf("focused %s, want the blocked pane w1:p2", focus[0].Params)
	}
}

// herdr suppresses a toast aimed at the tab you are already looking at, and
// focusing makes the target exactly that tab — so the toast must be sent
// first, or it reliably shows nothing.
func TestNextToastsBeforeFocusing(t *testing.T) {
	s := nextFixture(t, "w1:p9", []herdrtest.SnapshotFixtureAgent{
		fixtureAgent("stuck", "w1:p2", herdr.StatusBlocked, 10),
	})
	var stdout, stderr bytes.Buffer
	run([]string{"next"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))

	order := s.CallOrder()
	toast, focus := -1, -1
	for i, m := range order {
		switch m {
		case "notification.show":
			if toast < 0 {
				toast = i
			}
		case "agent.focus":
			if focus < 0 {
				focus = i
			}
		}
	}
	if toast < 0 || focus < 0 {
		t.Fatalf("call order %v missing toast or focus", order)
	}
	if toast > focus {
		t.Errorf("toast at %d came after focus at %d — herdr would suppress it (%v)", toast, focus, order)
	}
}

// Nothing waiting still announces itself — a key that does nothing visible is
// indistinguishable from a broken keybinding — but the EXIT CODE says
// "nothing", not "fine". A caller must be able to tell an empty queue from a
// successful jump without parsing the toast, which is what output.ExitEmpty
// is for; a human on a keybinding never sees an exit code either way.
func TestNextWithNothingWaiting(t *testing.T) {
	s := nextFixture(t, "w1:p9", []herdrtest.SnapshotFixtureAgent{
		fixtureAgent("calm", "w1:p1", herdr.StatusIdle, 90),
	})
	var stdout, stderr bytes.Buffer
	if code := run([]string{"next"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s))); code != output.ExitEmpty {
		t.Fatalf("code = %d, want %d (ExitEmpty)", code, output.ExitEmpty)
	}
	if len(s.Calls("agent.focus")) != 0 {
		t.Error("focused something with nothing waiting")
	}
	toasts := s.Calls("notification.show")
	if len(toasts) != 1 || !strings.Contains(string(toasts[0].Params), "nothing needs you") {
		t.Errorf("toast = %v, want one saying nothing needs you", toasts)
	}
}

// prev walks the other way.
func TestPrevGoesBackwards(t *testing.T) {
	s := nextFixture(t, "w1:p9", []herdrtest.SnapshotFixtureAgent{
		fixtureAgent("a", "w1:p1", herdr.StatusBlocked, 30),
		fixtureAgent("c", "w1:p3", herdr.StatusBlocked, 10),
	})
	var stdout, stderr bytes.Buffer
	run([]string{"prev"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	focus := s.Calls("agent.focus")
	if len(focus) != 1 || !strings.Contains(string(focus[0].Params), "w1:p3") {
		t.Errorf("prev focused %v, want the least urgent w1:p3", focus)
	}
}

func TestNextRejectsArguments(t *testing.T) {
	s := nextFixture(t, "", nil)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"next", "extra"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s))); code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
}

// --dry-run answers "where would you send me" without sending anyone. A
// surveying caller needs this because focusing a `done` agent marks it seen
// and turns it idle — so a survey that focused would quietly destroy the very
// signal it was surveying.
func TestNextDryRunDoesNotFocus(t *testing.T) {
	s := nextFixture(t, "w1:p9", []herdrtest.SnapshotFixtureAgent{
		fixtureAgent("stuck", "w1:p2", herdr.StatusBlocked, 10),
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"next", "--dry-run", "--json"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	if code != output.ExitOK {
		t.Fatalf("code = %d, want 0 (stderr %q)", code, stderr.String())
	}
	if n := len(s.Calls("agent.focus")); n != 0 {
		t.Errorf("--dry-run focused %d times; it must not move anyone", n)
	}
	if !strings.Contains(stdout.String(), "w1:p2") {
		t.Errorf("--dry-run did not report the target it would pick: %q", stdout.String())
	}
}

// An unknown flag is the caller's mistake, and retrying it unchanged cannot
// help — so it is exit 2, distinct from a runtime failure.
func TestNextRejectsUnknownFlags(t *testing.T) {
	s := nextFixture(t, "", nil)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"next", "--nope"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s))); code != output.ExitUsage {
		t.Errorf("code = %d, want %d (ExitUsage)", code, output.ExitUsage)
	}
}
