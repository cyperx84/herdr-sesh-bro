// cmdWorktree creates or focuses the Herdr workspace for a GitHub issue/PR
// (sesh-bro:391-437, BEHAVIOUR.md §2.7) — updated to create a REAL git
// worktree.
//
// FEATURE CHANGE, NOT PARITY: BEHAVIOUR.md §2.7 documents the original
// bash's behaviour accurately — "despite the name, no git worktree is ever
// created". That was true of the bash and is no longer true of this port.
// Two ctrl-clicks on different issues in the SAME repo used to yield two
// Herdr workspaces pointing at ONE working tree — exactly the
// two-agents-one-tree hazard herdr-api's own WorktreeCreateParams doc
// comment calls out ("amend becomes a new commit, uncommitted edits look
// like phantom modifications, reflog fills with commits neither agent
// believes it made... not a hypothetical"). This project treats that
// hazard as a defect everywhere else; its own flagship feature was the
// worst offender. Fixing that is worth diverging from BEHAVIOUR.md's
// oracle role for — see docs/COMPETITIVE-DEMAND.md's item 3, which flags
// exactly this gap.
//
// New behaviour, replacing §2.7's "otherwise create" branch only (the
// no-URL, unrecognized-URL, and existing-workspace-focus paths are
// unchanged and still match BEHAVIOUR.md exactly):
//
//   - No existing workspace matches the issue number (still the original,
//     repo-agnostic check — herdrx.WorkspaceForIssueNumber, §9 S15):
//     attempt worktree.create on a branch derived from the issue number
//     (worktreeBranchName), anchored with CWD set EXPLICITLY to the
//     repo's checkout path — see createGitWorktree's comment for why an
//     implicit/ambient CWD is the wrong default here specifically.
//   - It succeeds: the new worktree's workspace is already focused
//     (Focus: true, same convention as every other workspace-creating
//     call in this codebase — see herdrx.Client.CreateWorktree). Print
//     the new "created worktree for ..." message and return 0.
//   - It fails — repo isn't a git repo, herdr rejects the call, anything
//     else — fall back to §2.7's ORIGINAL plain-workspace behaviour
//     verbatim (same cwd, same label, same success/failure messages),
//     after printing one extra line explaining the fallback. Someone
//     ctrl-clicking a link wants to get somewhere, not read a stack
//     trace.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	herdr "github.com/cyperx84/herdr-api"

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

// worktreeBranchName derives the branch a real git worktree is created on
// from the issue/PR number: "issue-<num>" uniformly, whatever form the ref
// was parsed from.
//
// SHAPE CHOSEN: "issue-<num>", not "pr-<num>" for a /pull/ URL, for two
// reasons:
//
//  1. GitHub issues and PRs share ONE numbering sequence per repo, and
//     "owner/repo#N" — BEHAVIOUR.md §2.7's second accepted ref form, and
//     the one a hand-typed `sesh-bro worktree` invocation is likeliest to
//     use — carries no issue/PR distinction at all. external.WorktreeRef
//     doesn't capture "issues" vs "pull" from the URL form either (it
//     only keeps Owner/Repo/Num). Branching the shape on kind would need
//     a new field threaded through parsing that's only ever populated for
//     one of the two accepted ref forms — two shapes for a distinction
//     the tool doesn't otherwise track anywhere, for no behavioural gain.
//  2. The existing-workspace check this function's caller runs first
//     (herdrx.WorkspaceForIssueNumber) is already number-only and
//     repo/kind-agnostic by design (§9 S15). "issue-<num>" keeps the
//     branch-naming convention consistent with that: the number is the
//     identity here, not whatever GitHub currently classifies the number
//     as — which can itself change (an issue later gets a linked PR that
//     closes it; the number sesh-bro was pointed at doesn't).
//
// Kept stable once chosen: a branch name that changes shape across
// versions orphans every worktree anyone already has checked out under
// the old one.
func worktreeBranchName(num string) string {
	return "issue-" + num
}

// createGitWorktree wraps herdrx.Client.CreateWorktree with the same
// openErr-passthrough convention herdrcalls.go's shared wrappers use for
// every other herdr call in this command layer (openHerdr never itself
// prints or exits; each caller decides how "no client" surfaces). Kept
// local to this file rather than folded into herdrcalls.go's shared set —
// this task's touch surface is worktree.go, internal/herdrx, and
// README.md's worktree section only.
func createGitWorktree(ctx context.Context, client *herdrx.Client, openErr error, cwd, branch, label string) (herdr.WorktreeCreated, error) {
	if openErr != nil {
		return herdr.WorktreeCreated{}, openErr
	}
	return client.CreateWorktree(ctx, cwd, branch, label)
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
	// standalone integer, not scoped to owner/repo). Unchanged: a second
	// ctrl-click on the same issue still focuses the workspace already
	// created for it — it never creates a second worktree.
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
	// cwd anchors BOTH the worktree.create attempt below AND the
	// plain-workspace fallback to the repo the issue actually belongs to
	// — external.WorktreeCWD("$HOME/github/<repo>", falling back to
	// $HOME) is the same convention §2.7's original plain-workspace path
	// already used, reused here rather than duplicated. Passing it as
	// CWD explicitly (not WorkspaceID, not leaving both unset) is what
	// stops worktree.create from resolving against whichever workspace
	// happens to be focused right now — see CreateWorktree's own comment
	// in internal/herdrx/herdrx.go for why that specific failure mode is
	// not hypothetical.
	cwd := external.WorktreeCWD(env.getenv("HOME"), ref.Repo)
	branch := worktreeBranchName(ref.Num)

	// Verify the guess before letting it mutate a repository.
	//
	// WorktreeCWD guesses "$HOME/github/<repo>" and falls back to "$HOME".
	// That was harmless when this command only opened a plain workspace at
	// that path. Creating a real git worktree makes a wrong guess destructive:
	// on a machine with no ~/github whose owner has run `git init` in their
	// home directory — dotfiles-in-$HOME, which is common — the fallback
	// resolves to $HOME and clicking an issue link would branch and
	// materialise a worktree inside their dotfiles repo.
	//
	// RepoCheckout requires the path to be a git root that actually belongs to
	// the repo in the link. When it cannot confirm that, the worktree is
	// skipped and the plain workspace below still runs, so the user still
	// lands somewhere — they simply do not get a worktree in a repo they never
	// named.
	root, isRepo := external.RepoCheckout(ctx, "", cwd, ref.Owner, ref.Repo)
	if !isRepo {
		fmt.Fprintf(env.stderr,
			"sesh-bro: %s is not a checkout of %s/%s — opening a plain workspace instead of creating a worktree there\n",
			cwd, ref.Owner, ref.Repo)
	}

	wtErr := errNoRepoCheckout
	if isRepo {
		_, wtErr = createGitWorktree(ctx, client, openErr, root, branch, label)
	}
	if wtErr == nil {
		fmt.Fprintf(env.stdout, "sesh-bro: created worktree for %s/%s#%s on %s\n", ref.Owner, ref.Repo, ref.Num, branch)
		return 0
	}
	// Not a git repo, herdr rejected the call, cwd doesn't exist
	// (WorktreeCWD's own known gap — it only checks that "$HOME/github"
	// exists, not "$HOME/github/<repo>" itself) — whatever the reason,
	// land the user somewhere instead of failing the command outright.
	// The friendly line stays short; the underlying error goes on its
	// own line for whoever wants it, the same split create.go and
	// cmdWorktree's own failure path below already use.
	fmt.Fprintf(env.stderr, "sesh-bro: could not create a git worktree for %s/%s#%s at %s — falling back to a plain workspace\n", ref.Owner, ref.Repo, ref.Num, cwd)
	fmt.Fprintln(env.stderr, wtErr)

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

// errNoRepoCheckout marks the "could not confirm a checkout" path so it takes
// the same plain-workspace fallback as a failed worktree.create, without
// pretending an error came back from herdr.
var errNoRepoCheckout = errors.New("sesh-bro: no confirmed checkout for this repo")
