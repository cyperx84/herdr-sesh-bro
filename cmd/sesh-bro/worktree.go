// cmdWorktree reproduces cmd_worktree (sesh-bro:391-437, BEHAVIOUR.md §2.7):
// create or focus the Herdr workspace for a GitHub issue/PR — despite the
// name, no git worktree is ever created (this is an issue-labelled Herdr
// workspace; the README says so).
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cyperx84/herdr-sesh-bro/internal/external"
	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx"
)

// resolveWorktreeURL reproduces sesh-bro:392's precedence:
// `${HERDR_PLUGIN_CLICKED_URL:-${1:-}}` — the ENV VAR wins over the
// positional argument, not the other way around. Split out as its own pure
// function so this precedence is testable without driving cmdWorktree all
// the way to a gh/herdr call.
func resolveWorktreeURL(env *appEnv, args []string) string {
	if url := env.getenv("HERDR_PLUGIN_CLICKED_URL"); url != "" {
		return url
	}
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

func cmdWorktree(ctx context.Context, env *appEnv, args []string) int {
	url := resolveWorktreeURL(env, args)
	if url == "" {
		fmt.Fprintln(env.stderr, "sesh-bro worktree: no URL (usage: sesh-bro worktree <github-url>)")
		return 1
	}

	ref, err := external.ParseWorktreeRef(url)
	if err != nil {
		fmt.Fprintln(env.stderr, err)
		return 1
	}

	client, openErr := openHerdr()

	// 24h gh-title cache (BEHAVIOUR.md §2.7). Cache dir: $HERDR_PLUGIN_STATE_DIR,
	// else $TMPDIR-or-/tmp/sesh-bro-<uid> (sesh-bro:405).
	cacheDir := env.getenv("HERDR_PLUGIN_STATE_DIR")
	if cacheDir == "" {
		tmp := env.getenv("TMPDIR")
		if tmp == "" {
			tmp = "/tmp"
		}
		cacheDir = filepath.Join(tmp, fmt.Sprintf("sesh-bro-%d", os.Getuid()))
	}
	// DIVERGENCE, DELIBERATE: sesh-bro:406's `mkdir -p "$cache_dir"` has NO
	// `||` guard, so under `set -euo pipefail` a REAL mkdir failure (e.g.
	// permission denied on $TMPDIR) would abort the whole script right
	// there, before gh or herdr are ever touched — this is a genuine bash
	// failure mode, not a non-issue. It is also an extremely pathological
	// one (the cache dir is under $TMPDIR/$HERDR_PLUGIN_STATE_DIR, which
	// must already be writable for the process to be running at all in
	// practice). This port treats it as soft instead — same "no title,
	// keep going" degradation gh's own absence already gets — trading a
	// hard-to-reach parity edge for not aborting `worktree` over a cache
	// directory the rest of the command never needs again. Flagged, not
	// hidden.
	title, _ := external.ResolveIssueTitle(ctx, cacheDir, ref, time.Now(), "")

	// Existing-workspace check (BEHAVIOUR.md §2.7, §9 S15: repo-agnostic —
	// matches ANY workspace whose label contains the number as a
	// standalone integer, not scoped to owner/repo).
	workspaces, err := listWorkspaces(ctx, client, openErr)
	if err != nil {
		fmt.Fprintln(env.stderr, err)
		return 1
	}
	if existing, ok := herdrx.WorkspaceForIssueNumber(workspaces, ref.Num); ok {
		if err := focusWorkspace(ctx, client, openErr, existing); err != nil {
			fmt.Fprintln(env.stderr, err)
			return 1
		}
		fmt.Fprintf(env.stdout, "sesh-bro: focused workspace %s\n", existing)
		return 0
	}

	label := external.WorktreeLabel(ref.Num, title)
	cwd := external.WorktreeCWD(env.getenv("HOME"), ref.Repo)
	// sesh-bro:434: `... --focus >/dev/null || { echo "..." ; exit 1; }` —
	// only STDOUT is discarded; herdr's own stderr passes through IN
	// ADDITION to sesh-bro's message, same asymmetry as create.go.
	if err := createWorkspace(ctx, client, openErr, cwd, label); err != nil {
		fmt.Fprintf(env.stderr, "sesh-bro: failed to create worktree workspace for %s/%s#%s\n", ref.Owner, ref.Repo, ref.Num)
		fmt.Fprintln(env.stderr, err)
		return 1
	}
	fmt.Fprintf(env.stdout, "sesh-bro: created workspace for %s/%s#%s\n", ref.Owner, ref.Repo, ref.Num)
	return 0
}
