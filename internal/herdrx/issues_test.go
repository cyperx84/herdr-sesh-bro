package herdrx

import (
	"strings"
	"testing"

	"github.com/cyperx84/herdr-sesh-bro/internal/external"
)

func TestIssueRows(t *testing.T) {
	rows := IssueRows([]external.Issue{
		{Number: 4, Title: "Measured: 3-4 concurrent agents are safe", URL: "https://x/issues/4", Author: "cyperx84"},
		{Number: 3, Title: "No 'contains' predicate", URL: "https://x/issues/3", Author: "someone", Labels: []string{"bug", "help wanted"}},
	})
	if len(rows) != 2 {
		t.Fatalf("got %d rows", len(rows))
	}
	// The target is the URL, because that is what `worktree` already takes and
	// what its ref parser already understands. A number would have to be
	// re-qualified with an owner and repo the row no longer carries.
	if rows[0].Target != "https://x/issues/4" {
		t.Errorf("target = %q", rows[0].Target)
	}
	// The number leads the label so it survives fzf filtering on title words.
	if !strings.HasPrefix(rows[0].Label, "#4 ") {
		t.Errorf("label = %q, want it to lead with the number", rows[0].Label)
	}
	if rows[1].Detail != "@someone · bug, help wanted" {
		t.Errorf("detail = %q", rows[1].Detail)
	}
	if rows[0].Detail != "@cyperx84" {
		t.Errorf("detail with no labels = %q", rows[0].Detail)
	}
}

// TestIssueRowsSkipsUrllessIssues: the URL is the whole action. A row Enter
// cannot do anything with is worse than no row.
func TestIssueRowsSkipsUrllessIssues(t *testing.T) {
	rows := IssueRows([]external.Issue{{Number: 1, Title: "t"}})
	if len(rows) != 0 {
		t.Fatalf("got %v", rows)
	}
}

// TestIssueRowsHandlesAnEmptyTitle: an issue with a blank title would render
// as a bare number followed by nothing, which reads as a broken row.
func TestIssueRowsHandlesAnEmptyTitle(t *testing.T) {
	rows := IssueRows([]external.Issue{{Number: 7, Title: "   ", URL: "u"}})
	if len(rows) != 1 || rows[0].Label != "#7 (no title)" {
		t.Fatalf("got %+v", rows)
	}
}

// TestIssueRowsAreLocal: issues belong to the repository the picker was opened
// from, so their Session must stay empty or every mutating guard would refuse
// them.
func TestIssueRowsAreLocal(t *testing.T) {
	rows := IssueRows([]external.Issue{{Number: 1, Title: "t", URL: "u"}})
	if rows[0].Session != "" {
		t.Fatalf("issue row carries Session %q", rows[0].Session)
	}
}
