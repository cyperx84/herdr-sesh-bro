package herdrx

import (
	"regexp"
	"sort"

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

// workspaceLabelNumberRe builds BEHAVIOUR.md §2.7's existing-workspace test,
// `(^|[^0-9])N([^0-9]|$)`, for a specific issue/PR number. num is always a
// run of digits (captured from a `[0-9]+` URL match upstream), so
// regexp.QuoteMeta is a no-op in practice; it's here defensively in case
// that constraint is ever loosened.
func workspaceLabelNumberRe(num string) (*regexp.Regexp, error) {
	return regexp.Compile(`(^|[^0-9])` + regexp.QuoteMeta(num) + `([^0-9]|$)`)
}

// WorkspaceForIssueNumber returns the id of the first workspace (in
// workspaces order) whose label contains num as a standalone integer — not
// scoped to any particular repo. Matches BEHAVIOUR.md §2.7's existing-
// workspace check for `worktree`.
//
// §9 S15: this is deliberately repo-agnostic — a workspace labelled
// "409 — something unrelated" matches `worktree owner/repo#409` regardless
// of repo. S15 also notes that a null `.label` would crash the bash's jq
// `test()` under set -e; Workspace.Label is a plain Go string, so that
// failure mode cannot occur in this type — it's structurally unrepresentable
// here, not fixed.
func WorkspaceForIssueNumber(workspaces []herdr.Workspace, num string) (string, bool) {
	re, err := workspaceLabelNumberRe(num)
	if err != nil {
		return "", false
	}
	for _, w := range workspaces {
		if re.MatchString(w.Label) {
			return w.ID, true
		}
	}
	return "", false
}
