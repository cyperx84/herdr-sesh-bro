// This file is the one place that dials the herdr socket. Every subcommand
// gets its Client (or the reason it doesn't have one) from openHerdr, so the
// "what happens when herdr is unreachable" decision is made once instead of
// once per file.
package main

import (
	"context"
	"os/exec"

	"github.com/cyperx84/herdr-sesh-bro/internal/external"
	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx"
)

// constAliver is an external.Aliver that never actually asks anything —
// used when the socket couldn't even be dialed, so CheckDeps/CheckListDeps
// still get an Aliver to call and still produce the correct "daemon is not
// responding" diagnostic (rather than a distinct, undocumented error path).
type constAliver bool

func (a constAliver) Alive(context.Context) bool { return bool(a) }

// openHerdr dials the herdr socket, mirroring what bash's `$HERDR` calls
// implicitly assume is reachable. It never itself prints anything or exits —
// callers decide how "no client" surfaces for their specific subcommand,
// because bash's own behaviour differs by call site:
//
//   - `list`/`startup` gate explicitly on herdr_ok before ever touching the
//     daemon (BEHAVIOUR.md §7.1) — those commands call CheckDeps/
//     CheckListDeps with the returned Aliver and report ITS message.
//   - `connect`/`create`/`preview`/`last`/`root`/`worktree` never check
//     first (BEHAVIOUR.md §7.2: "—" across the board) — an unreachable
//     daemon there just makes the specific herdr call fail, which each of
//     those commands already has a bash-specified failure message for. Pass
//     openErr straight through to the small wrapper functions in
//     herdrcalls.go, which turn "couldn't dial" into the exact same failure
//     those commands report for any other herdr-call error.
//
// getenv is the getenv run() already threads through every subcommand
// (appEnv.getenv — normally os.Getenv), NOT a new seam: resolving
// HERDR_SOCKET_PATH through it means a command-level test injects the
// tests-only fake socket server (internal/herdrx/herdrtest) through the
// same fakeEnv every other env var uses, with no process-wide mutation.
// For a real run the resolution is identity: HERDR_SOCKET_PATH set means
// herdrx.OpenPath(path), unset means exactly today's behaviour
// (herdrx.Open, which reads os.Getenv itself and fails with herdr-api's
// own no-socket error — unchanged).
//
// herdr-api's Client.Open dials $HERDR_SOCKET_PATH only, with no fallback
// (herdr-api client.go:118-124) — unlike bash's `herdr` CLI, which has its
// own daemon-discovery mechanism independent of any single env var. Running
// this binary from a plain shell outside a herdr pane (HERDR_SOCKET_PATH
// unset) therefore reports "daemon is not responding" where the bash CLI
// might have found the daemon another way. This is a real, load-bearing
// divergence from herdr-api's own dependency, not an oversight in this
// package — see the final report.
func openHerdr(getenv func(string) string) (*herdrx.Client, error) {
	if path := getenv("HERDR_SOCKET_PATH"); path != "" {
		return herdrx.OpenPath(path)
	}

	// No socket in the environment. herdr only injects HERDR_SOCKET_PATH into
	// panes it spawned, so this is every invocation from an ordinary shell —
	// which, since 0.5.0, includes another coding agent driving sesh-bro
	// headlessly. Before this fallback such a command failed before it did
	// anything, with an error about a variable the caller had never heard of.
	//
	// `herdr session list --json` is the only correct enumeration (see
	// docs/MULTI-SESSION.md), and DefaultRunningSocket refuses to guess when
	// several sessions are running and none is default.
	if sessions, err := listSessions(context.Background(), sessionRunner(getenv)); err == nil {
		if socket, ok := herdrx.DefaultRunningSocket(sessions); ok {
			return herdrx.OpenPath(socket)
		}
	}

	// Nothing in the environment, nothing discoverable. Report it through the
	// same path as any other bad socket rather than calling herdrx.Open(),
	// which reads os.Getenv directly and would therefore consult the REAL
	// process environment — defeating the getenv seam this function exists to
	// honour, and making the outcome depend on whether the binary happens to
	// be running inside a herdr pane.
	return herdrx.OpenPath("")
}

// listSessions and sessionRunner are seams: tests point them at canned output
// so no herdr binary is required, and so a test never shells out at all.
var listSessions = func(ctx context.Context, run herdrx.Runner) ([]herdrx.Session, error) {
	return herdrx.ListSessions(ctx, "", run)
}

// sessionRunner resolves the herdr binary the same way every other call site
// does, honouring HERDR_BIN_PATH.
func sessionRunner(getenv func(string) string) herdrx.Runner {
	bin := getenv("HERDR_BIN_PATH")
	if bin == "" {
		return nil // ListSessions defaults to "herdr" on PATH
	}
	return func(ctx context.Context, _ string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, bin, args...).Output()
	}
}

// aliverFor adapts (client, openErr) into an external.Aliver: the real
// client when dialing succeeded, constAliver(false) otherwise — so
// CheckDeps/CheckListDeps below never need a nil check of their own.
func aliverFor(client *herdrx.Client, openErr error) external.Aliver {
	if openErr != nil {
		return constAliver(false)
	}
	return client
}
