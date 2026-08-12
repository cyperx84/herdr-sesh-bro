package herdrx

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	herdr "github.com/cyperx84/herdr-api"
)

// FocusedOrFirstPane returns the focused pane's id in panes, else the first
// pane's id, else "" and false when panes is empty. Matches BEHAVIOUR.md
// §2.8's preview-workspace pane pick:
// `(map(select(.focused)) | .[0].pane_id) // .[0].pane_id // empty`.
func FocusedOrFirstPane(panes []herdr.Pane) (string, bool) {
	if len(panes) == 0 {
		return "", false
	}
	for _, p := range panes {
		if p.Focused {
			return p.ID, true
		}
	}
	return panes[0].ID, true
}

// WorkspaceForCWD returns the workspace id of the pane whose cwd equals
// path exactly, preferring a focused pane over the first match in panes
// order. Matches BEHAVIOUR.md §2.3's `connect dir`:
// `(map(select(.cwd == $p and .focused)) | .[0].workspace_id) //
// (map(select(.cwd == $p)) | .[0].workspace_id) // empty`.
func WorkspaceForCWD(panes []herdr.Pane, path string) (string, bool) {
	var firstMatch string
	haveFirst := false
	for _, p := range panes {
		if p.CWD != path {
			continue
		}
		if p.Focused {
			return p.WorkspaceID, true
		}
		if !haveFirst {
			firstMatch, haveFirst = p.WorkspaceID, true
		}
	}
	return firstMatch, haveFirst
}

// WorkspaceForCWDAny returns the workspace id of the FIRST pane (in panes
// order) whose cwd equals path exactly — no focused-pane preference.
// Matches BEHAVIOUR.md §2.6's `root`:
// `(map(select(.cwd == $p)) | .[0].workspace_id) // empty`.
//
// This is deliberately a separate function from WorkspaceForCWD, not a
// shared helper with a bool flag: `root` and `connect dir` genuinely differ
// here (§2.6: "Unlike connect dir, there is no focused-pane preference"),
// and merging them risks one caller silently inheriting the other's
// behaviour on the next edit.
func WorkspaceForCWDAny(panes []herdr.Pane, path string) (string, bool) {
	for _, p := range panes {
		if p.CWD == path {
			return p.WorkspaceID, true
		}
	}
	return "", false
}

// PreviousWorkspace picks the workspace `last` focuses: every workspace
// except current, sorted focused-first then by DESCENDING number, first
// result. Matches BEHAVIOUR.md §2.5:
// `sort_by((.focused | not), (-(.number // 0))) | .[0].workspace_id`.
//
// §9 S2: when current == "", nothing is excluded and the already-focused
// workspace sorts to position 0 — `last` becomes a no-op that looks like
// success. That is reproduced here, not fixed: this function does exactly
// what the jq pipeline does when current == "" is passed in.
func PreviousWorkspace(workspaces []herdr.Workspace, current string) (string, bool) {
	kept := make([]herdr.Workspace, 0, len(workspaces))
	for _, w := range workspaces {
		if w.ID == current {
			continue
		}
		kept = append(kept, w)
	}
	sort.SliceStable(kept, func(i, j int) bool {
		if kept[i].Focused != kept[j].Focused {
			return kept[i].Focused // focused sorts before not-focused
		}
		return kept[i].Number > kept[j].Number // descending
	})
	if len(kept) == 0 {
		return "", false
	}
	return kept[0].ID, true
}

var qualifiedIssueLabelRe = regexp.MustCompile(`^([^/[:space:]]+)/([^#[:space:]]+)#([0-9]+)`)

// WorkspaceForIssue returns the workspace for one repo-qualified GitHub
// issue/PR identity. New labels begin "owner/repo#N", which is durable data
// carried by Herdr's workspace list and is matched case-insensitively because
// GitHub owner and repository names are case-insensitive.
//
// Labels created by older sesh-bro versions contained only N (and possibly a
// title). They remain matchable, but only when Herdr independently ties the
// workspace to repoRoot: either workspace.worktree.repo_root matches, or a
// pane's cwd is that root. A number-only label without repository evidence is
// deliberately not a match; guessing would recreate the cross-repo bug this
// function exists to prevent. A qualified label for another repository is
// never treated as legacy, even if its path happens to match.
//
// repoRoot must be a caller-verified checkout of owner/repo. Passing an empty
// root disables legacy matching while retaining qualified-label matching.
func WorkspaceForIssue(workspaces []herdr.Workspace, panes []herdr.Pane, owner, repo, num, repoRoot string) (string, bool) {
	want := owner + "/" + repo + "#" + num

	// Prefer an explicit identity regardless of workspace-list order. This
	// prevents an earlier legacy candidate from shadowing a newer exact label.
	for _, w := range workspaces {
		m := qualifiedIssueLabelRe.FindStringSubmatch(w.Label)
		if len(m) == 0 {
			continue
		}
		identity := m[1] + "/" + m[2] + "#" + m[3]
		suffix := strings.TrimPrefix(w.Label, m[0])
		validShape := suffix == "" || strings.HasPrefix(suffix, " — ")
		if validShape && strings.EqualFold(identity, want) {
			return w.ID, true
		}
	}
	if repoRoot == "" {
		return "", false
	}

	numberRe, err := regexp.Compile(`(^|[^0-9])` + regexp.QuoteMeta(num) + `([^0-9]|$)`)
	if err != nil {
		return "", false
	}
	root := filepath.Clean(repoRoot)
	paneAtRoot := make(map[string]bool)
	for _, p := range panes {
		if p.CWD != "" && filepath.Clean(p.CWD) == root {
			paneAtRoot[p.WorkspaceID] = true
		}
	}
	for _, w := range workspaces {
		if qualifiedIssueLabelRe.MatchString(w.Label) || !numberRe.MatchString(w.Label) {
			continue
		}
		worktreeAtRoot := w.Worktree != nil && w.Worktree.RepoRoot != "" && filepath.Clean(w.Worktree.RepoRoot) == root
		if worktreeAtRoot || paneAtRoot[w.ID] {
			return w.ID, true
		}
	}
	return "", false
}
