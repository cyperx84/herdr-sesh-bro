// Attaching to another herdr session is the one cross-session action that
// works, and it works by leaving herdr entirely.
//
// A herdr session IS a daemon: N sessions are N `herdr server` processes, each
// with its own socket, and NO API method anywhere takes a session or host
// parameter (docs/MULTI-SESSION.md, re-verified against protocol 22 — the only
// `session_id` in the whole schema is `agent_session_id`, the coding agent's
// own identity). So there is no call that focuses session B from session A.
// What there is, is a client: `herdr session attach <name>` takes over a
// terminal. Give it a terminal of its own and the user lands in session B.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// attachEnvPrefixes name the variables that must NOT reach the spawned
// terminal.
//
// This is the load-bearing part of the whole mechanism, learned the hard way
// against a live daemon. macOS `open` passes the caller's environment to the
// application it launches, so a terminal spawned from inside a herdr pane
// inherits HERDR_ENV=1 along with the pane, tab and socket variables — and
// herdr refuses to start nested:
//
//	error: nested herdr is disabled by default.
//	see configuration if you want to enable it.
//
// The refusal is correct and the fix is not to disable it. A terminal opened to
// attach a DIFFERENT session is not a nested herdr; it only looks like one
// because of variables that leaked. Stripping them is what makes the spawn
// truthful about what it is.
var attachEnvPrefixes = []string{"HERDR_"}

// AttachCmdVar is the template that turns a session name into a command line.
const AttachCmdVar = "SESH_BRO_ATTACH_CMD"

// attachPlaceholder is replaced by the session name.
const attachPlaceholder = "{session}"

// defaultAttachCmd is the macOS default, matching the shape already proven on
// this machine by the Hammerspoon summon binding.
//
// Linux gets NO default, on purpose. Terminal emulators vary too much for a
// guess to be anything but wrong on most machines, and the failure mode of a
// wrong guess is a command that appears to do nothing. Refusing loudly, and
// naming the variable to set, is strictly more useful than opening nothing.
const defaultAttachCmd = "open -na Ghostty --args -e {herdr} session attach " + attachPlaceholder

// herdrPlaceholder is replaced by an ABSOLUTE path to the herdr binary.
//
// Absolute, not the bare name, because macOS `open` does not hand the
// application the caller's PATH in any dependable way — the terminal launches
// with a login environment, and herdr installed by Homebrew on a machine whose
// login shell has not been configured for it is simply not found. The failure
// is a terminal that flashes open and closes, which reads as "the key does
// nothing".
const herdrPlaceholder = "{herdr}"

// lookHerdr is a seam so tests do not depend on herdr being installed.
var lookHerdr = func() string {
	if p, err := exec.LookPath("herdr"); err == nil {
		return p
	}
	return "herdr"
}

// attachCommand resolves the template for a session name.
func attachCommand(getenv func(string) string, session string) (string, error) {
	tmpl := getenv(AttachCmdVar)
	if tmpl == "" {
		if runtime.GOOS != "darwin" {
			return "", fmt.Errorf(
				"sesh-bro: attaching to another session needs a terminal command; set %s (a shell command containing %s)",
				AttachCmdVar, attachPlaceholder)
		}
		tmpl = strings.ReplaceAll(defaultAttachCmd, herdrPlaceholder, lookHerdr())
	}
	if !strings.Contains(tmpl, attachPlaceholder) {
		return "", fmt.Errorf("sesh-bro: %s must contain %s, so it knows which session to attach", AttachCmdVar, attachPlaceholder)
	}
	// The session name is substituted into a shell command line, so it must
	// not be able to end the command and start another. Session names come
	// from `herdr session list --json`, not from a stranger — but a name is
	// still a string herdr let somebody choose, and the difference between
	// "trusted enough" and "trusted" is exactly the sort of thing that turns
	// into a shell injection later.
	if strings.ContainsAny(session, "'\"`$;&|<>\n") {
		return "", fmt.Errorf("sesh-bro: refusing to attach a session whose name contains shell metacharacters: %q", session)
	}
	return strings.ReplaceAll(tmpl, attachPlaceholder, session), nil
}

// attachEnv is the current environment with the herdr variables removed.
func attachEnv(environ []string) []string {
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		drop := false
		for _, p := range attachEnvPrefixes {
			if strings.HasPrefix(kv, p) {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}

// runAttach is the seam tests replace: production spawns a process, tests
// record the argv and environment and spawn nothing.
var runAttach = func(ctx context.Context, command string, environ []string) error {
	// Through a shell because the template is a command LINE, not an argv:
	// the macOS default alone needs `open -na … --args -e …`, and asking a
	// user to express their terminal's invocation as a JSON array would be a
	// worse interface than the one thing every terminal emulator's own
	// documentation already gives them.
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = environ
	// Deliberately detached from this process's streams. The picker is a
	// full-screen fzf; a terminal emulator writing its startup noise into that
	// screen would corrupt it, and the spawned terminal has its own.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	return cmd.Start()
}

// attachSession opens a terminal attached to another session.
func attachSession(ctx context.Context, env *appEnv, session string) error {
	command, err := attachCommand(env.getenv, session)
	if err != nil {
		return err
	}
	if err := runAttach(ctx, command, attachEnv(os.Environ())); err != nil {
		return fmt.Errorf("sesh-bro: attach %s: %w", session, err)
	}
	return nil
}
