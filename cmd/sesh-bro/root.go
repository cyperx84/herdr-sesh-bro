// cmdRoot reproduces cmd_root (sesh-bro:373-385, BEHAVIOUR.md §2.6): focus,
// or create, the workspace whose cwd is the current directory's git
// toplevel. Any arguments are ignored — bash's cmd_root never reads $1.
package main

import (
	"context"
	"fmt"

	"github.com/cyperx84/herdr-sesh-bro/internal/external"
	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx"
)

// failedPaneListMsg is the pane-read failure message (sesh-bro:114,
// BEHAVIOUR.md §7.3, Appendix B). It is invariant across every call site —
// what differs is whether a given caller prints it at all (list and
// connect-dir suppress it, root does not, BEHAVIOUR.md §2.6). The read it
// names is a session.snapshot since 0.4.0 (§10), but the message is part of
// the observable contract and stays as it was.
const failedPaneListMsg = "sesh-bro: herdr pane list failed"

func cmdRoot(ctx context.Context, env *appEnv) int {
	pwd := resolvePWD(env)
	root, ok := external.GitRoot(ctx, "", pwd)
	if !ok {
		fmt.Fprintln(env.stderr, "sesh-bro: not in a git repo")
		return 1
	}

	client, openErr := openHerdr(env.getenv)
	// BEHAVIOUR.md §2.6: UNLIKE `connect dir`, this pane read is NOT
	// `2>/dev/null`-guarded in bash — a daemon failure message leaks to
	// stderr here, and root still proceeds to the create branch on an empty
	// result. This is the one pane-read call site that prints
	// failedPaneListMsg itself. The read is a session.snapshot now that the
	// pane cache is gone (BEHAVIOUR.md §10).
	snap, err := loadSnapshot(ctx, client, openErr)
	if err != nil {
		fmt.Fprintln(env.stderr, failedPaneListMsg)
	}
	panes := snap.Panes

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
