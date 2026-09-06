// cmdLast focuses the workspace you were in before this one.
//
// It now means that literally (BEHAVIOUR.md §10.8). Before 0.4.0 it picked the
// highest-numbered OTHER workspace, which coincides with "previous" only when
// you have two of them — docs/FEATURE-DEMAND.md listed an MRU toggle as
// already shipped, and it was not. The [[events]] hook records every
// workspace.focused, so there is now a real history to consult; without one
// recorded yet, the old rule remains the fallback.
package main

import (
	"context"
	"fmt"

	herdr "github.com/cyperx84/herdr-api"

	"github.com/cyperx84/herdr-sesh-bro/internal/attention"
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
	prev, ok := mruWorkspace(attention.Load(statePath(env.getenv)), workspaces, current)
	if !ok {
		// No recorded history — the hook has not seen a focus change yet, or
		// this is a plain `sesh-bro last` outside herdr. Fall back to the
		// pre-0.4.0 rule rather than refusing.
		prev, ok = herdrx.PreviousWorkspace(workspaces, current)
	}
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

// mruWorkspace returns the most recently focused workspace that is not the
// current one and still exists.
//
// Both filters matter. Skipping the current one is what makes the command a
// toggle rather than a no-op, and requiring the workspace to still exist keeps
// a closed workspace from making `last` fail when there is a perfectly good
// older one behind it.
func mruWorkspace(state attention.State, workspaces []herdr.Workspace, current string) (string, bool) {
	if len(state.MRUWorkspaces) == 0 {
		return "", false
	}
	live := make(map[string]bool, len(workspaces))
	for _, w := range workspaces {
		live[w.ID] = true
	}
	for _, id := range state.MRUWorkspaces {
		if id != current && live[id] {
			return id, true
		}
	}
	return "", false
}
