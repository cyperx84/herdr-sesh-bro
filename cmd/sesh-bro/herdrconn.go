// This file is the one place that dials the herdr socket. Every subcommand
// gets its Client (or the reason it doesn't have one) from openHerdr, so the
// "what happens when herdr is unreachable" decision is made once instead of
// once per file.
package main

import (
	"context"

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
// herdr-api's Client.Open dials $HERDR_SOCKET_PATH only, with no fallback
// (herdr-api client.go:118-124) — unlike bash's `herdr` CLI, which has its
// own daemon-discovery mechanism independent of any single env var. Running
// this binary from a plain shell outside a herdr pane (HERDR_SOCKET_PATH
// unset) therefore reports "daemon is not responding" where the bash CLI
// might have found the daemon another way. This is a real, load-bearing
// divergence from herdr-api's own dependency, not an oversight in this
// package — see the final report.
func openHerdr() (*herdrx.Client, error) {
	return herdrx.Open()
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
