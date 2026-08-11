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

func TestWorkspaceForIssueNumber_StandaloneMatch(t *testing.T) {
	workspaces := []herdr.Workspace{
		{ID: "w1", Label: "4091 unrelated"}, // 409 is a substring, not standalone
		{ID: "w2", Label: "409 — some title"},
	}
	ws, ok := WorkspaceForIssueNumber(workspaces, "409")
	if !ok || ws != "w2" {
		t.Fatalf("got (%q, %v), want (\"w2\", true) — standalone match only", ws, ok)
	}
}

func TestWorkspaceForIssueNumber_RepoAgnostic(t *testing.T) {
	// §9 S15: matches any workspace with the number, regardless of repo.
	workspaces := []herdr.Workspace{{ID: "w1", Label: "409 — owner-b/other-repo thing"}}
	ws, ok := WorkspaceForIssueNumber(workspaces, "409")
	if !ok || ws != "w1" {
		t.Fatalf("got (%q, %v), want (\"w1\", true)", ws, ok)
	}
}

func TestWorkspaceForIssueNumber_NoMatch(t *testing.T) {
	workspaces := []herdr.Workspace{{ID: "w1", Label: "unrelated"}}
	if _, ok := WorkspaceForIssueNumber(workspaces, "409"); ok {
		t.Fatalf("expected no match")
	}
}

func TestWorkspaceForIssueNumber_FirstMatchInOrder(t *testing.T) {
	workspaces := []herdr.Workspace{
		{ID: "w1", Label: "409 first"},
		{ID: "w2", Label: "409 second"},
	}
	ws, _ := WorkspaceForIssueNumber(workspaces, "409")
	if ws != "w1" {
		t.Fatalf("got %q, want first match w1", ws)
	}
}
