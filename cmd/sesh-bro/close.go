// cmdClose is sesh-bro's answer to docs/COMPETITIVE-DEMAND.md #1 ("Close /
// remove a workspace from the picker") — bash has no equivalent command to
// cite; every comment in this file explains a DECISION, not a source line,
// because there is no bash line to reproduce.
//
// Wired into the picker as the alt-x (default; SESH_BRO_KEY_CLOSE
// overrides it — see internal/picker.KeyBindings and config.Config.Keys)
// execute-silent+reload bind, the exact same shape internal/picker already
// uses for ctrl-/ create: fzf shells out to `<self> close {1} {2}`,
// discards its output, then reloads the list. Also runnable directly
// (`sesh-bro close TYPE TARGET`), for the same reason `connect` and
// `create` are.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx"
)

func cmdClose(ctx context.Context, env *appEnv, args []string) int {
	// Mirrors cmd_connect's TYPE/TARGET contract (connect.go, BEHAVIOUR.md
	// §9 S9), not because bash's `close` ever existed to be unbound under
	// `set -u`, but because close is the same shape of command — fzf drives
	// it positionally off fields {1} {2} exactly like connect — and a
	// caller invoking either by hand with too few arguments should see the
	// same class of failure: our own message, exit 1, not a usage error.
	if len(args) < 2 {
		fmt.Fprintln(env.stderr, "sesh-bro: close: missing TYPE/TARGET arguments")
		return 1
	}
	client, openErr := openHerdr(env.getenv)
	if err := closeRow(ctx, env, client, openErr, args[0], args[1]); err != nil {
		return 1
	}
	return 0
}

// closeRow is close's actual dispatch, shared between cmdClose and the
// picker's alt-x bind (both ultimately invoke the compiled binary as
// `close TYPE TARGET` — there is exactly one entry point, see cmdClose's
// doc comment).
//
// Only "workspace" is ever closable: docs/COMPETITIVE-DEMAND.md #1's own
// scoping ("Only workspace rows are closable") and the task this file
// implements agree. "agent" and "dir" are a deliberate NO-OP with a
// message, never an error — the picker must not treat "the highlighted row
// happens to be the wrong kind" as a failure any differently than a
// genuinely inert keypress would be. An unknown TYPE is unreachable through
// the picker (only the three generated row kinds ever reach here via {1});
// it is reachable only via a hand-typed `sesh-bro close <garbage> <id>`, and
// follows cmd_connect's own unknown-type contract (connect.go: os.Exit(2))
// since that IS this binary's established "you typed something we don't
// understand" signal.
func closeRow(ctx context.Context, env *appEnv, client *herdrx.Client, openErr error, kind, target string) error {
	switch kind {
	case "workspace":
		return closeWorkspaceRow(ctx, env, client, openErr, target)
	case "agent", "dir":
		fmt.Fprintf(env.stderr, "sesh-bro: cannot close a %s row; only workspaces can be closed\n", kind)
		return nil
	default:
		fmt.Fprintf(env.stderr, "sesh-bro close: unknown type %s\n", kind)
		os.Exit(2)
		return nil // unreachable
	}
}

// closeWorkspaceRow does the one thing docs/COMPETITIVE-DEMAND.md #1 asks
// for, plus the guard the task calls out by name: never close the workspace
// the picker itself is running in. current is resolved exactly the way
// every other "what workspace am I in" call site in this port resolves it
// (herdrx.CurrentWorkspaceID — see last.go, list.go) so this guard uses the
// SAME id the picker's own "current" marker and current-first sort priority
// are built from, not a second, possibly-disagreeing notion of "current".
//
// Closing your own workspace out from under the picker would kill the pane
// running fzf mid-selection — strictly worse than any other failure mode
// this command has — so it is refused as a no-op with a message, exactly
// like the agent/dir case in closeRow, never a hard error that could
// surface as a picker crash.
func closeWorkspaceRow(ctx context.Context, env *appEnv, client *herdrx.Client, openErr error, target string) error {
	current := herdrx.CurrentWorkspaceID(env.getenv("HERDR_WORKSPACE_ID"), env.getenv("HERDR_PLUGIN_CONTEXT_JSON"))
	if current != "" && target == current {
		fmt.Fprintf(env.stderr, "sesh-bro: refusing to close the current workspace %s\n", target)
		return nil
	}
	if openErr != nil {
		fmt.Fprintf(env.stderr, "sesh-bro: failed to close workspace %s\n", target)
		return openErr
	}

	// Second guard, and the one that actually holds. The env check above fails
	// OPEN: with neither HERDR_WORKSPACE_ID nor HERDR_PLUGIN_CONTEXT_JSON set,
	// `current` is empty and the most destructive action in this program is
	// permitted unguarded. That is not hypothetical — the picker's primary
	// launch path is a popup pane, and BEHAVIOUR.md records
	// HERDR_PLUGIN_CONTEXT_JSON as never observed in a live pane, so the
	// env-missing case is the expected one rather than the exotic one.
	//
	// workspace.list reports which workspace the daemon considers focused.
	// That needs no environment, cannot be dropped by a nested shell, and is
	// the same call the rows already use to render "· current".
	//
	// It over-blocks one case: closing the focused workspace while running
	// from an external terminal, where doing so would be harmless. For an
	// irreversible action reached by one keystroke, refusing too often is the
	// correct direction to be wrong in.
	if focused, err := focusedWorkspaceID(ctx, client); err == nil && focused != "" && focused == target {
		fmt.Fprintf(env.stderr, "sesh-bro: refusing to close the focused workspace %s\n", target)
		return nil
	}

	if err := client.CloseWorkspace(ctx, target); err != nil {
		fmt.Fprintf(env.stderr, "sesh-bro: failed to close workspace %s\n", target)
		return err
	}
	return nil
}

// focusedWorkspaceID asks the daemon which workspace is focused.
//
// Separate from CurrentWorkspaceID because the two answer different questions
// with different failure modes: the env vars say "which workspace launched this
// process" and can simply be absent, while this says "which workspace is
// focused right now" and is authoritative whenever the daemon answers at all.
func focusedWorkspaceID(ctx context.Context, client *herdrx.Client) (string, error) {
	wss, err := client.ListWorkspaces(ctx)
	if err != nil {
		return "", err
	}
	for _, w := range wss {
		if w.Focused {
			return w.ID, nil
		}
	}
	return "", nil
}
