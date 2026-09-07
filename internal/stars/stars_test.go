package stars

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

var t0 = time.Unix(1_700_000_000, 0)

// A pin the user cannot take off is worse than one they cannot put on: the
// agent would lead its group forever, with no way to stop it. Toggle must
// be a real switch, not a one-way door.
func TestToggleOnThenOff(t *testing.T) {
	s := empty()
	if !s.Toggle("agent", "geoff", t0) {
		t.Fatal("the first Toggle reported not-starred — the pin never landed")
	}
	if !s.Has("agent", "geoff") {
		t.Fatal("Has denied a star Toggle had just added")
	}
	if len(s.Stars) != 1 || s.Stars[0].AddedUnixMs != t0.UnixMilli() {
		t.Errorf("stars = %+v, want one star recording t0", s.Stars)
	}
	if s.Toggle("agent", "geoff", t0.Add(time.Minute)) {
		t.Error("the second Toggle reported starred — the pin cannot be removed")
	}
	if s.Has("agent", "geoff") {
		t.Error("Has confirmed a star Toggle had just removed")
	}

	// A star with no kind or key could never be matched by a row, so it
	// could never be unstarred or pruned either: unmatchable junk in the
	// file forever. Refuse it at the door.
	if s.Toggle("agent", "", t0) || s.Toggle("", "geoff", t0) {
		t.Error("Toggle accepted an empty kind or key")
	}
	if len(s.Stars) != 0 {
		t.Errorf("stars = %+v, want none", s.Stars)
	}
}

// The row layer asks one question — "is this row pinned?" — for two kinds
// of identity, and kind is part of that identity: a name and a pane id are
// different stars even when the strings collide. If Has mis-answers any of
// these, a pinned agent quietly stops leading its group while the file
// still says it is starred: the pin exists, but the row cannot find it.
func TestHasByBothKinds(t *testing.T) {
	s := empty()
	s.Toggle("agent", "geoff", t0)
	s.Toggle("pane", "pane-7", t0)

	for _, tc := range []struct {
		kind, key string
		want      bool
	}{
		{"agent", "geoff", true},
		{"pane", "pane-7", true},
		{"agent", "pane-7", false}, // same string, other kind: not this star
		{"pane", "geoff", false},
		{"agent", "nobody", false},
		{"pane", "nobody", false},
	} {
		if got := s.Has(tc.kind, tc.key); got != tc.want {
			t.Errorf("Has(%q, %q) = %v, want %v", tc.kind, tc.key, got, tc.want)
		}
	}
}

// Set is the row layer's only lens on this file, and it must speak the same
// key language the wiring does. If Set keyed stars differently than the row
// layer builds keys, every pin would silently miss — stars present in the
// file, none of them leading anything.
func TestSetKeysByKindAndKey(t *testing.T) {
	s := empty()
	s.Toggle("agent", "geoff", t0)
	s.Toggle("pane", "pane-7", t0)

	got := Set(s)
	for _, key := range []string{"agent:geoff", "pane:pane-7"} {
		if !got[key] {
			t.Errorf("Set is missing %q — the row layer would not find this pin", key)
		}
	}
	for _, key := range []string{"agent:pane-7", "pane:geoff", "agent:nobody"} {
		if got[key] {
			t.Errorf("Set invented %q — a pin nobody toggled", key)
		}
	}
}

// Prune keeps a long-lived file from hoarding a star for every pane the
// machine ever had, and the two judgments it makes are the ones a user
// would feel:
//
//   - A named agent whose pane was destroyed and recreated KEEPS its pin —
//     the name survived, and so must the pin. Dropping it would make
//     starring no more durable than the pane id it exists to beat.
//   - A star on an unnamed agent dies with its pane — the pane id was the
//     only identity it ever had. That is the documented cost of starring
//     an unnamed agent, not a bug.
func TestPruneDropsOnlyTheDead(t *testing.T) {
	s := empty()
	s.Toggle("agent", "geoff", t0)    // named; its pane is gone, the name lives on
	s.Toggle("agent", "ghost", t0)    // named; the agent itself is gone
	s.Toggle("pane", "pane-7", t0)    // unnamed; pane still live
	s.Toggle("pane", "pane-dead", t0) // unnamed; pane gone, agent gone with it

	// geoff's original pane appears nowhere in livePanes: it was recreated
	// under a new id. Only the name says geoff still exists.
	s.Prune(map[string]bool{"geoff": true}, map[string]bool{"pane-7": true})

	want := map[string]bool{"agent:geoff": true, "pane:pane-7": true}
	got := Set(s)
	for k := range want {
		if !got[k] {
			t.Errorf("Prune dropped %q — a live target lost its pin", k)
		}
	}
	for k := range got {
		if !want[k] {
			t.Errorf("Prune kept %q — a dead target is still pinned", k)
		}
	}
}

// Every failure mode of the file must degrade to "no pins", never to an
// error: a star is a convenience, and a corrupt or future file the user
// never opens by hand must not be able to stop `list` from opening at all.
func TestLoadDegradesToEmpty(t *testing.T) {
	dir := t.TempDir()

	// The unreadable case is a directory rather than a chmod-0 file,
	// because reading a directory fails even for root: the test needs no
	// skip-if-root escape hatch and cannot silently pass as root.
	unreadable := filepath.Join(dir, "unreadable")
	if err := os.Mkdir(unreadable, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "malformed.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	future := filepath.Join(dir, "future.json")
	body := `{"version":999,"stars":[{"kind":"agent","key":"geoff"}]}`
	if err := os.WriteFile(future, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		path string
	}{
		{"missing file", filepath.Join(dir, "nope.json")},
		{"unreadable path", unreadable},
		{"malformed json", filepath.Join(dir, "malformed.json")},
		{"future version", future},
	} {
		if got := Load(tc.path); len(got.Stars) != 0 || got.Version != Version {
			t.Errorf("%s: Load = %+v, want empty stars at version %d — a bad file must cost the pins, never the command",
				tc.name, got, Version)
		}
	}
}

// A star that died with the process that recorded it would be a pin that
// lasts one picker session. Update is the only write path and Load the only
// read path, so this round trip is what a real pin depends on.
func TestUpdatePersistsToggle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stars.json")

	if err := Update(path, func(s *Stars) { s.Toggle("agent", "geoff", t0) }); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	loaded := Load(path)
	if !loaded.Has("agent", "geoff") {
		t.Error("a star written by Update was missing on reload — the pin did not outlive the process that made it")
	}

	if err := Update(path, func(s *Stars) { s.Toggle("agent", "geoff", t0) }); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	loaded = Load(path)
	if loaded.Has("agent", "geoff") {
		t.Error("an unstar written by Update was still there on reload — the pin could not be put down")
	}
}

// The writers are independent short-lived processes, and this is the race
// the whole locking design exists to prevent: several in flight at once —
// two picker windows, or a pin racing a prune — must all land. With the
// lock held only around the write, every writer would Load the same file,
// mutate its own copy, and write; the last one out would silently erase
// the others' pins. The tally is also the atomicity check: a torn write
// would fail to parse, Load would degrade to empty, and the count would
// be zero.
func TestUpdateSerialisesConcurrentWriters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stars.json")

	const n = 16
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := "agent-" + string(rune('a'+i))
			if err := Update(path, func(s *Stars) { s.Toggle("agent", key, t0) }); err != nil {
				t.Errorf("writer %d failed: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	got := Set(Load(path))
	if len(got) != n {
		t.Fatalf("%d of %d pins survived concurrent writers — a user's star was silently erased", len(got), n)
	}
	for i := 0; i < n; i++ {
		if key := "agent:agent-" + string(rune('a'+i)); !got[key] {
			t.Errorf("pin %q was lost to a concurrent writer", key)
		}
	}
}
