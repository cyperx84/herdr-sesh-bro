package external

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// liveGhIssueList is `gh issue list --json number,title,url,author,labels`
// captured from a real repository. A real fixture rather than a written one,
// because the narrowing has to survive the fields this package ignores — and a
// hand-written sample only ever contains the fields its author remembered.
const liveGhIssueList = `[{"author":{"id":"MDQ6VXNlcjQ3MzM5MzM4","is_bot":false,"login":"cyperx84","name":"Cyperx"},"labels":[],"number":4,"title":"Measured: 3-4 concurrent cmd agents are safe","url":"https://github.com/cyperx84/herdr-loop/issues/4"},{"author":{"login":"someone"},"labels":[{"id":"L1","name":"bug"},{"id":"L2","name":"help wanted"}],"number":3,"title":"No 'contains' predicate","url":"https://github.com/cyperx84/herdr-loop/issues/3"}]`

func fakeRunner(out string, err error) (IssueRunner, *[]string) {
	var args []string
	return func(_ context.Context, dir, name string, a ...string) ([]byte, error) {
		args = append([]string{dir, name}, a...)
		return []byte(out), err
	}, &args
}

func TestListIssuesNarrowsLiveOutput(t *testing.T) {
	run, _ := fakeRunner(liveGhIssueList, nil)
	got, err := ListIssues(t.Context(), "/repo", "gh", run)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d issues", len(got))
	}
	if got[0].Number != 4 || got[0].Author != "cyperx84" || len(got[0].Labels) != 0 {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Labels[0] != "bug" || got[1].Labels[1] != "help wanted" {
		t.Errorf("labels = %v", got[1].Labels)
	}
	if !strings.HasSuffix(got[1].URL, "/issues/3") {
		t.Errorf("url = %q", got[1].URL)
	}
}

// TestListIssuesRunsInTheRepo: `gh issue list` picks its repository from the
// working directory's origin remote, so running it anywhere else silently
// lists a different project's issues.
func TestListIssuesRunsInTheRepo(t *testing.T) {
	run, args := fakeRunner("[]", nil)
	if _, err := ListIssues(t.Context(), "/repo/root", "gh", run); err != nil {
		t.Fatal(err)
	}
	if (*args)[0] != "/repo/root" {
		t.Fatalf("ran in %q, want the repo root", (*args)[0])
	}
	joined := strings.Join(*args, " ")
	for _, want := range []string{"issue list", "--state open", "--json number,title,url,author,labels"} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv %q missing %q", joined, want)
		}
	}
	// PRs are excluded on purpose: Enter on a PR row would create a fresh
	// branch named for its number instead of checking out its head branch.
	if strings.Contains(joined, "pr list") {
		t.Error("PRs are not listed this release; see ListIssues' doc comment")
	}
}

// TestListIssuesSoftFails: gh exits non-zero for "no GitHub remote" and "not
// authenticated" alike, and neither should be reported as an error on every
// render. Same soft-dependency contract zoxide has.
func TestListIssuesSoftFails(t *testing.T) {
	run, _ := fakeRunner("", errors.New("exit 1"))
	got, err := ListIssues(t.Context(), "/repo", "gh", run)
	if err != nil || got != nil {
		t.Fatalf("got %v, %v; want nothing and no error", got, err)
	}
}

func TestListIssuesNoRepoRoot(t *testing.T) {
	run, args := fakeRunner(liveGhIssueList, nil)
	got, err := ListIssues(t.Context(), "", "gh", run)
	if err != nil || got != nil {
		t.Fatalf("got %v, %v", got, err)
	}
	if len(*args) != 0 {
		t.Error("gh was run with no repository to run it in")
	}
}

func TestIssueCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := IssueCachePath(dir, "/Users/x/github/proj")
	now := time.Now()
	want := []Issue{{Number: 1, Title: "t", URL: "u", Author: "a", Labels: []string{"bug"}}}
	if err := WriteIssueCache(path, "/Users/x/github/proj", want, now); err != nil {
		t.Fatal(err)
	}
	got, ok := ReadIssueCache(path, now.Add(time.Minute))
	if !ok || len(got) != 1 || got[0].Number != 1 || got[0].Labels[0] != "bug" {
		t.Fatalf("got %v, ok=%v", got, ok)
	}
}

// TestIssueCacheExpires and its sibling below pin the TTL boundary from both
// sides, because an off-by-one here is either a network call every render or a
// list that never updates.
func TestIssueCacheExpires(t *testing.T) {
	dir := t.TempDir()
	path := IssueCachePath(dir, "/repo")
	now := time.Now()
	if err := WriteIssueCache(path, "/repo", []Issue{{Number: 1}}, now); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadIssueCache(path, now.Add(IssueCacheTTL)); ok {
		t.Error("cache served at exactly the TTL; it should be stale")
	}
	if _, ok := ReadIssueCache(path, now.Add(IssueCacheTTL-time.Second)); !ok {
		t.Error("cache expired a second early")
	}
}

// TestIssueCacheStoresEmpty: "this repo has no open issues" is a real answer
// that cost a network round trip. Not caching it means paying again every
// render.
func TestIssueCacheStoresEmpty(t *testing.T) {
	dir := t.TempDir()
	path := IssueCachePath(dir, "/repo")
	now := time.Now()
	if err := WriteIssueCache(path, "/repo", nil, now); err != nil {
		t.Fatal(err)
	}
	got, ok := ReadIssueCache(path, now)
	if !ok {
		t.Fatal("an empty result was not cached")
	}
	if len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestReadIssueCacheDegradesQuietly(t *testing.T) {
	dir := t.TempDir()
	if _, ok := ReadIssueCache(IssueCachePath(dir, "/missing"), time.Now()); ok {
		t.Error("a missing file reported a hit")
	}
	bad := IssueCachePath(dir, "/bad")
	if err := writeFile(bad, "{not json"); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadIssueCache(bad, time.Now()); ok {
		t.Error("malformed JSON reported a hit")
	}
	future := IssueCachePath(dir, "/future")
	if err := writeFile(future, `{"version":99,"fetched_unix_ms":0,"issues":[]}`); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadIssueCache(future, time.Now()); ok {
		t.Error("a future schema version reported a hit")
	}
}

// TestIssueCachePathIsPerRepo: a worktree and its parent checkout are two roots
// of the same repository, and this plugin creates such pairs routinely. They
// must not share a cache entry.
func TestIssueCachePathIsPerRepo(t *testing.T) {
	a := IssueCachePath("/c", "/Users/x/github/proj")
	b := IssueCachePath("/c", "/Users/x/github/proj-wt-409")
	if a == b {
		t.Fatalf("two roots share a cache file: %s", a)
	}
	if strings.Contains(strings.TrimPrefix(a, "/c/"), "/") {
		t.Errorf("cache path nests directories: %s", a)
	}
}

func writeFile(path, body string) error {
	return osWriteFile(path, body)
}
