// cmdConnect reproduces cmd_connect (sesh-bro:287-320, BEHAVIOUR.md §2.3):
// focus a workspace or agent, or resolve/create a workspace for a directory.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cyperx84/herdr-sesh-bro/internal/config"
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
	cfg := config.Load(env.getenv)
	cachePath := paneCachePath(env.getenv)
	// sesh-bro:300: `pane_list 2>/dev/null` — suppressed; a cache/daemon
	// failure here just means "no existing pane found", falling through to
	// the create branch below, not a connect failure of its own.
	panes, _ := paneList(ctx, client, openErr, cachePath, cfg.CacheTTL, cfg.CacheTTLValid(), time.Now())
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
