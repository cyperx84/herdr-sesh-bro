// Package attention remembers when each agent entered its current state, and
// which workspaces you visited most recently.
//
// herdr exposes neither. There is no state_changed_at anywhere in the 0.8.2
// API — herdr discussion #707 asked for it (7 upvotes, "such a killer
// feature") and #3619 asks again; #2034 asked and was closed. The only signal
// is state_change_seq, a global monotonic counter that orders changes but
// cannot measure them. So "blocked for 9m" has to be timed locally, and
// something has to be watching when the change happens.
//
// That something is a herdr [[events]] hook, not a daemon. herdr runs a
// plugin command per event, and — verified against 0.8.2 — those hooks fire
// for every pane with no per-pane registration, unlike events.subscribe whose
// status subscription demands a pane_id. So the cost of this feature is one
// short-lived process per state change, which on a busy machine is a handful
// per minute. A resident process would be a much larger footprint for the
// same answer.
package attention

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Version is the on-disk schema version. A file from a future version is
// ignored rather than misread: the data is a convenience, and losing it costs
// a duration badge, not correctness.
const Version = 1

// maxMRU bounds the recent-workspace list. Long enough that `last` is right
// after a detour, short enough that the file stays small.
const maxMRU = 32

// PaneState is when a pane's agent entered the state it is in now.
type PaneState struct {
	Status string `json:"status"`
	// SinceUnixMs is when this status began, by the recording hook's clock.
	SinceUnixMs int64  `json:"since_unix_ms"`
	WorkspaceID string `json:"workspace_id"`
}

// State is the whole recorded model.
type State struct {
	Version int                  `json:"version"`
	Panes   map[string]PaneState `json:"panes"`
	// MRUWorkspaces is most-recently-focused first.
	MRUWorkspaces []string `json:"mru_workspaces"`
	UpdatedUnixMs int64    `json:"updated_unix_ms"`
}

// New returns an empty, usable State.
func New() State {
	return State{Version: Version, Panes: map[string]PaneState{}}
}

// Load reads path. A missing, unreadable, malformed, or future-versioned file
// yields an empty State and no error: every consumer of this data degrades to
// "no badge, no MRU", which is the pre-0.4.0 behaviour and not worth failing a
// command over.
func Load(path string) State {
	b, err := os.ReadFile(path)
	if err != nil {
		return New()
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return New()
	}
	if s.Version != Version {
		return New()
	}
	if s.Panes == nil {
		s.Panes = map[string]PaneState{}
	}
	return s
}

// ApplyStatus records a pane's status change.
//
// Two rules, both load-bearing:
//
// An unchanged status does NOT restart the clock. herdr re-reports a pane's
// status on events that changed something else, and treating those as
// transitions would make every badge read "0s" forever.
//
// done -> idle does not restart it either. Those are the same underlying
// state: done is idle-with-unseen-work, and it becomes idle the moment you
// look at the tab. An agent that finished twenty minutes ago and that you
// glanced at has still been waiting twenty minutes, and resetting on the
// glance would erase exactly the number the user wanted (the consensus in
// herdr discussion #707).
func (s *State) ApplyStatus(paneID, workspaceID, status string, now time.Time) {
	if paneID == "" {
		return
	}
	if s.Panes == nil {
		s.Panes = map[string]PaneState{}
	}
	prev, existed := s.Panes[paneID]
	next := PaneState{Status: status, SinceUnixMs: now.UnixMilli(), WorkspaceID: workspaceID}
	if existed && sameClock(prev.Status, status) {
		next.SinceUnixMs = prev.SinceUnixMs
	}
	if workspaceID == "" {
		next.WorkspaceID = prev.WorkspaceID
	}
	s.Panes[paneID] = next
	s.UpdatedUnixMs = now.UnixMilli()
}

// sameClock reports whether two statuses should share one start time.
func sameClock(prev, next string) bool {
	if prev == next {
		return true
	}
	return prev == "done" && next == "idle"
}

// ApplyFocus records a workspace visit, moving it to the front.
func (s *State) ApplyFocus(workspaceID string, now time.Time) {
	if workspaceID == "" {
		return
	}
	out := make([]string, 0, len(s.MRUWorkspaces)+1)
	out = append(out, workspaceID)
	for _, id := range s.MRUWorkspaces {
		if id == workspaceID {
			continue
		}
		out = append(out, id)
		if len(out) == maxMRU {
			break
		}
	}
	s.MRUWorkspaces = out
	s.UpdatedUnixMs = now.UnixMilli()
}

// Prune drops panes and workspaces that no longer exist, so a long-lived file
// does not accumulate every pane the machine ever had.
func (s *State) Prune(livePanes, liveWorkspaces map[string]bool) {
	for id := range s.Panes {
		if !livePanes[id] {
			delete(s.Panes, id)
		}
	}
	kept := s.MRUWorkspaces[:0]
	for _, id := range s.MRUWorkspaces {
		if liveWorkspaces[id] {
			kept = append(kept, id)
		}
	}
	s.MRUWorkspaces = kept
}

// Since returns how long a pane has been in its current status.
//
// ok is false when the pane is unknown (nothing recorded it yet) or when the
// recorded status disagrees with what the caller sees — a disagreement means
// the hook missed a transition, and a badge derived from a stale start time
// would be confidently wrong, which is worse than absent.
func Since(s State, paneID, status string, now time.Time) (time.Duration, bool) {
	ps, ok := s.Panes[paneID]
	if !ok || ps.Status != status || ps.SinceUnixMs == 0 {
		return 0, false
	}
	d := now.Sub(time.UnixMilli(ps.SinceUnixMs))
	if d < 0 {
		// Clock moved backwards; report nothing rather than a negative age.
		return 0, false
	}
	return d, true
}

// FormatAge renders a duration for a row badge: short, fixed-width-ish, and
// never more precise than it is useful. Seconds below a minute, minutes below
// an hour, hours and minutes below a day, then days.
func FormatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) - h*60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// Save writes state to path atomically, under an exclusive lock.
//
// Both are needed because the writers are independent short-lived processes:
// herdr can invoke several event hooks at once, and each does
// read-modify-write on this file. The lock serialises them so one does not
// clobber another's change; the temp-file-and-rename means a reader never sees
// a half-written file.
func Save(path string, s State) error {
	if s.Version == 0 {
		s.Version = Version
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("attention: create state dir: %w", err)
	}

	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("attention: open lock: %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("attention: lock: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("attention: marshal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("attention: write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("attention: rename: %w", err)
	}
	return nil
}
