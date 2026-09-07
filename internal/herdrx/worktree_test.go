package herdrx

import (
	"context"
	"encoding/json"
	"testing"

	herdr "github.com/cyperx84/herdr-api"

	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx/herdrtest"
)

func TestWorktreeRows_OpenWorkspaceSkipped(t *testing.T) {
	// THE DEDUPE RULE. An open worktree appearing here means every open
	// workspace is listed twice — once as a workspace row and once as a
	// worktree row — and with different labels, so no downstream dedupe
	// could catch it. The entire reason this source exists is the worktrees
	// that are NOT open; a failure here means the picker got the duplicate
	// noise instead of the new information.
	in := []herdr.Worktree{
		{Path: "/repo/.wt/issue-12", Branch: strp("issue-12"), OpenWorkspaceID: strp("w-1")},
		{Path: "/repo/.wt/issue-14", Branch: strp("issue-14")},
	}
	rows := WorktreeRows(in, "")
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1 (open worktree must be deduped, unopened kept)", len(rows))
	}
	if rows[0].Target != "/repo/.wt/issue-14" {
		t.Fatalf("kept worktree = %q, want the unopened one", rows[0].Target)
	}
}

func TestWorktreeRows_BareSkipped(t *testing.T) {
	// A bare worktree in the picker is a row whose only possible action
	// fails: there is no working tree to open a workspace into. A failure
	// here means the picker offers an action that cannot succeed.
	in := []herdr.Worktree{
		{Path: "/repo/.bare-main", Branch: strp("main"), IsBare: true},
		{Path: "/repo/.wt/issue-14", Branch: strp("issue-14")},
	}
	rows := WorktreeRows(in, "")
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1 (bare skipped, linked kept)", len(rows))
	}
	if rows[0].Target != "/repo/.wt/issue-14" {
		t.Fatalf("kept worktree = %q, want the non-bare one", rows[0].Target)
	}
}

func TestWorktreeRows_PrunableKeptButMarked(t *testing.T) {
	// A prunable worktree is most commonly one whose directory is gone. It
	// must still appear — the human can only decide to run `git worktree
	// prune` if something tells them it's there — but visibly marked, so
	// they don't pick it expecting it to open. A failure here means either
	// the row vanished (information lost) or the marker did (a dead
	// worktree indistinguishable from a live one).
	in := []herdr.Worktree{
		{Path: "/repo/.wt/gone", Branch: strp("stale"), IsPrunable: true},
		{Path: "/repo/.wt/live", Branch: strp("live")},
	}
	rows := WorktreeRows(in, "")
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (prunable is shown, not hidden)", len(rows))
	}
	if rows[0].Detail != "/repo/.wt/gone · prunable" {
		t.Fatalf("prunable detail = %q, want path + prunable marker", rows[0].Detail)
	}
	if rows[1].Detail != "/repo/.wt/live" {
		t.Fatalf("live detail = %q, want bare path with no marker", rows[1].Detail)
	}
}

func TestWorktreeRows_LabelFallbacks(t *testing.T) {
	// A detached worktree has no branch (herdr sends null) but must still
	// be identifiable — an empty label renders as a blank the picker can
	// neither display nor choose between. The fallback chain is branch,
	// then herdr's own label, then the path's basename; each case here
	// fails if its fallback silently produced "".
	cases := []struct {
		name string
		in   herdr.Worktree
		want string
	}{
		{
			name: "branch wins when present",
			in:   herdr.Worktree{Path: "/repo/.wt/a", Branch: strp("feature-x"), Label: "ignored"},
			want: "feature-x",
		},
		{
			name: "label when no branch",
			in:   herdr.Worktree{Path: "/repo/.wt/a", Label: "issue-14 fixes"},
			want: "issue-14 fixes",
		},
		{
			name: "basename when detached and unlabelled",
			in:   herdr.Worktree{Path: "/repo/.wt/detached-abc", IsDetached: true},
			want: "detached-abc",
		},
	}
	for _, tc := range cases {
		rows := WorktreeRows([]herdr.Worktree{tc.in}, "")
		if len(rows) != 1 {
			t.Fatalf("%s: got %d rows, want 1", tc.name, len(rows))
		}
		if rows[0].Label != tc.want {
			t.Errorf("%s: label = %q, want %q", tc.name, rows[0].Label, tc.want)
		}
	}
}

func TestWorktreeRows_Blacklist(t *testing.T) {
	// The blacklist must mean the same thing here as it does for zoxide
	// dirs (§9 S6) — one matcher, reused. A failure here means a path the
	// human explicitly blacklisted from the picker reappears the moment it
	// happens to be a worktree instead of a zoxide entry.
	in := []herdr.Worktree{
		{Path: "/tmp/scratch-wt", Branch: strp("scratch")},
		{Path: "/repo/.wt/issue-14", Branch: strp("issue-14")},
	}
	rows := WorktreeRows(in, "/tmp/*")
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1 (blacklisted path dropped)", len(rows))
	}
	if rows[0].Target != "/repo/.wt/issue-14" {
		t.Fatalf("kept worktree = %q, want the non-blacklisted one", rows[0].Target)
	}
}

func TestWorktreeRows_EmptyInput(t *testing.T) {
	// Empty in, empty (non-nil-shaped, zero-length) out. A failure here is
	// a nil-deref or a panic in the caller that ranges over the result —
	// the repo with no worktrees at all is the common case, not an edge.
	rows := WorktreeRows(nil, "")
	if len(rows) != 0 {
		t.Fatalf("got %d rows, want 0 for nil input", len(rows))
	}
	rows = WorktreeRows([]herdr.Worktree{}, "/tmp/*")
	if len(rows) != 0 {
		t.Fatalf("got %d rows, want 0 for empty input", len(rows))
	}
}

func TestWorktreeRows_Fields(t *testing.T) {
	// Locks the field contract in one place: target is the PATH (that is
	// what opens the worktree), status is the dir placeholder (a worktree
	// hosts no agent), type is RowWorktree. A failure here means the wiring
	// in list/connect was built against a different row shape.
	rows := WorktreeRows([]herdr.Worktree{
		{Path: "/repo/.wt/issue-14", Branch: strp("issue-14")},
	}, "")
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	r := rows[0]
	if r.Type != RowWorktree {
		t.Errorf("Type = %q, want %q", r.Type, RowWorktree)
	}
	if r.Target != "/repo/.wt/issue-14" {
		t.Errorf("Target = %q, want the worktree path", r.Target)
	}
	if r.Status != dirStatus {
		t.Errorf("Status = %q, want dir placeholder %q", r.Status, dirStatus)
	}
	if r.PaneID != "" {
		t.Errorf("PaneID = %q, want empty (worktrees have no agent pane)", r.PaneID)
	}
}

func TestListWorktrees(t *testing.T) {
	// Drives the real herdr-api client against the fake daemon to pin the
	// wire traffic: the call must be worktree.list with a cwd param (the
	// scope that makes it per-repo), and the result must be the worktrees
	// array from the response. A failure in the params assertion means the
	// daemon would resolve the repo from the focused workspace instead of
	// the cwd — i.e. list some other repo's worktrees, the trap
	// CreateWorktree's doc comment documents from a sibling project.
	srv := herdrtest.Start(t)
	srv.Handle("worktree.list", func(params json.RawMessage) (any, error) {
		return map[string]any{
			"source": map[string]any{
				"repo_key": "k", "repo_name": "repo", "repo_root": "/repo",
			},
			"worktrees": []map[string]any{
				{"path": "/repo", "branch": "main", "open_workspace_id": "w-1"},
				{"path": "/repo/.wt/issue-14", "branch": "issue-14"},
			},
		}, nil
	})

	c := New(herdr.New(srv.Path()))
	got, err := c.ListWorktrees(context.Background(), "/repo")
	if err != nil {
		t.Fatalf("ListWorktrees: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d worktrees, want 2", len(got))
	}
	if got[0].Path != "/repo" || got[0].OpenWorkspaceID == nil || *got[0].OpenWorkspaceID != "w-1" {
		t.Errorf("worktree[0] = %+v, want open main checkout", got[0])
	}
	if got[1].OpenWorkspaceID != nil {
		t.Errorf("worktree[1].OpenWorkspaceID = %v, want nil (unopened)", got[1].OpenWorkspaceID)
	}

	// Assert the cwd scoping made it onto the wire.
	calls := srv.Calls("worktree.list")
	if len(calls) != 1 {
		t.Fatalf("got %d worktree.list calls, want 1", len(calls))
	}
	var params struct {
		CWD *string `json:"cwd"`
	}
	if err := json.Unmarshal(calls[0].Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if params.CWD == nil || *params.CWD != "/repo" {
		t.Errorf("params.cwd = %v, want %q (per-repo scoping must be explicit)", params.CWD, "/repo")
	}
}

func TestListWorktrees_Error(t *testing.T) {
	// An API error must surface wrapped, not as an empty list — a caller
	// that treats []Worktree + nil error as "no worktrees" would show an
	// empty worktree section when the daemon is misbehaving, and the human
	// cannot tell "repo has no worktrees" from "the call failed".
	srv := herdrtest.Start(t)
	srv.Handle("worktree.list", func(params json.RawMessage) (any, error) {
		return nil, &herdrtest.APIError{Code: "not_a_repo", Message: "no git repo at /tmp"}
	})

	c := New(herdr.New(srv.Path()))
	got, err := c.ListWorktrees(context.Background(), "/tmp")
	if err == nil {
		t.Fatalf("got nil error, want wrapped API error")
	}
	if got != nil {
		t.Errorf("got %v worktrees with an error, want nil", got)
	}
}
