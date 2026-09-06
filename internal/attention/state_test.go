package attention

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

var t0 = time.Unix(1_700_000_000, 0)

// A repeated report of the same status must not restart the clock: herdr
// re-reports a pane's status on events that changed something else, and
// treating those as transitions would pin every badge at "0s".
func TestApplyStatusUnchangedKeepsClock(t *testing.T) {
	s := New()
	s.ApplyStatus("p1", "w1", "blocked", t0)
	s.ApplyStatus("p1", "w1", "blocked", t0.Add(5*time.Minute))

	d, ok := Since(s, "p1", "blocked", t0.Add(5*time.Minute))
	if !ok {
		t.Fatal("Since reported nothing")
	}
	if d != 5*time.Minute {
		t.Errorf("age = %v, want 5m — a repeated status must not reset the clock", d)
	}
}

// done -> idle is the same underlying state; idle just means you have now
// looked at it. Resetting on the glance would erase the number the user wanted
// (herdr discussion #707).
func TestApplyStatusDoneToIdleKeepsClock(t *testing.T) {
	s := New()
	s.ApplyStatus("p1", "w1", "done", t0)
	s.ApplyStatus("p1", "w1", "idle", t0.Add(20*time.Minute))

	d, ok := Since(s, "p1", "idle", t0.Add(20*time.Minute))
	if !ok {
		t.Fatal("Since reported nothing")
	}
	if d != 20*time.Minute {
		t.Errorf("age = %v, want 20m — looking at a done agent must not reset its clock", d)
	}
}

// A real transition does reset it.
func TestApplyStatusRealTransitionResetsClock(t *testing.T) {
	s := New()
	s.ApplyStatus("p1", "w1", "working", t0)
	s.ApplyStatus("p1", "w1", "blocked", t0.Add(10*time.Minute))

	d, ok := Since(s, "p1", "blocked", t0.Add(11*time.Minute))
	if !ok {
		t.Fatal("Since reported nothing")
	}
	if d != time.Minute {
		t.Errorf("age = %v, want 1m", d)
	}
}

// A recorded status that disagrees with what the caller sees means the hook
// missed a transition. A badge from a stale start time would be confidently
// wrong, which is worse than no badge.
func TestSinceRejectsStaleStatus(t *testing.T) {
	s := New()
	s.ApplyStatus("p1", "w1", "blocked", t0)
	if _, ok := Since(s, "p1", "working", t0.Add(time.Minute)); ok {
		t.Error("Since reported an age for a status it did not record")
	}
	if _, ok := Since(s, "unknown-pane", "blocked", t0); ok {
		t.Error("Since reported an age for an unrecorded pane")
	}
}

func TestApplyFocusMovesToFrontAndDedupes(t *testing.T) {
	s := New()
	for _, ws := range []string{"w1", "w2", "w3", "w1"} {
		s.ApplyFocus(ws, t0)
	}
	want := []string{"w1", "w3", "w2"}
	if len(s.MRUWorkspaces) != len(want) {
		t.Fatalf("mru = %v, want %v", s.MRUWorkspaces, want)
	}
	for i := range want {
		if s.MRUWorkspaces[i] != want[i] {
			t.Fatalf("mru = %v, want %v", s.MRUWorkspaces, want)
		}
	}
}

func TestApplyFocusCaps(t *testing.T) {
	s := New()
	for i := 0; i < maxMRU*2; i++ {
		s.ApplyFocus(string(rune('a'+i%26))+string(rune('0'+i/26)), t0)
	}
	if len(s.MRUWorkspaces) > maxMRU {
		t.Errorf("mru length = %d, want at most %d", len(s.MRUWorkspaces), maxMRU)
	}
}

func TestPruneDropsDeadEntries(t *testing.T) {
	s := New()
	s.ApplyStatus("p1", "w1", "blocked", t0)
	s.ApplyStatus("p2", "w2", "idle", t0)
	s.ApplyFocus("w1", t0)
	s.ApplyFocus("w2", t0)

	s.Prune(map[string]bool{"p1": true}, map[string]bool{"w1": true})
	if _, ok := s.Panes["p2"]; ok {
		t.Error("Prune kept a dead pane")
	}
	if _, ok := s.Panes["p1"]; !ok {
		t.Error("Prune dropped a live pane")
	}
	if len(s.MRUWorkspaces) != 1 || s.MRUWorkspaces[0] != "w1" {
		t.Errorf("mru = %v, want [w1]", s.MRUWorkspaces)
	}
}

func TestFormatAge(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Second, "5s"},
		{59 * time.Second, "59s"},
		{time.Minute, "1m"},
		{9 * time.Minute, "9m"},
		{time.Hour, "1h"},
		{time.Hour + 12*time.Minute, "1h12m"},
		{25 * time.Hour, "1d"},
	} {
		if got := FormatAge(tc.d); got != tc.want {
			t.Errorf("FormatAge(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

// The writers are independent short-lived hook processes, so the file must
// survive concurrent read-modify-write without being left half-written.
func TestSaveIsAtomicUnderConcurrency(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := Load(path)
			s.ApplyStatus("p"+string(rune('a'+i)), "w1", "blocked", t0)
			_ = Save(path, s)
		}(i)
	}
	wg.Wait()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("state file unreadable after concurrent writes: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("state file is empty")
	}
	if s := Load(path); s.Version != Version {
		t.Errorf("state file did not parse after concurrent writes: %s", b)
	}
}

// Every failure mode of the file degrades to "no data", never to an error:
// the badge is a convenience and must not be able to fail a command.
func TestLoadDegradesToEmpty(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope.json")
	if got := Load(missing); len(got.Panes) != 0 {
		t.Error("missing file did not yield an empty state")
	}

	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Load(bad); len(got.Panes) != 0 {
		t.Error("malformed file did not yield an empty state")
	}

	future := filepath.Join(dir, "future.json")
	if err := os.WriteFile(future, []byte(`{"version":999,"panes":{"p1":{"status":"blocked"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Load(future); len(got.Panes) != 0 {
		t.Error("future-versioned file was read anyway")
	}
}
