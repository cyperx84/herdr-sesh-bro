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
	"bufio"
	"context"
	"fmt"
	"github.com/cyperx84/herdr-sesh-bro/internal/output"
	"github.com/cyperx84/herdr-sesh-bro/internal/render"
	"os"
	"strings"

	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx"
)

func cmdClose(ctx context.Context, env *appEnv, args []string) int {
	// --from-file is how the picker hands over a multi-selection: fzf's {+f}
	// writes the chosen rows to a temp file. It is a separate path rather than
	// more positional arguments because closing several workspaces has to
	// confirm first, and confirming needs the whole set up front.
	if len(args) >= 1 && args[0] == "--from-file" {
		if len(args) < 2 {
			fmt.Fprintln(env.stderr, "sesh-bro: close: --from-file needs a path")
			return output.ExitUsage
		}
		assumeYes := false
		for _, a := range args[2:] {
			if a == "--yes" {
				assumeYes = true
				continue
			}
			fmt.Fprintf(env.stderr, "sesh-bro: close: unknown flag %s\n", a)
			return output.ExitUsage
		}
		return closeFromFile(ctx, env, args[1], assumeYes)
	}

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

// closeFromFile closes every workspace named in a TSV rows file, after showing
// the user exactly what it resolved.
//
// The confirmation is not politeness, it is the argument for allowing
// multi-select at all. The picker previously refused --multi so that
// "highlighted" and "selected" could never diverge for an irreversible action,
// which was sound: a user who selected three rows and then moved the cursor
// would otherwise have no way to know which set was about to be destroyed.
// Printing the resolved list before touching anything removes the ambiguity
// instead of avoiding it — you cannot be surprised by a set you just read.
//
// Non-workspace rows are reported, never silently dropped. A user who selected
// an agent and a workspace and saw only "closed 1 workspace" would reasonably
// conclude the agent had been closed too.
func closeFromFile(ctx context.Context, env *appEnv, path string, assumeYes bool) int {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(env.stderr, "sesh-bro: close: %v\n", err)
		return output.ExitFailure
	}

	var targets []string
	var skipped []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		kind, target := fields[0], fields[1]
		// The pinned counts row is chrome, not a candidate; fzf's
		// --header-lines keeps it unselectable, so seeing one here would mean
		// something upstream changed, and closing it would be nonsense.
		if kind == render.HeaderRowKind {
			continue
		}
		if kind != string(render.KindWorkspace) {
			skipped = append(skipped, kind+" "+target)
			continue
		}
		targets = append(targets, target)
	}

	for _, s := range skipped {
		fmt.Fprintf(env.stderr, "sesh-bro: close: skipping %s (only workspaces can be closed)\n", s)
	}
	if len(targets) == 0 {
		fmt.Fprintln(env.stderr, "sesh-bro: close: no workspace rows selected")
		return output.ExitEmpty
	}

	if !assumeYes {
		if !confirmClose(env, targets) {
			fmt.Fprintln(env.stderr, "sesh-bro: close: cancelled")
			return output.ExitOK
		}
	}

	client, openErr := openHerdr(env.getenv)
	failed := 0
	for _, target := range targets {
		if err := closeRow(ctx, env, client, openErr, string(render.KindWorkspace), target); err != nil {
			failed++
		}
	}
	if failed > 0 {
		return output.ExitFailure
	}
	return output.ExitOK
}

// confirmClose prints what is about to be destroyed and reads y/N from the
// terminal.
//
// It reads /dev/tty rather than stdin because fzf's `execute` hands the child
// the terminal while stdin may still be the row stream — asking on stdin would
// consume rows and never see the user. When there is no tty at all (a script,
// an agent) it refuses rather than assuming yes: an irreversible action with
// no one to ask is exactly when to stop, and --yes exists to say otherwise.
func confirmClose(env *appEnv, targets []string) bool {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		fmt.Fprintln(env.stderr, "sesh-bro: close: no terminal to confirm on; pass --yes to close without confirming")
		return false
	}
	defer tty.Close()

	fmt.Fprintf(tty, "Close %d workspace(s) and every agent in them?\n", len(targets))
	for _, t := range targets {
		fmt.Fprintf(tty, "  %s\n", t)
	}
	fmt.Fprint(tty, "This cannot be undone. [y/N] ")

	reader := bufio.NewReader(tty)
	answer, _ := reader.ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}
