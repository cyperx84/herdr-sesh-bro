// cmdRoot reproduces cmd_root (sesh-bro:373-385, BEHAVIOUR.md §2.6): focus,
// or create, the workspace whose cwd is the current directory's git
// toplevel. Any arguments are ignored — bash's cmd_root never reads $1.
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/cyperx84/herdr-sesh-bro/internal/config"
	"github.com/cyperx84/herdr-sesh-bro/internal/external"
	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx"
)

func cmdRoot(ctx context.Context, env *appEnv) int {
	pwd := resolvePWD(env)
	root, ok := external.GitRoot(ctx, "", pwd)
	if !ok {
		fmt.Fprintln(env.stderr, "sesh-bro: not in a git repo")
		return 1
	}

	client, openErr := openHerdr(env.getenv)
	cfg := config.Load(env.getenv)
	// BEHAVIOUR.md §2.6: UNLIKE `connect dir`, this pane_list call is NOT
	// `2>/dev/null`-guarded in bash — a cache/daemon failure message leaks
	// to stderr here, and root still proceeds to the create branch on an
	// empty result. This is the one paneList call site that prints
	// failedPaneListMsg itself.
	panes, err := paneList(ctx, client, openErr, paneCachePath(env.getenv), cfg.CacheTTL, cfg.CacheTTLValid(), time.Now())
	if err != nil {
		fmt.Fprintln(env.stderr, failedPaneListMsg)
	}

	// BEHAVIOUR.md §2.6: unlike `connect dir`'s WorkspaceForCWD, there is NO
	// focused-pane preference here — the first pane (in list order) whose
	// cwd matches wins.
	if ws, ok := herdrx.WorkspaceForCWDAny(panes, root); ok {
		// bash has no `|| { echo ...; }` wrapper around root's own
		// focus/create calls (unlike connect/create) — only stdout is
		// discarded; herdr's own stderr passes straight through, and the
		// exit code IS the herdr call's. There is no sesh-bro-authored
		// message for this specific failure to reproduce (BEHAVIOUR.md
		// §2.6: "Exit code is the focus/create call's").
		if err := focusWorkspace(ctx, client, openErr, ws); err != nil {
			fmt.Fprintln(env.stderr, err)
			return 1
		}
		return 0
	}
	label := herdrx.Basename(root)
	if err := createWorkspace(ctx, client, openErr, root, label); err != nil {
		fmt.Fprintln(env.stderr, err)
		return 1
	}
	return 0
}
