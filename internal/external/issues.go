package external

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

// Issue is one open GitHub issue, narrowed to what a picker row can show.
type Issue struct {
	Number int      `json:"number"`
	Title  string   `json:"title"`
	URL    string   `json:"url"`
	Author string   `json:"author"`
	Labels []string `json:"labels"`
}

// ghIssue mirrors `gh issue list --json`'s shape, which nests the author and
// the labels as objects. Unexported so the wire layout can change without
// widening this package's API — the same reason herdrx narrows its snapshot.
type ghIssue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

// issueLimit caps how many issues are fetched.
//
// A picker row costs a line and a keypress to scroll past, so a repo with four
// hundred open issues would bury every other source under itself. Fifty is
// enough to find the one you meant with fzf's filter and few enough that the
// block stays skimmable. gh returns them newest first.
const issueLimit = 50

// issueTimeout bounds the gh call.
//
// gh talks to the network, and the one thing this must never do is hang. It
// runs on a warm-up goroutine rather than the picker's open path, so a timeout
// costs a view that stays empty until the next attempt — not a picker that
// will not open.
const issueTimeout = 5 * time.Second

// ListIssues returns the open issues of the repository checked out at
// repoRoot.
//
// Pull requests are deliberately NOT included, even though `gh issue list`
// excludes them and `gh pr list` would be one more call. The row is only worth
// having because of what Enter does to it — create a worktree for that issue —
// and a PR needs its OWN head branch checked out, not a fresh branch named for
// its number. Listing PRs with the wrong Enter action would be a row that looks
// right and does the wrong thing, which is worse than a row that is not there.
//
// gh missing, not authenticated, no GitHub remote, not a repository: all yield
// no issues and no error. This is a soft dependency exactly like zoxide — the
// block is absent and everything else works.
func ListIssues(ctx context.Context, repoRoot, ghBin string, run IssueRunner) ([]Issue, error) {
	if repoRoot == "" {
		return nil, nil
	}
	if ghBin == "" {
		ghBin = "gh"
	}
	if run == nil {
		if _, err := exec.LookPath(ghBin); err != nil {
			return nil, nil
		}
		run = execIssueRunner
	}

	ctx, cancel := context.WithTimeout(ctx, issueTimeout)
	defer cancel()

	out, err := run(ctx, repoRoot, ghBin,
		"issue", "list",
		"--state", "open",
		"--limit", strconv.Itoa(issueLimit),
		"--json", "number,title,url,author,labels",
	)
	if err != nil {
		// Not a failure worth propagating: gh exits non-zero for "no GitHub
		// remote" and "not authenticated" alike, and neither is something the
		// picker should report as an error every time it renders.
		return nil, nil
	}

	var raw []ghIssue
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("external: parse gh issue list: %w", err)
	}
	issues := make([]Issue, 0, len(raw))
	for _, r := range raw {
		labels := make([]string, 0, len(r.Labels))
		for _, l := range r.Labels {
			labels = append(labels, l.Name)
		}
		issues = append(issues, Issue{
			Number: r.Number,
			Title:  r.Title,
			URL:    r.URL,
			Author: r.Author.Login,
			Labels: labels,
		})
	}
	return issues, nil
}

// IssueRunner executes gh in a directory and returns its stdout. It is the
// seam that lets tests supply canned output with no gh binary and no network.
type IssueRunner func(ctx context.Context, dir, name string, args ...string) ([]byte, error)

// execIssueRunner is the default: gh, run IN the repository, because `gh issue
// list` resolves which repo it means from the working directory's origin
// remote. Running it anywhere else silently lists a different project's issues.
func execIssueRunner(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	var buf bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
