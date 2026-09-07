package herdrx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// Session is one entry of `herdr session list --json`. It is the only way
// to enumerate herdr sessions: the CLI is CLI-only — there is no
// session.list RPC, and the entire session.* socket namespace is a single
// method, session.snapshot (docs/MULTI-SESSION.md, "A session is a daemon").
// That is why this file shells out where the rest of this package dials a
// socket: there is nothing to dial for enumeration.
type Session struct {
	Name       string `json:"name"`
	Default    bool   `json:"default"`
	Running    bool   `json:"running"`
	SessionDir string `json:"session_dir"`
	SocketPath string `json:"socket_path"`
}

// sessionListResult mirrors the shape `herdr session list --json` emits:
// {"sessions": [...]}. Kept unexported so the wire shape can change without
// widening this package's API.
type sessionListResult struct {
	Sessions []Session `json:"sessions"`
}

// Runner executes a herdr CLI invocation and returns its raw stdout. It is
// the seam that lets tests supply canned output with no herdr binary on
// PATH; production callers leave it nil and get execRunner below. Mirrors
// the run/getenv seams of the command layer in spirit: no process-wide
// state is mutated to fake a subprocess.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// execRunner is the default Runner: a real exec.CommandContext. Output goes
// through a bytes.Buffer rather than cmd.Output() so a non-zero exit is
// still distinguishable (ExitError) while the captured stdout stays
// available for the error path — same observable behaviour, less magic.
func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	var buf bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ListSessions runs `<herdrBin> session list --json` and decodes the
// sessions it reports.
//
// herdrBin empty means "herdr" — callers shouldn't have to know the binary's
// name just to get the default, and the empty string is otherwise the one
// input that would make exec look for a file literally called "".
//
// Enumeration MUST go through this CLI call, never through globbing
// ~/.config/herdr/sessions/*: the DEFAULT session's session_dir is
// ~/.config/herdr itself, not sessions/default/, so a glob enumerates every
// session except the one almost everybody is using (docs/MULTI-SESSION.md's
// own "gotcha worth the whole paragraph"). A headless CLI that globbed would
// look fine on a multi-session setup and silently miss the common case.
//
// No caching here by design; a later task wires that in.
func ListSessions(ctx context.Context, herdrBin string, run Runner) ([]Session, error) {
	if herdrBin == "" {
		herdrBin = "herdr"
	}
	if run == nil {
		run = execRunner
	}
	out, err := run(ctx, herdrBin, "session", "list", "--json")
	if err != nil {
		return nil, fmt.Errorf("herdrx: list sessions: %w", err)
	}
	var res sessionListResult
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, fmt.Errorf("herdrx: list sessions: parse output: %w", err)
	}
	return res.Sessions, nil
}

// DefaultRunningSocket returns the socket path a headless command should
// dial: the session marked default that is also running.
//
// Trust the `running` flag rather than probing SocketPath: a stopped
// session has no socket file at all (the daemon unlinks it on exit, so a
// dial gets ENOENT), and probing would race a session that stops between
// the stat and the dial anyway.
//
// If the default session isn't running, fall back to any single running
// session — the common one-machine case where "default" was just never
// started today. If several sessions are running and none is default,
// report not-found rather than guessing: an arbitrary pick would silently
// attach a headless command to the wrong session's daemon, and every
// request is implicitly scoped to the socket it dialled (there is no
// session parameter on any 0.8.2 request), so nothing downstream could
// even detect the mistake.
func DefaultRunningSocket(sessions []Session) (string, bool) {
	var fallback string
	running := 0
	for _, s := range sessions {
		if !s.Running {
			continue
		}
		running++
		fallback = s.SocketPath
		if s.Default {
			return s.SocketPath, true
		}
	}
	if running == 1 {
		return fallback, true
	}
	return "", false
}
