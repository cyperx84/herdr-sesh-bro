// cmdLast reproduces cmd_last (sesh-bro:359-368, BEHAVIOUR.md §2.5): focus
// the previously-focused workspace. Any arguments are ignored — bash's
// cmd_last never reads $1.
package main

import (
	"context"
	"fmt"

	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx"
)

func cmdLast(ctx context.Context, env *appEnv) int {
	client, openErr := openHerdr(env.getenv)
	current := herdrx.CurrentWorkspaceID(env.getenv("HERDR_WORKSPACE_ID"), env.getenv("HERDR_PLUGIN_CONTEXT_JSON"))
	// BEHAVIOUR.md §9 S2: when current == "" (no $HERDR_WORKSPACE_ID —
	// running outside a herdr pane), nothing is excluded and the already-
	// focused workspace sorts to position 0 via PreviousWorkspace's own
	// focused-first tiebreak, so `last` becomes a no-op that LOOKS like
	// success. Reproduced, not special-cased.
	// sesh-bro:666: `prev="$("$HERDR" workspace list | jq -r ...)"` is a
	// plain assignment under `set -euo pipefail` — a failing `workspace
	// list` here aborts immediately with herdr's own stderr, exactly like
	// `list`'s mid-run failure (BEHAVIOUR.md §2.2.11), NOT the distinct
	// "no previous workspace" message below (which is bash's explicit
	// `[[ -n $prev ]] ||` check on an EMPTY result, a different condition).
	workspaces, err := listWorkspaces(ctx, client, openErr)
	if err != nil {
		fmt.Fprintln(env.stderr, err)
		return 1
	}
	prev, ok := herdrx.PreviousWorkspace(workspaces, current)
	if !ok {
		fmt.Fprintln(env.stderr, "sesh-bro: no previous workspace")
		return 1
	}
	// sesh-bro:667: `"$HERDR" workspace focus "$prev" >/dev/null` — only
	// STDOUT is discarded; herdr's own stderr passes straight through
	// (there is no `|| { echo ...; }` wrapper here, unlike `connect`).
	if err := focusWorkspace(ctx, client, openErr, prev); err != nil {
		fmt.Fprintln(env.stderr, err)
		return 1
	}
	return 0
}
