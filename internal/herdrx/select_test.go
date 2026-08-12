package herdrx

import (
	"testing"

	herdr "github.com/cyperx84/herdr-api"
)

func TestFocusedOrFirstPane_PrefersFocused(t *testing.T) {
	panes := []herdr.Pane{{ID: "p1"}, {ID: "p2", Focused: true}, {ID: "p3"}}
	id, ok := FocusedOrFirstPane(panes)
	if !ok || id != "p2" {
		t.Fatalf("got (%q, %v), want (\"p2\", true)", id, ok)
	}
}

func TestFocusedOrFirstPane_FallsBackToFirst(t *testing.T) {
	panes := []herdr.Pane{{ID: "p1"}, {ID: "p2"}}
	id, ok := FocusedOrFirstPane(panes)
	if !ok || id != "p1" {
		t.Fatalf("got (%q, %v), want (\"p1\", true)", id, ok)
	}
}

func TestFocusedOrFirstPane_Empty(t *testing.T) {
	if _, ok := FocusedOrFirstPane(nil); ok {
		t.Fatalf("expected ok=false for no panes")
	}
}

func TestWorkspaceForCWD_PrefersFocusedOverFirstMatch(t *testing.T) {
	panes := []herdr.Pane{
		{WorkspaceID: "w1", CWD: "/x"},                // first match, not focused
		{WorkspaceID: "w2", CWD: "/x", Focused: true}, // later, but focused
	}
	ws, ok := WorkspaceForCWD(panes, "/x")
	if !ok || ws != "w2" {
		t.Fatalf("got (%q, %v), want (\"w2\", true) — focused pane wins", ws, ok)
	}
}

func TestWorkspaceForCWD_FallsBackToFirstMatch(t *testing.T) {
	panes := []herdr.Pane{
		{WorkspaceID: "w1", CWD: "/x"},
		{WorkspaceID: "w2", CWD: "/x"},
	}
	ws, ok := WorkspaceForCWD(panes, "/x")
	if !ok || ws != "w1" {
		t.Fatalf("got (%q, %v), want (\"w1\", true)", ws, ok)
	}
}

func TestWorkspaceForCWD_NoMatch(t *testing.T) {
	if _, ok := WorkspaceForCWD([]herdr.Pane{{WorkspaceID: "w1", CWD: "/y"}}, "/x"); ok {
		t.Fatalf("expected no match")
	}
}

func TestWorkspaceForCWDAny_NoFocusedPreference(t *testing.T) {
	// §2.6: root has no focused-pane preference, unlike connect dir — the
	// first pane in list order wins even if a later one is focused.
	panes := []herdr.Pane{
		{WorkspaceID: "w1", CWD: "/x"},
		{WorkspaceID: "w2", CWD: "/x", Focused: true},
	}
	ws, ok := WorkspaceForCWDAny(panes, "/x")
	if !ok || ws != "w1" {
		t.Fatalf("got (%q, %v), want (\"w1\", true) — first match wins regardless of focus", ws, ok)
	}
}

func TestPreviousWorkspace_FocusedFirstThenNumberDescending(t *testing.T) {
	workspaces := []herdr.Workspace{
		{ID: "w1", Number: 1},
		{ID: "w2", Number: 2},
		{ID: "w3", Number: 3, Focused: true},
	}
	ws, ok := PreviousWorkspace(workspaces, "w1")
	if !ok || ws != "w3" {
		t.Fatalf("got (%q, %v), want (\"w3\", true) — focused sorts first", ws, ok)
	}
}

func TestPreviousWorkspace_DescendingNumberAmongUnfocused(t *testing.T) {
	workspaces := []herdr.Workspace{
		{ID: "w1", Number: 1},
		{ID: "w3", Number: 3},
		{ID: "w2", Number: 2},
	}
	ws, ok := PreviousWorkspace(workspaces, "w1")
	if !ok || ws != "w3" {
		t.Fatalf("got (%q, %v), want (\"w3\", true) — highest number among unfocused", ws, ok)
	}
}

func TestPreviousWorkspace_ExcludesCurrent(t *testing.T) {
	workspaces := []herdr.Workspace{{ID: "w1", Number: 1}}
	if _, ok := PreviousWorkspace(workspaces, "w1"); ok {
		t.Fatalf("expected no previous workspace when it's the only one and it's current")
	}
}

func TestPreviousWorkspace_EmptyCurrentRefocusesFocused(t *testing.T) {
	// §9 S2: current == "" excludes nothing, so the already-focused
	// workspace sorts to position 0 — reproduced deliberately, not fixed.
	workspaces := []herdr.Workspace{
		{ID: "w1", Number: 1},
		{ID: "w2", Number: 2, Focused: true},
	}
	ws, ok := PreviousWorkspace(workspaces, "")
	if !ok || ws != "w2" {
		t.Fatalf("got (%q, %v), want (\"w2\", true) per S2's documented no-op", ws, ok)
	}
}

func TestWorkspaceForIssue_RepoQualifiedSameNumberAcrossOwnersAndRepos(t *testing.T) {
	workspaces := []herdr.Workspace{
		{ID: "owner-a", Label: "ownerA/repo#409 — first"},
		{ID: "repo-b", Label: "ownerB/other#409 — second"},
		{ID: "wanted", Label: "ownerB/repo#409 — wanted"},
	}
	ws, ok := WorkspaceForIssue(workspaces, nil, "ownerB", "repo", "409", "")
	if !ok || ws != "wanted" {
		t.Fatalf("got (%q, %v), want (\"wanted\", true)", ws, ok)
	}
}

func TestWorkspaceForIssue_QualifiedIdentityIsCaseInsensitive(t *testing.T) {
	workspaces := []herdr.Workspace{{ID: "w1", Label: "Owner/Repo#409"}}
	ws, ok := WorkspaceForIssue(workspaces, nil, "owner", "repo", "409", "")
	if !ok || ws != "w1" {
		t.Fatalf("got (%q, %v), want (\"w1\", true)", ws, ok)
	}
}

func TestWorkspaceForIssue_LegacyLabelRequiresMatchingWorktreeRepoRoot(t *testing.T) {
	workspaces := []herdr.Workspace{
		{ID: "wrong", Label: "409 — old title", Worktree: &herdr.WorkspaceWorktree{RepoRoot: "/src/owner-a/repo"}},
		{ID: "wanted", Label: "409 — old title", Worktree: &herdr.WorkspaceWorktree{RepoRoot: "/src/owner-b/repo"}},
	}
	ws, ok := WorkspaceForIssue(workspaces, nil, "owner-b", "repo", "409", "/src/owner-b/repo")
	if !ok || ws != "wanted" {
		t.Fatalf("got (%q, %v), want legacy workspace at matching repo root", ws, ok)
	}
}

func TestWorkspaceForIssue_LegacyPlainWorkspaceUsesPaneCWD(t *testing.T) {
	workspaces := []herdr.Workspace{{ID: "wrong", Label: "409"}, {ID: "wanted", Label: "409 — title"}}
	panes := []herdr.Pane{{WorkspaceID: "wrong", CWD: "/src/other"}, {WorkspaceID: "wanted", CWD: "/src/repo"}}
	ws, ok := WorkspaceForIssue(workspaces, panes, "owner", "repo", "409", "/src/repo")
	if !ok || ws != "wanted" {
		t.Fatalf("got (%q, %v), want legacy plain workspace at matching cwd", ws, ok)
	}
}

func TestWorkspaceForIssue_LegacyLabelWithoutRepoEvidenceDoesNotMatch(t *testing.T) {
	workspaces := []herdr.Workspace{{ID: "w1", Label: "409 — old title"}}
	if _, ok := WorkspaceForIssue(workspaces, nil, "ownerB", "repo", "409", ""); ok {
		t.Fatal("number-only legacy label must not match without verified repository evidence")
	}
}

func TestWorkspaceForIssue_OtherQualifiedLabelNeverFallsBackToLegacy(t *testing.T) {
	workspaces := []herdr.Workspace{{ID: "wrong", Label: "ownerA/repo#409 — title"}}
	panes := []herdr.Pane{{WorkspaceID: "wrong", CWD: "/src/repo"}}
	if _, ok := WorkspaceForIssue(workspaces, panes, "ownerB", "repo", "409", "/src/repo"); ok {
		t.Fatal("a qualified label for another owner must not be reinterpreted as legacy")
	}
}

func TestWorkspaceForIssue_ExplicitIdentityPreferredOverEarlierLegacy(t *testing.T) {
	workspaces := []herdr.Workspace{
		{ID: "legacy", Label: "409", Worktree: &herdr.WorkspaceWorktree{RepoRoot: "/src/repo"}},
		{ID: "qualified", Label: "owner/repo#409"},
	}
	ws, _ := WorkspaceForIssue(workspaces, nil, "owner", "repo", "409", "/src/repo")
	if ws != "qualified" {
		t.Fatalf("got %q, want explicit qualified match", ws)
	}
}
