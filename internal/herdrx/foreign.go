package herdrx

import (
	"context"
	"strconv"
	"strings"
	"sync"
)

// SessionSeparator joins an id to the session it belongs to, producing the
// globally unique target a foreign row needs.
//
// Why a separator is needed at all: ids are scoped to one server. herdr's own
// skill file says so plainly — "Two saved SSH machines can both have `w1:p1`
// or an agent named `reviewer`" — and the same is true of two local sessions.
// The picker's rows must be uniquely addressable (fzf's --id-nth tracks the
// highlighted row across reloads by them), so "w1:p1" from two sessions cannot
// both appear as themselves.
//
// "@" is an ASSUMPTION, not a verified fact, and it is written down as one.
// herdr documents no grammar for session names, and the only constraint
// visible from outside is that a name becomes a directory under
// ~/.config/herdr/sessions/, which rules out "/" and very little else. "@" is
// legal in a filename, so a session could in principle be named "a@b" and make
// a composite target ambiguous. ForeignRows therefore SKIPS any session whose
// name contains this separator rather than emitting a row nothing could
// resolve — visibly missing beats silently wrong — and TestSessionSeparator
// pins the choice so a future change has to be deliberate.
const SessionSeparator = "@"

// ForeignTarget composes the unique target for something inside another
// session.
func ForeignTarget(id, session string) string { return id + SessionSeparator + session }

// SplitForeignTarget reverses ForeignTarget, reporting whether the target named
// a foreign session at all. A local target has no separator and comes back
// unchanged with ok=false, so one call classifies as well as parses.
func SplitForeignTarget(target string) (id, session string, ok bool) {
	// Cut from the RIGHT. A pane id contains no "@", but this way an id that
	// somehow did would still yield the correct session, which is the half
	// that decides whether a mutating command must refuse.
	i := strings.LastIndex(target, SessionSeparator)
	if i < 0 {
		return target, "", false
	}
	return target[:i], target[i+1:], true
}

// ForeignSnapshot is one other session's state, or the reason there is none.
type ForeignSnapshot struct {
	Session  Session
	Snapshot Snapshot
	// Err is why this session contributed nothing. A session that will not
	// answer must not fail the render — the local rows are the ones the user
	// is actually working in — so the error is carried per session and left
	// for the caller to report or ignore.
	Err error
}

// ForeignSnapshots dials every running session other than the one at
// selfSocket and snapshots it.
//
// Reading a foreign socket is the one cross-session thing that works: sockets
// carry no auth beyond file permissions, and session.snapshot is a pure read
// (docs/MULTI-SESSION.md). Every MUTATING call is refused elsewhere, because
// it would succeed against a session the user cannot see.
//
// Concurrent, because the cost here is N round trips to N daemons and they are
// independent; serial dialling would make the picker's foreign refresh scale
// with the slowest session on the machine.
func ForeignSnapshots(ctx context.Context, sessions []Session, selfSocket string) []ForeignSnapshot {
	var wanted []Session
	for _, s := range sessions {
		if !s.Running || s.SocketPath == selfSocket || s.SocketPath == "" {
			continue
		}
		wanted = append(wanted, s)
	}
	if len(wanted) == 0 {
		return nil
	}

	out := make([]ForeignSnapshot, len(wanted))
	var wg sync.WaitGroup
	for i, s := range wanted {
		wg.Add(1)
		go func(i int, s Session) {
			defer wg.Done()
			out[i] = ForeignSnapshot{Session: s}
			client, err := OpenPath(s.SocketPath)
			if err != nil {
				out[i].Err = err
				return
			}
			snap, err := client.SessionSnapshot(ctx)
			if err != nil {
				out[i].Err = err
				return
			}
			out[i].Snapshot = snap
		}(i, s)
	}
	wg.Wait()
	return out
}

// RowSession is a whole other herdr session, offered as one row: Enter on it
// opens a terminal attached to that session.
//
// It exists beside the per-agent rows rather than instead of them because the
// two answer different questions. "Is anything happening over there" is
// answered by the agents; "take me over there" is answered by this.
const RowSession RowType = "session"

// RowRAgent is an agent inside another session — an "r" for remote, kept short
// because the type is TSV field 1 and also the preview bind's first argument.
//
// A separate type from RowAgent, rather than an agent row with Session set,
// because the type is what every consumer switches on: connect, preview and
// close all dispatch on it, and a foreign agent must reach different code in
// all three. A shared type would make "did you remember to check Session"
// the correctness condition in every one of them.
const RowRAgent RowType = "ragent"

// foreignStatus is the placeholder for a session row, which has no status of
// its own to report. It matches dirStatus for the same reason dirStatus
// exists: the column is positional and must be filled.
const foreignStatus = "-"

// ForeignRows converts other sessions' snapshots into read-only picker rows.
//
// One session row per session, plus one ragent row per agent in it. Workspaces
// and directories are deliberately absent: a foreign workspace cannot be
// focused, and the only action a foreign session offers is "attach me to it",
// which the session row already provides — a list of workspaces nobody can
// open is noise wearing the shape of a feature.
//
// A session whose snapshot failed contributes NOTHING, not an error row. The
// foreign view is secondary, it refreshes on a ticker, and a session that is
// shutting down would otherwise spend several seconds as an error the user can
// do nothing about.
//
// A session whose NAME contains SessionSeparator is skipped for the reason
// that constant documents: its composite targets would be ambiguous, and a row
// nothing can resolve is worse than a row that is not there.
func ForeignRows(snaps []ForeignSnapshot) []Row {
	var rows []Row
	for _, fs := range snaps {
		if fs.Err != nil {
			continue
		}
		name := fs.Session.Name
		if name == "" || strings.Contains(name, SessionSeparator) {
			continue
		}

		agents := fs.Snapshot.Agents
		rows = append(rows, Row{
			Type:    RowSession,
			Target:  name,
			Status:  foreignStatus,
			Label:   name,
			Detail:  foreignSessionDetail(fs.Snapshot),
			Session: name,
		})

		for _, a := range agents {
			a.Status = NormalizeStatus(a.Status)
			id := a.Name
			if id == "" {
				id = a.PaneID
			}
			if id == "" {
				continue
			}
			label := a.Name
			if label == "" && a.Agent.Agent != nil {
				label = *a.Agent.Agent
			}
			if label == "" {
				label = a.PaneID
			}
			rows = append(rows, Row{
				Type: RowRAgent,
				// Composed, because ids are scoped to one server and two
				// sessions can both hold "w1:p1".
				Target:  ForeignTarget(id, name),
				Status:  string(a.Status),
				Label:   label,
				Detail:  agentDetail(a) + " · " + name,
				PaneID:  a.PaneID,
				Session: name,
			})
		}
	}
	return rows
}

// foreignSessionDetail summarises a session in the space a detail column has:
// how much is in it, and how much of that wants a human.
func foreignSessionDetail(snap Snapshot) string {
	attention := 0
	for _, a := range snap.Agents {
		if isAttentionStatus(string(NormalizeStatus(a.Status))) {
			attention++
		}
	}
	d := strconv.Itoa(len(snap.Workspaces)) + "w/" + strconv.Itoa(len(snap.Agents)) + "a"
	if attention > 0 {
		d += " · " + strconv.Itoa(attention) + " waiting"
	}
	return d
}
