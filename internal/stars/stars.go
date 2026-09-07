// Package stars stores which agents the user pinned, so a pinned agent
// leads its group in the picker regardless of what the sort order or the
// attention clock says. This file is the storage and identity layer only;
// the picker wiring is deliberately not here.
//
// Why its own file, and not a field inside attention's state.json: a
// recorded pane clock is derived, disposable data — written constantly by
// event hooks, and its loss costs a badge. A star is the opposite on every
// axis: an explicit thing a human did, written rarely, whose loss is a bug.
// Sharing a file would mean the noisiest writer (the hooks, several a
// minute on a busy machine) is the one most likely to clobber the quietest
// (a pin, written when a person pressed a key). attention.Update's own
// comment says its locking "stops being cosmetic the moment anything the
// user typed lives in here" — this package is that moment. The separation
// is half the answer to it; the whole-read-modify-write lock below is the
// other half.
//
// Identity: star an agent by NAME when it has one, because a name survives
// the pane being recreated — the pin follows the agent, not the window it
// happened to be in. Fall back to pane id when it has no name, and accept
// plainly that starring an unnamed agent therefore lasts only as long as
// its pane. Do not try to make that durable by naming the agent on the
// user's behalf: renaming something unasked is a side effect the user did
// not request, and a star must never cause one.
package stars

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Version is the on-disk schema version. A file from a future version is
// ignored rather than misread: this build does not know that layout, and
// guessing at it could turn a curated pin list into nonsense. Degrading
// costs the pins until the file is rewritten, never the command.
const Version = 1

// Star is one pin: an agent the user wants leading its group.
type Star struct {
	// Kind is how the star names its target: "agent" (by name) or "pane"
	// (by pane id). Which one to use is the identity rule in the package
	// comment, and applying it — name when the agent has one, pane id
	// when it does not — is the caller's job; this package records what
	// it is told.
	Kind string `json:"kind"`
	// Key is the name or pane id that Kind denotes.
	Key string `json:"key"`
	// AddedUnixMs is when the user pinned it. Nothing reads it yet; it is
	// recorded because a file a human curates should be able to say when
	// they did.
	AddedUnixMs int64 `json:"added_unix_ms"`
}

// Stars is the whole recorded model.
type Stars struct {
	Version int    `json:"version"`
	Stars   []Star `json:"stars"`
}

// empty is the value every failure of the file degrades to.
func empty() Stars {
	return Stars{Version: Version}
}

// Load reads path. A missing, unreadable, malformed, or future-versioned
// file yields empty Stars and no error, exactly like attention.Load: a
// star is a convenience, so a bad file must cost the pins, never the
// command — `list` still opens, it just puts nothing first.
func Load(path string) Stars {
	b, err := os.ReadFile(path)
	if err != nil {
		return empty()
	}
	var s Stars
	if err := json.Unmarshal(b, &s); err != nil {
		return empty()
	}
	if s.Version != Version {
		return empty()
	}
	return s
}

// Toggle pins kind+key, or unpins it if already pinned, and reports
// whether it is pinned now.
//
// There is deliberately no cap on how many stars the file may hold:
// attention bounds its MRU list because that data is derived and
// disposable, but a cap here would throw away a pin to save a byte —
// losing the exact thing this file exists to keep.
//
// An empty kind or key is refused rather than recorded: a star no row
// could ever match could never be unstarred or pruned either, and would
// be junk in the file forever.
func (s *Stars) Toggle(kind, key string, now time.Time) bool {
	if kind == "" || key == "" {
		return false
	}
	for i, st := range s.Stars {
		if st.Kind == kind && st.Key == key {
			s.Stars = append(s.Stars[:i], s.Stars[i+1:]...)
			return false
		}
	}
	s.Stars = append(s.Stars, Star{Kind: kind, Key: key, AddedUnixMs: now.UnixMilli()})
	return true
}

// Has reports whether kind+key is pinned. Kind is part of identity: an
// agent pinned by name and the same agent pinned by pane id are different
// stars, found by different rows.
func (s *Stars) Has(kind, key string) bool {
	for _, st := range s.Stars {
		if st.Kind == kind && st.Key == key {
			return true
		}
	}
	return false
}

// Prune drops stars whose referent no longer exists, so a long-lived file
// does not hoard a star for every pane the machine ever had.
//
// liveAgents is keyed by agent NAME, livePanes by pane id — the same keys
// the two kinds of star carry — and each star is judged by its own kind's
// map. That is the identity rule doing its job: a named agent whose pane
// was destroyed and recreated keeps its pin, because the name survived;
// a star on an unnamed agent dies with its pane, because the pane id was
// the only identity it ever had. A kind this build does not know cannot
// be vouched for by either map, so it is dropped — the conservative
// direction for a file whose entries should all be live.
func (s *Stars) Prune(liveAgents, livePanes map[string]bool) {
	kept := s.Stars[:0]
	for _, st := range s.Stars {
		live := false
		switch st.Kind {
		case "agent":
			live = liveAgents[st.Key]
		case "pane":
			live = livePanes[st.Key]
		}
		if live {
			kept = append(kept, st)
		}
	}
	s.Stars = kept
}

// Set turns stars into the lookup the row layer holds while it renders:
// one question per row — "is this row's agent pinned?" — answered by the
// key "<kind>:<key>". The row layer builds the same string for the agent
// it is rendering (name when named, pane id when not), so the two sides
// never disagree about who is who.
func Set(stars Stars) map[string]bool {
	out := make(map[string]bool, len(stars.Stars))
	for _, st := range stars.Stars {
		out[st.Kind+":"+st.Key] = true
	}
	return out
}

// Update applies mutate to the stored stars under an exclusive lock held
// across the whole read-modify-write, then replaces the file atomically.
//
// The shape is attention's, deliberately, because the race is the same one
// and the stakes are the ones its comment predicted. The writers are
// independent short-lived processes — a pin typed in the picker, a prune
// at the next herdr server start — and with the lock held only around the
// write, two
// of them could both Load, both mutate their own copy, and both write,
// losing one change silently. In attention that costs a badge; here it
// would erase something a human did. There is deliberately no Save: every
// change to this file derives from what is already in it, so Update is the
// only write path, and a Load-then-Save pair — the exact shape with the
// lost-update race — cannot be written against this package.
func Update(path string, mutate func(*Stars)) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("stars: create state dir: %w", err)
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("stars: open lock: %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("stars: lock: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	// Read INSIDE the lock. That is the entire point.
	s := Load(path)
	mutate(&s)
	return writeLocked(path, s)
}

// writeLocked marshals and atomically replaces the file. The caller holds
// the lock; temp-file-and-rename means a reader — including one about to
// become a writer itself — never sees a half-written file.
func writeLocked(path string, s Stars) error {
	if s.Version == 0 {
		s.Version = Version
	}
	if s.Stars == nil {
		// A file a human might open by hand deserves [] over null.
		s.Stars = []Star{}
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("stars: marshal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("stars: write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("stars: rename: %w", err)
	}
	return nil
}
