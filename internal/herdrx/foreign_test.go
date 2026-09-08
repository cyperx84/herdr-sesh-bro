package herdrx

import (
	"strings"
	"testing"

	herdr "github.com/cyperx84/herdr-api"
)

// TestSessionSeparator pins the assumption ForeignTarget rests on.
//
// herdr documents no grammar for session names. The only constraint visible
// from outside is that a name becomes a directory under
// ~/.config/herdr/sessions/, which rules out "/" and very little else — so "@"
// is a choice, not a fact, and this test is where that choice is recorded. If
// it ever changes, every stored target and every guard changes with it.
func TestSessionSeparator(t *testing.T) {
	if SessionSeparator != "@" {
		t.Fatalf("SessionSeparator = %q: changing it changes every composite target and every refuseForeign check", SessionSeparator)
	}
}

func TestForeignTargetRoundTrip(t *testing.T) {
	target := ForeignTarget("w1:p1", "work")
	if target != "w1:p1@work" {
		t.Fatalf("ForeignTarget = %q", target)
	}
	id, session, ok := SplitForeignTarget(target)
	if !ok || id != "w1:p1" || session != "work" {
		t.Fatalf("SplitForeignTarget(%q) = %q,%q,%v", target, id, session, ok)
	}
}

// TestSplitForeignTargetLocalIsUnchanged: a local target has no separator and
// must come back byte-identical with ok=false, because one call has to
// classify as well as parse — every mutating command asks this question about
// targets that are almost always local.
func TestSplitForeignTargetLocalIsUnchanged(t *testing.T) {
	for _, local := range []string{"w1:p1", "builder", "/some/path", ""} {
		id, session, ok := SplitForeignTarget(local)
		if ok || id != local || session != "" {
			t.Errorf("SplitForeignTarget(%q) = %q,%q,%v, want it left alone", local, id, session, ok)
		}
	}
}

// TestSplitForeignTargetCutsFromTheRight: the session is the last field, so an
// id that somehow contained the separator still yields the correct session —
// which is the half that decides whether a mutating command must refuse.
func TestSplitForeignTargetCutsFromTheRight(t *testing.T) {
	id, session, ok := SplitForeignTarget("a@b@work")
	if !ok || session != "work" || id != "a@b" {
		t.Fatalf("got id=%q session=%q ok=%v", id, session, ok)
	}
}

func snap(agents ...Agent) Snapshot {
	return Snapshot{Workspaces: []herdr.Workspace{{ID: "w1"}}, Agents: agents}
}

func fagent(name, pane string, status herdr.AgentStatus) Agent {
	var a Agent
	a.Name, a.PaneID, a.Status = name, pane, status
	return a
}

func rowTargets(rows []Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = string(r.Type) + ":" + r.Target
	}
	return out
}

func TestForeignRowsComposesTargets(t *testing.T) {
	rows := ForeignRows([]ForeignSnapshot{{
		Session:  Session{Name: "work", Running: true},
		Snapshot: snap(fagent("builder", "w1:p1", herdr.StatusBlocked), fagent("", "w1:p2", herdr.StatusIdle)),
	}})
	want := []string{"session:work", "ragent:builder@work", "ragent:w1:p2@work"}
	got := rowTargets(rows)
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	for _, r := range rows {
		if r.Session != "work" {
			t.Errorf("row %q has Session %q, want work — the emptiness of this field is what every guard tests", r.Target, r.Session)
		}
	}
}

// TestForeignRowsSkipsFailedSessions: a session that will not answer
// contributes nothing, not an error row. The foreign view refreshes on a
// ticker, so a session shutting down would otherwise sit there for seconds as
// an error the user can do nothing about.
func TestForeignRowsSkipsFailedSessions(t *testing.T) {
	rows := ForeignRows([]ForeignSnapshot{
		{Session: Session{Name: "dead", Running: true}, Err: errFake},
		{Session: Session{Name: "live", Running: true}, Snapshot: snap()},
	})
	for _, r := range rows {
		if strings.Contains(r.Target, "dead") {
			t.Fatalf("a failed session produced a row: %+v", r)
		}
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want just the live session's", len(rows))
	}
}

// TestForeignRowsSkipsAmbiguousSessionNames is the other half of the separator
// assumption. A session named "a@b" would produce targets nothing could split
// correctly, so its rows are omitted: visibly missing beats silently wrong.
func TestForeignRowsSkipsAmbiguousSessionNames(t *testing.T) {
	rows := ForeignRows([]ForeignSnapshot{{
		Session:  Session{Name: "a@b", Running: true},
		Snapshot: snap(fagent("builder", "w1:p1", herdr.StatusIdle)),
	}})
	if len(rows) != 0 {
		t.Fatalf("got %v, want no rows for a session whose name contains %q", rowTargets(rows), SessionSeparator)
	}
}

// TestForeignSessionDetailCountsWhatWants: the detail column's job is to say
// whether looking over there is worth it.
func TestForeignSessionDetailCountsWhatWants(t *testing.T) {
	d := foreignSessionDetail(snap(
		fagent("a", "w1:p1", herdr.StatusBlocked),
		fagent("b", "w1:p2", herdr.StatusDone),
		fagent("c", "w1:p3", herdr.StatusWorking),
	))
	if !strings.Contains(d, "3a") || !strings.Contains(d, "2 waiting") {
		t.Fatalf("detail = %q, want 3 agents and 2 waiting (blocked and done, not working)", d)
	}
	if strings.Contains(foreignSessionDetail(snap()), "waiting") {
		t.Error("an empty session should not advertise a waiting count")
	}
}

// TestForeignSnapshotsExcludesSelfAndStopped: dialling our own socket would
// duplicate every local row as a foreign one, and a stopped session has no
// socket to dial at all.
func TestForeignSnapshotsExcludesSelfAndStopped(t *testing.T) {
	sessions := []Session{
		{Name: "self", Running: true, SocketPath: "/tmp/self.sock"},
		{Name: "stopped", Running: false, SocketPath: "/tmp/stopped.sock"},
		{Name: "nosocket", Running: true, SocketPath: ""},
	}
	// Every remaining candidate is excluded, so this returns before dialling
	// anything — which is also what makes the test hermetic.
	if got := ForeignSnapshots(t.Context(), sessions, "/tmp/self.sock"); got != nil {
		t.Fatalf("got %+v, want nothing to dial", got)
	}
}

var errFake = fakeErr{}

type fakeErr struct{}

func (fakeErr) Error() string { return "fake" }
