package herdrx

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	herdr "github.com/cyperx84/herdr-api"
)

// RowWorktree is the picker's fourth row type: a git worktree that exists on
// disk but has no open workspace. It is declared here rather than beside
// RowWorkspace/RowAgent/RowDir because those three are aliases of render.Kind
// constants, and internal/render has no worktree kind — its FormatRow
// validates against exactly the three kinds the bash ever produced
// (BEHAVIOUR.md §4.1) and rejects anything else. Adding KindWorktree to
// render is the rendering package's decision to make (it needs an icon and a
// colour mapping, not just a string); until then this constant exists so the
// row-assembly layer can be built and tested independently. RowType is a
// type alias for render.Kind, so this slots into Row.Type with no cast.
const RowWorktree RowType = "worktree"

// ListWorktrees is `worktree.list` scoped by cwd — the repo at that path is
// the one whose worktrees are returned. It is verified working per-repo
// against a live daemon: passing a repo path returns that repo's worktrees.
//
// cwd is not optional for the same reason CreateWorktree's is not: leaving
// both WorktreeListParams filters unset makes the server resolve the repo
// from whichever workspace it currently has focused, which silently lists an
// unrelated repo's worktrees whenever this process isn't running inside the
// target repo's workspace. The picker invokes it with the workspace the
// picker was opened from, which is exactly the repo the human means.
//
// Beyond the §6 parity set: sesh-bro's bash never asked for worktrees, so
// there is no mapping-table row to cite. The demand is the picker's own gap
// — herdr knows about every worktree on disk, and the picker until now could
// only show the ones already open (docs/COMPETITIVE-DEMAND.md's review of
// competitor plugins converges on worktree listing as table-stakes).
func (c *Client) ListWorktrees(ctx context.Context, cwd string) ([]herdr.Worktree, error) {
	res, err := c.c.WorktreeList(ctx, herdr.WorktreeListParams{CWD: &cwd})
	if err != nil {
		return nil, fmt.Errorf("herdrx: list worktrees for %s: %w", cwd, err)
	}
	return res.Worktrees, nil
}

// prunableDetail is the marker a prunable worktree's Detail carries. herdr
// marks a worktree prunable when git reports it as such — most commonly its
// directory no longer exists on disk. That is a worktree worth SHOWING
// differently (the human should see it is dead before trying to open it),
// not hiding: a prunable worktree is still in git's metadata and git
// worktree prune is the fix, which the human can only decide to run if the
// picker tells them it's there.
const prunableDetail = " · prunable"

// WorktreeRows converts worktree.list's result into picker rows.
//
// THE DEDUPE RULE IS THE POINT. A worktree whose OpenWorkspaceID is non-nil
// is already open as a workspace, and the workspace source lists every open
// workspace — emitting it here too would list it twice, and the duplicate
// would even be differently-labelled (branch vs workspace label), which no
// dedupe on the picker side can catch. What makes this source worth having
// is precisely the complement: the worktrees on disk that have NOT been
// opened. Skip anything with an OpenWorkspaceID, no exceptions.
//
// Also skipped: bare worktrees (a bare checkout has no working tree to open
// a workspace into — nothing the picker could do with it), and paths caught
// by the same blacklist globs DirRows honours (§9 S6) — one matcher, reused,
// so the two sources cannot drift apart on what "hidden" means.
//
// Field by field, mirroring DirRows (§2.2.6) wherever a worktree is just a
// directory with extra metadata:
//
//   - target: the worktree's Path. That is what opens it — `worktree.open`
//     takes a path, and the picker's connect action for a worktree row is
//     "open this directory as a workspace", for which the path is the handle.
//   - status: dirStatus, the §2.2.6 placeholder. A worktree hosts no agent,
//     so it has no agent_status to normalize; it gets the same "-" a zoxide
//     directory does.
//   - label: the branch when there is one, else the worktree's Label, else
//     the path's Basename. The fallbacks exist for the detached case: a
//     detached worktree has no branch (herdr sends null, and a null Branch
//     is also the bare-checkout shape), but it must still be identifiable —
//     an empty label renders as a blank the picker can neither display nor
//     disambiguate.
//   - detail: the path, plus prunableDetail when herdr reports the worktree
//     prunable. A worktree whose directory is gone is worth showing
//     differently, not hiding — see prunableDetail.
func WorktreeRows(worktrees []herdr.Worktree, blacklist string) []Row {
	rows := make([]Row, 0, len(worktrees))
	for _, wt := range worktrees {
		// The dedupe rule. OpenWorkspaceID non-nil means a workspace row for
		// this same worktree is already in the picker; keeping it here would
		// be the duplicate noise this whole filter exists to prevent.
		if wt.OpenWorkspaceID != nil {
			continue
		}
		// Bare: no working tree to open. Not "currently useless", but
		// structurally unopenable from the picker.
		if wt.IsBare {
			continue
		}
		if blacklistMatches(blacklist, wt.Path) {
			continue
		}

		label := ""
		if wt.Branch != nil {
			label = *wt.Branch
		}
		if label == "" {
			label = wt.Label
		}
		if label == "" {
			label = Basename(wt.Path)
		}

		detail := wt.Path
		if wt.IsPrunable {
			detail += prunableDetail
		}

		rows = append(rows, Row{
			Type:   RowWorktree,
			Target: wt.Path,
			Status: dirStatus,
			Label:  label,
			Detail: detail,
		})
	}
	return rows
}

// OpenWorktree is `worktree.open` — attach a workspace to a worktree that
// already exists on disk.
//
// Distinct from CreateWorktree, which makes a new one. The picker's worktree
// rows are by construction worktrees that exist and are NOT open, so opening
// is the only sensible action on them; creating would ask git for a second
// worktree on a branch that already has one, and be refused.
func (c *Client) OpenWorktree(ctx context.Context, path string, focus bool) error {
	// CWD is not optional despite being nullable, and it is not the worktree
	// either. Both were learned the hard way against a live daemon:
	//
	//   path only            -> not_git_worktree: "Herdr worktree actions
	//                           require a workspace inside a Git work tree"
	//                           (with neither CWD nor WorkspaceID, herdr
	//                           resolves against whatever workspace happens to
	//                           be focused, which may be anywhere)
	//   cwd = the worktree   -> linked_worktree_source: "New and open worktree
	//                           actions start from the repo parent workspace."
	//
	// So worktree actions are anchored at the MAIN checkout, with Path naming
	// which linked worktree to act on. parent resolves that main checkout from
	// the worktree itself; when it cannot, the call is still attempted with no
	// CWD, because herdr's own error is more useful than a guess.
	params := herdr.WorktreeOpenParams{Path: &path, Focus: focus}
	if root, ok := parentCheckout(ctx, path); ok {
		params.CWD = &root
	}
	_, err := c.c.WorktreeOpen(ctx, params)
	if err != nil {
		return fmt.Errorf("herdrx: open worktree %s: %w", path, err)
	}
	return nil
}

// parentCheckout returns the main checkout a (possibly linked) worktree
// belongs to.
//
// `git worktree list --porcelain` always names the main worktree first — that
// is its documented ordering, and it is the one herdr calls the "repo parent
// workspace". Asked inside the main checkout it simply returns that same path,
// so this is correct for both cases without needing to know which it is
// looking at.
func parentCheckout(ctx context.Context, worktree string) (string, bool) {
	out, err := exec.CommandContext(ctx, "git", "-C", worktree, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return "", false
	}
	first, _, _ := strings.Cut(string(out), "\n")
	root, ok := strings.CutPrefix(strings.TrimSpace(first), "worktree ")
	if !ok || root == "" {
		return "", false
	}
	return root, true
}
