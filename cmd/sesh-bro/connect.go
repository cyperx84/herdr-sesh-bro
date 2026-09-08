// cmdConnect reproduces cmd_connect (sesh-bro:287-320, BEHAVIOUR.md §2.3):
// focus a workspace or agent, or resolve/create a workspace for a directory.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/cyperx84/herdr-sesh-bro/internal/external"
	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx"
)

func cmdConnect(ctx context.Context, env *appEnv, args []string) int {
	// BEHAVIOUR.md §9 S9: bash reads $1/$2 unchecked and dies under `set -u`
	// with its own diagnostic and exit 1 — NOT a usage message, NOT exit 2.
	// This port cannot reproduce bash's literal "line N: $1: unbound
	// variable" text (there is no source line to blame), but the
	// OBSERVABLE contract — a clean, non-zero-but-not-2 failure — is
	// preserved: exit 1, our own message.
	if len(args) < 2 {
		fmt.Fprintln(env.stderr, "sesh-bro: connect: missing TYPE/TARGET arguments")
		return 1
	}
	client, openErr := openHerdr(env.getenv)
	if err := connect(ctx, env, client, openErr, args[0], args[1]); err != nil {
		return 1
	}
	return 0
}

// connect is cmd_connect's actual dispatch (sesh-bro:288-319), shared
// verbatim with the picker's post-selection Connector (picker.go) so both
// entry points fail identically. An unknown TYPE calls os.Exit(2) directly,
// matching bash's `exit 2` INSIDE cmd_connect (sesh-bro:318) — it exits the
// whole process immediately, not merely this function, which matters for a
// caller mid-picker exactly as much as for a direct `sesh-bro connect`
// invocation (BEHAVIOUR.md §2.3: "exits the process, does not return").
func connect(ctx context.Context, env *appEnv, client *herdrx.Client, openErr error, kind, target string) error {
	// A composed target reaching the local kinds means a foreign row was
	// dispatched as a local one — most easily by someone typing
	// `connect agent builder@work` by hand. Without this it reaches
	// agent.focus on THIS daemon and fails as not-found, exit 1, which reads
	// as "that agent is gone" rather than "that agent is somewhere I cannot
	// reach from here".
	switch kind {
	case "workspace", "agent":
		if err := refuseForeign("connect", target); err != nil {
			fmt.Fprintln(env.stderr, err)
			os.Exit(2)
		}
	}

	switch kind {
	case "workspace":
		if err := focusWorkspace(ctx, client, openErr, target); err != nil {
			fmt.Fprintf(env.stderr, "sesh-bro: failed to focus workspace %s\n", target)
			return err
		}
		return nil
	case "agent":
		if err := focusAgent(ctx, client, openErr, target); err != nil {
			fmt.Fprintf(env.stderr, "sesh-bro: failed to focus agent %s\n", target)
			return err
		}
		return nil
	case "dir":
		return connectDir(ctx, env, client, openErr, target)
	case "worktree":
		return connectWorktree(ctx, env, client, openErr, target)
	case "issue":
		// Enter on an issue means "give me somewhere to work on this", which
		// is exactly what the worktree command already does with a URL: a real
		// git worktree on a branch named for the issue, opened as a workspace
		// labelled with the issue title. Called again for the same issue it
		// focuses the existing workspace, so pressing Enter twice is safe.
		//
		// Reusing the command rather than its internals keeps ONE path that
		// creates worktrees for issues, including its repository verification
		// — the guard that stops a wrong repo guess materialising a branch
		// inside somebody's dotfiles checkout.
		if code := cmdWorktree(ctx, env, []string{target}); code != 0 {
			return fmt.Errorf("sesh-bro: worktree for %s failed", target)
		}
		return nil
	case "session":
		// The whole cross-session escape hatch: a new terminal running
		// `herdr session attach`. See attach.go for why nothing else works.
		if err := attachSession(ctx, env, target); err != nil {
			fmt.Fprintln(env.stderr, err)
			return err
		}
		return nil
	case "ragent":
		// An agent in another session cannot be focused — no herdr call takes
		// a session, so `agent.focus` would land on THIS daemon and either
		// miss or, worse, hit a same-named agent here. The honest action is
		// the one that actually gets the user to it: attach the session it
		// lives in, and let them find it there.
		_, session, ok := herdrx.SplitForeignTarget(target)
		if !ok {
			fmt.Fprintf(env.stderr, "sesh-bro: %s is not a foreign agent target\n", target)
			return fmt.Errorf("sesh-bro: malformed ragent target %q", target)
		}
		if err := attachSession(ctx, env, session); err != nil {
			fmt.Fprintln(env.stderr, err)
			return err
		}
		return nil
	default:
		fmt.Fprintf(env.stderr, "sesh-bro connect: unknown type %s\n", kind)
		os.Exit(2)
		return nil // unreachable
	}
}

// connectDir reproduces sesh-bro:298-317 (BEHAVIOUR.md §2.3 "connect dir"):
// prefer an existing pane at that exact cwd (focused pane wins), else
// require the path to exist, zoxide-add it, and create a focused workspace
// there.
func connectDir(ctx context.Context, env *appEnv, client *herdrx.Client, openErr error, target string) error {
	// sesh-bro:300: `pane_list 2>/dev/null` — suppressed; a daemon failure
	// here just means "no existing pane found", falling through to the
	// create branch below, not a connect failure of its own. The read is a
	// session.snapshot now that the pane cache is gone (BEHAVIOUR.md §10).
	panes := snapshotPanes(ctx, client, openErr)
	if ws, ok := herdrx.WorkspaceForCWD(panes, target); ok {
		if err := focusWorkspace(ctx, client, openErr, ws); err != nil {
			fmt.Fprintf(env.stderr, "sesh-bro: failed to focus workspace %s\n", ws)
			return err
		}
		return nil
	}

	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		fmt.Fprintf(env.stderr, "sesh-bro: not a directory: %s\n", target)
		return fmt.Errorf("sesh-bro: not a directory: %s", target)
	}
	external.ZoxideAdd(ctx, target)
	label := herdrx.Basename(target)
	if err := createWorkspace(ctx, client, openErr, target, label); err != nil {
		fmt.Fprintf(env.stderr, "sesh-bro: failed to create workspace for %s\n", target)
		return err
	}
	return nil
}

// connectWorktree opens an existing git worktree as a workspace.
//
// This is worktree.OPEN, not worktree.create: the worktree already exists on
// disk — that is the entire reason it appeared as a row (WorktreeRows skips
// any worktree that already has a workspace). Calling create here would try to
// make a second worktree for a branch that already has one, which git refuses,
// and the refusal would surface as a confusing failure on a row whose whole
// promise is "open this".
//
// If it fails, fall back to opening the path as a plain workspace. A worktree
// herdr will not open is still a directory the user can work in, and stranding
// them because the specialised call failed would be worse than a workspace
// that is merely not worktree-aware. Same shape as `worktree`'s own fallback.
func connectWorktree(ctx context.Context, env *appEnv, client *herdrx.Client, openErr error, target string) error {
	if openErr != nil {
		fmt.Fprintf(env.stderr, "sesh-bro: failed to open worktree %s\n", target)
		return openErr
	}
	if err := client.OpenWorktree(ctx, target, true); err == nil {
		return nil
	} else {
		fmt.Fprintf(env.stderr, "sesh-bro: worktree.open failed for %s (%v) — opening it as a plain workspace\n", target, err)
	}
	return connectDir(ctx, env, client, openErr, target)
}
