package herdrx

import (
	"strconv"
	"strings"

	"github.com/cyperx84/herdr-sesh-bro/internal/external"
)

// RowIssue is an open GitHub issue in the repository the picker was opened
// from. Enter creates a worktree for it.
//
// It is the picker's fifth source and the only one that is not already
// somewhere on the machine: workspaces, agents, directories and worktrees all
// name something that exists, while an issue names work that does not exist
// yet. That is what makes it worth a row — the gap between "I should look at
// #12" and "I am in a branch for #12" is the friction this closes.
const RowIssue RowType = "issue"

// issueStatus is the placeholder for the status column, which an issue has
// nothing to put in. Same value and same reason as dirStatus.
const issueStatus = "-"

// IssueRows converts gh's open issues into picker rows.
//
// The target is the issue URL rather than its number, because the URL is what
// the `worktree` command already takes and what its ref parser already
// understands. A number would have to be re-qualified with an owner and repo
// at the point of use, from context the row no longer carries.
//
// The label leads with "#N" so the number stays visible after fzf's filter has
// narrowed on words from the title, and so two issues whose titles begin
// alike are still told apart at a glance.
func IssueRows(issues []external.Issue) []Row {
	rows := make([]Row, 0, len(issues))
	for _, is := range issues {
		if is.URL == "" {
			// No URL means nothing Enter could do with the row.
			continue
		}
		title := strings.TrimSpace(is.Title)
		if title == "" {
			title = "(no title)"
		}
		rows = append(rows, Row{
			Type:   RowIssue,
			Target: is.URL,
			Status: issueStatus,
			Label:  "#" + strconv.Itoa(is.Number) + " " + title,
			Detail: issueDetail(is),
		})
	}
	return rows
}

// issueDetail is the secondary column: who opened it, and how it is labelled.
//
// Labels are worth the space because they are how a repo says "this one is a
// bug" or "this one is blocked" — the closest thing an issue has to the status
// the other row types carry in a column this one leaves empty.
func issueDetail(is external.Issue) string {
	var parts []string
	if is.Author != "" {
		parts = append(parts, "@"+is.Author)
	}
	if len(is.Labels) > 0 {
		parts = append(parts, strings.Join(is.Labels, ", "))
	}
	return strings.Join(parts, " · ")
}
