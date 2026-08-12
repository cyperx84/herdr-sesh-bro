package external

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------- deps ----

func TestHerdrNotFoundError_Message(t *testing.T) {
	err := &HerdrNotFoundError{Bin: "/opt/nope/herdr"}
	want := "sesh-bro: herdr binary not found (HERDR_BIN_PATH=/opt/nope/herdr)"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestDaemonNotRespondingError_Message(t *testing.T) {
	cases := []struct {
		suffix string
		want   string
	}{
		{"", "sesh-bro: herdr daemon is not responding"},
		{daemonSuffixStartup, "sesh-bro: herdr daemon is not responding (is 'herdr' running?)"},
	}
	for _, tc := range cases {
		err := &DaemonNotRespondingError{Suffix: tc.suffix}
		if got := err.Error(); got != tc.want {
			t.Errorf("Error() with suffix %q = %q, want %q", tc.suffix, got, tc.want)
		}
	}
}

func TestRequireHerdr(t *testing.T) {
	if err := RequireHerdr("/definitely/not/a/real/path/herdr-xyz"); err == nil {
		t.Error("RequireHerdr on a nonexistent path returned nil")
	} else {
		var hnf *HerdrNotFoundError
		if !errors.As(err, &hnf) || hnf.Bin != "/definitely/not/a/real/path/herdr-xyz" {
			t.Errorf("RequireHerdr error = %v, want *HerdrNotFoundError{Bin: ...}", err)
		}
	}

	// A real absolute path to an executable file succeeds — the same
	// property BEHAVIOUR.md §7.1 notes for `command -v`, and how the
	// original bash test suite's mock herdr binary is wired.
	self, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable unavailable: %v", err)
	}
	if err := RequireHerdr(self); err != nil {
		t.Errorf("RequireHerdr(%q) = %v, want nil", self, err)
	}
}

type fakeAliver bool

func (f fakeAliver) Alive(ctx context.Context) bool { return bool(f) }

func TestCheckDeps(t *testing.T) {
	self, _ := os.Executable()

	// Herdr binary missing: fails before the daemon is ever probed.
	if err := CheckDeps(context.Background(), "/nope/herdr", fakeAliver(true)); err == nil {
		t.Error("CheckDeps with a missing binary returned nil")
	} else if _, ok := err.(*HerdrNotFoundError); !ok {
		t.Errorf("CheckDeps binary-missing error = %T, want *HerdrNotFoundError", err)
	}

	// Binary present, daemon down: startup's longer message.
	err := CheckDeps(context.Background(), self, fakeAliver(false))
	if err == nil {
		t.Fatal("CheckDeps with a dead daemon returned nil")
	}
	want := "sesh-bro: herdr daemon is not responding (is 'herdr' running?)"
	if err.Error() != want {
		t.Errorf("CheckDeps daemon-down error = %q, want %q", err.Error(), want)
	}

	// Both present: success.
	if err := CheckDeps(context.Background(), self, fakeAliver(true)); err != nil {
		t.Errorf("CheckDeps with everything up = %v, want nil", err)
	}
}

func TestCheckListDeps_UsesShorterDaemonMessage(t *testing.T) {
	self, _ := os.Executable()
	err := CheckListDeps(context.Background(), self, fakeAliver(false))
	if err == nil {
		t.Fatal("CheckListDeps with a dead daemon returned nil")
	}
	want := "sesh-bro: herdr daemon is not responding"
	if err.Error() != want {
		t.Errorf("CheckListDeps daemon-down error = %q, want %q (no startup suffix)", err.Error(), want)
	}
}

// ----------------------------------------------------------------- git ----

func TestParseGitStatusBranch(t *testing.T) {
	cases := []struct {
		name       string
		output     string
		wantBranch string
		wantDirty  bool
	}{
		{
			name:       "clean tracked branch",
			output:     "## main...origin/main\n",
			wantBranch: "main",
			wantDirty:  false,
		},
		{
			name:       "dirty branch",
			output:     "## main...origin/main\n M foo.txt\n",
			wantBranch: "main",
			wantDirty:  true,
		},
		{
			name: "S7: dotted branch name truncates at the first dot",
			// bash's [^.]* stops at the first literal '.', so
			// "release/v1.2" must yield "release/v1", not the full name.
			// This is a documented bug, reproduced deliberately.
			output:     "## release/v1.2...origin/release/v1.2\n",
			wantBranch: "release/v1",
			wantDirty:  false,
		},
		{
			name:       "detached HEAD has no dot, captured whole",
			output:     "## HEAD (no branch)\n M foo.txt\n",
			wantBranch: "HEAD (no branch)",
			wantDirty:  true,
		},
		{
			name:       "no header at all",
			output:     "",
			wantBranch: "",
			wantDirty:  false,
		},
		{
			name:       "header without trailing newline",
			output:     "## main...origin/main",
			wantBranch: "main",
			wantDirty:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			branch, dirty := ParseGitStatusBranch(tc.output)
			if branch != tc.wantBranch || dirty != tc.wantDirty {
				t.Errorf("ParseGitStatusBranch(%q) = (%q, %v), want (%q, %v)",
					tc.output, branch, dirty, tc.wantBranch, tc.wantDirty)
			}
		})
	}
}

// initGitRepo creates a real git repo at dir (which must already exist),
// with one commit on branch, so tests exercise the actual git binary bash
// calls rather than a hand-written fixture of its output.
func initGitRepo(t *testing.T, dir, branch string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", branch)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f.txt")
	run("commit", "-q", "-m", "init")
}

// TestGitRoot_PathWithSpaces is one of this package's two named risks: a
// git repo whose path contains a space must round-trip intact through -C,
// since exec.Command never goes through a shell to mis-split it.
func TestGitRoot_PathWithSpaces(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "has space", "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, dir, "main")

	root, ok := GitRoot(context.Background(), "", dir)
	if !ok {
		t.Fatal("GitRoot on a real repo with a space in its path returned ok=false")
	}
	resolvedDir, _ := filepath.EvalSymlinks(dir)
	resolvedRoot, _ := filepath.EvalSymlinks(root)
	if resolvedRoot != resolvedDir {
		t.Errorf("GitRoot = %q (resolved %q), want repo dir %q (resolved %q)", root, resolvedRoot, dir, resolvedDir)
	}
}

// TestGitRoot_ThroughSymlink is this package's second named risk: a
// symlinked path into the repo must resolve the same as the real path.
// git may report either the symlink path or the physical path depending on
// version/config, so this asserts they resolve to the same place rather
// than hardcoding which form comes back.
func TestGitRoot_ThroughSymlink(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real-repo")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, real, "main")
	link := filepath.Join(base, "link-repo")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unsupported here: %v", err)
	}

	rootReal, okReal := GitRoot(context.Background(), "", real)
	rootLink, okLink := GitRoot(context.Background(), "", link)
	if !okReal || !okLink {
		t.Fatalf("GitRoot ok = (%v via real, %v via symlink), want both true", okReal, okLink)
	}
	resolvedReal, _ := filepath.EvalSymlinks(rootReal)
	resolvedLink, _ := filepath.EvalSymlinks(rootLink)
	if resolvedReal != resolvedLink {
		t.Errorf("GitRoot via real path resolves to %q, via symlink to %q, want equal", resolvedReal, resolvedLink)
	}
}

func TestGitRoot_NotARepo(t *testing.T) {
	dir := t.TempDir()
	if _, ok := GitRoot(context.Background(), "", dir); ok {
		t.Error("GitRoot on a non-repo directory returned ok=true")
	}
}

func TestGitEnrichment_OneCallPerWorkspace_CleanAndDirty(t *testing.T) {
	base := t.TempDir()
	cleanDir := filepath.Join(base, "clean")
	dirtyDir := filepath.Join(base, "dirty")
	notRepoDir := filepath.Join(base, "not-a-repo")
	for _, d := range []string{cleanDir, dirtyDir, notRepoDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	initGitRepo(t, cleanDir, "main")
	initGitRepo(t, dirtyDir, "release/v1.2") // exercises S7 through the real binary too
	if err := os.WriteFile(filepath.Join(dirtyDir, "f.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	wsCWD := map[string]string{
		"w-clean":   cleanDir,
		"w-dirty":   dirtyDir,
		"w-missing": notRepoDir,
	}
	got := GitEnrichment(context.Background(), "", wsCWD)

	if len(got) != 2 {
		t.Fatalf("GitEnrichment returned %d entries, want 2 (not-a-repo workspace must be absent): %+v", len(got), got)
	}
	if s := got["w-clean"]; s.Branch != "main" || s.Dirty {
		t.Errorf("w-clean = %+v, want {main false}", s)
	}
	if s := got["w-dirty"]; s.Branch != "release/v1" || !s.Dirty {
		t.Errorf("w-dirty = %+v, want {release/v1 true} (S7 truncation)", s)
	}
	if _, ok := got["w-missing"]; ok {
		t.Error("w-missing (not a git repo) got an enrichment entry, want none")
	}
}

func TestWorkspaceCWDs_PicksFirstPanePerWorkspaceInOrder(t *testing.T) {
	panes := []Pane{
		{WorkspaceID: "w2", CWD: "/second"},
		{WorkspaceID: "w1", CWD: "/first-w1"},
		{WorkspaceID: "w1", CWD: "/later-w1-ignored"},
		{WorkspaceID: "w2", CWD: "/later-w2-ignored"},
	}
	got := WorkspaceCWDs(panes)
	want := map[string]string{"w1": "/first-w1", "w2": "/second"}
	if len(got) != len(want) || got["w1"] != want["w1"] || got["w2"] != want["w2"] {
		t.Errorf("WorkspaceCWDs = %+v, want %+v", got, want)
	}
}

func TestWorkspaceCWDs_EmptyRepresentativePoisonsTheWorkspace(t *testing.T) {
	// BUG, REPRODUCED NOT FIXED (see WorkspaceCWDs' doc comment): the
	// FIRST pane for w1 has an empty cwd. Even though a later pane in the
	// same workspace has a perfectly good cwd, bash's group_by picks the
	// first pane as the representative BEFORE the emptiness filter runs,
	// so w1 gets no entry at all — not "/good".
	panes := []Pane{
		{WorkspaceID: "w1", CWD: ""},
		{WorkspaceID: "w1", CWD: "/good"},
	}
	got := WorkspaceCWDs(panes)
	if v, ok := got["w1"]; ok {
		t.Errorf("WorkspaceCWDs[w1] = %q, want absent (poisoned by the empty-cwd first pane)", v)
	}
}

func TestWorkspaceCWDs_EmptyWorkspaceIDDropped(t *testing.T) {
	panes := []Pane{{WorkspaceID: "", CWD: "/somewhere"}}
	got := WorkspaceCWDs(panes)
	if len(got) != 0 {
		t.Errorf("WorkspaceCWDs with empty workspace id = %+v, want empty map", got)
	}
}

func TestGitAvailable(t *testing.T) {
	// This machine has git (verified by every other test in this file
	// actually invoking it), so this should be true in this environment.
	if !GitAvailable() {
		t.Skip("git not on PATH in this environment")
	}
}

// -------------------------------------------------------------- zoxide ----

// zoxideIsolatedEnv points zoxide at a scratch data dir under t.TempDir()
// so tests never touch the real user's zoxide database (CLAUDE.md: tests
// isolate state, never write to the real home/config dir).
func zoxideIsolatedEnv(t *testing.T) {
	t.Helper()
	if !ZoxideAvailable() {
		t.Skip("zoxide not on PATH in this environment")
	}
	t.Setenv("_ZO_DATA_DIR", t.TempDir())
}

func TestZoxideAddAndList_PathWithSpaces(t *testing.T) {
	zoxideIsolatedEnv(t)
	dir := filepath.Join(t.TempDir(), "has space", "dir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ZoxideAdd(ctx, dir)

	list := ZoxideList(ctx)
	found := false
	for _, p := range list {
		if p == dir {
			found = true
		}
	}
	if !found {
		t.Errorf("ZoxideList() = %v, want it to contain %q after ZoxideAdd", list, dir)
	}
}

func TestZoxideAdd_MissingBinaryIsSilent(t *testing.T) {
	// Point PATH somewhere with no zoxide at all; ZoxideAdd must not
	// panic or block — it's the same silent-no-op contract as bash's
	// `command -v zoxide && zoxide add ... || true`.
	t.Setenv("PATH", t.TempDir())
	ZoxideAdd(context.Background(), "/whatever")
}

// --------------------------------------------------------- worktree/gh ----

func TestParseWorktreeRef_FullURL(t *testing.T) {
	ref, err := ParseWorktreeRef("https://github.com/cyperx84/herdr-sesh-bro/issues/409")
	if err != nil {
		t.Fatalf("ParseWorktreeRef error: %v", err)
	}
	want := WorktreeRef{Owner: "cyperx84", Repo: "herdr-sesh-bro", Num: "409"}
	if ref != want {
		t.Errorf("ParseWorktreeRef = %+v, want %+v", ref, want)
	}
}

func TestParseWorktreeRef_PullURL(t *testing.T) {
	ref, err := ParseWorktreeRef("https://github.com/o/r/pull/7")
	if err != nil {
		t.Fatalf("ParseWorktreeRef error: %v", err)
	}
	if ref.Owner != "o" || ref.Repo != "r" || ref.Num != "7" {
		t.Errorf("ParseWorktreeRef(pull) = %+v", ref)
	}
}

func TestParseWorktreeRef_UnanchoredAtEnd(t *testing.T) {
	// Both patterns are deliberately unanchored at the end — bash's =~
	// has no trailing $ either. Trailing garbage after the captured
	// number must still be accepted (BEHAVIOUR.md §2.7).
	ref, err := ParseWorktreeRef("https://github.com/o/r/issues/409/comments")
	if err != nil {
		t.Fatalf("unexpected error for unanchored URL: %v", err)
	}
	if ref.Num != "409" {
		t.Errorf("Num = %q, want 409", ref.Num)
	}

	ref2, err := ParseWorktreeRef("owner/repo#409x")
	if err != nil {
		t.Fatalf("unexpected error for unanchored short form: %v", err)
	}
	if ref2.Num != "409" {
		t.Errorf("Num = %q, want 409", ref2.Num)
	}
}

func TestParseWorktreeRef_ShortForm(t *testing.T) {
	ref, err := ParseWorktreeRef("cyperx84/herdr-sesh-bro#12")
	if err != nil {
		t.Fatalf("ParseWorktreeRef error: %v", err)
	}
	want := WorktreeRef{Owner: "cyperx84", Repo: "herdr-sesh-bro", Num: "12"}
	if ref != want {
		t.Errorf("ParseWorktreeRef = %+v, want %+v", ref, want)
	}
}

func TestParseWorktreeRef_Unrecognized(t *testing.T) {
	_, err := ParseWorktreeRef("not a url at all")
	if err == nil {
		t.Fatal("ParseWorktreeRef on garbage returned nil error")
	}
	want := "sesh-bro worktree: unrecognized URL not a url at all"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, ErrUnrecognizedWorktreeRef) {
		t.Error("error does not wrap ErrUnrecognizedWorktreeRef")
	}
}

func TestTitleCachePath(t *testing.T) {
	got := TitleCachePath("/cache", WorktreeRef{Owner: "o", Repo: "r", Num: "9"})
	want := filepath.Join("/cache", "gh-title-o-r-9")
	if got != want {
		t.Errorf("TitleCachePath = %q, want %q", got, want)
	}
}

func TestResolveIssueTitle_CacheHit_SkipsGh(t *testing.T) {
	dir := t.TempDir()
	ref := WorktreeRef{Owner: "o", Repo: "r", Num: "9"}
	cacheFile := TitleCachePath(dir, ref)
	if err := os.WriteFile(cacheFile, []byte("Cached Title"), 0o644); err != nil {
		t.Fatal(err)
	}
	// ghBin points at a nonexistent binary — if this function actually
	// tried to run gh, it would fail loudly. A cache hit must never
	// touch it.
	title, err := ResolveIssueTitle(context.Background(), dir, ref, time.Now(), "/no/such/gh")
	if err != nil {
		t.Fatalf("ResolveIssueTitle error on a cache hit: %v", err)
	}
	if title != "Cached Title" {
		t.Errorf("title = %q, want %q", title, "Cached Title")
	}
}

func TestResolveIssueTitle_StaleCacheIsIgnored(t *testing.T) {
	dir := t.TempDir()
	ref := WorktreeRef{Owner: "o", Repo: "r", Num: "9"}
	cacheFile := TitleCachePath(dir, ref)
	if err := os.WriteFile(cacheFile, []byte("Old Title"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(cacheFile, old, old); err != nil {
		t.Fatal(err)
	}
	// gh missing -> "" with no error, once the stale cache is correctly
	// ignored (a bug here would instead return "Old Title").
	title, err := ResolveIssueTitle(context.Background(), dir, ref, time.Now(), "/no/such/gh")
	if err != nil {
		t.Fatalf("ResolveIssueTitle error: %v", err)
	}
	if title != "" {
		t.Errorf("title = %q, want empty (stale cache must not be used, gh unavailable)", title)
	}
}

func TestResolveIssueTitle_GhMissing_EmptyNoError(t *testing.T) {
	dir := t.TempDir()
	ref := WorktreeRef{Owner: "o", Repo: "r", Num: "9"}
	title, err := ResolveIssueTitle(context.Background(), dir, ref, time.Now(), "/no/such/gh")
	if err != nil {
		t.Fatalf("ResolveIssueTitle error = %v, want nil (gh missing is not a failure)", err)
	}
	if title != "" {
		t.Errorf("title = %q, want empty", title)
	}
}

// fakeGhScript writes an executable shell script standing in for gh: it
// echoes different titles for `issue view` vs `pr view` so tests can
// verify ordering (issue tried first, even for a /pull/ URL) and the
// fallback path (issue view failing, pr view succeeding).
func fakeGhScript(t *testing.T, issueTitle, prTitle string, issueFails bool) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gh")
	var body strings.Builder
	body.WriteString("#!/bin/sh\n")
	body.WriteString("kind=\"$1\"\n")
	body.WriteString("if [ \"$kind\" = issue ]; then\n")
	if issueFails {
		body.WriteString("  exit 1\n")
	} else {
		body.WriteString("  printf '%s' " + shellQuote(issueTitle) + "\n")
	}
	body.WriteString("elif [ \"$kind\" = pr ]; then\n")
	body.WriteString("  printf '%s' " + shellQuote(prTitle) + "\n")
	body.WriteString("fi\n")
	if err := os.WriteFile(path, []byte(body.String()), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func TestResolveIssueTitle_IssueViewTriedFirst(t *testing.T) {
	dir := t.TempDir()
	ref := WorktreeRef{Owner: "o", Repo: "r", Num: "5"}
	gh := fakeGhScript(t, "Issue Title", "PR Title", false)

	title, err := ResolveIssueTitle(context.Background(), dir, ref, time.Now(), gh)
	if err != nil {
		t.Fatalf("ResolveIssueTitle error: %v", err)
	}
	if title != "Issue Title" {
		t.Errorf("title = %q, want %q (issue view must be tried before pr view, even for a /pull/ URL)", title, "Issue Title")
	}

	// And it must have been cached, with NO trailing newline.
	raw, err := os.ReadFile(TitleCachePath(dir, ref))
	if err != nil {
		t.Fatalf("reading cache file: %v", err)
	}
	if string(raw) != "Issue Title" {
		t.Errorf("cache file contents = %q, want %q (no trailing newline)", raw, "Issue Title")
	}
}

func TestResolveIssueTitle_FallsBackToPrView(t *testing.T) {
	dir := t.TempDir()
	ref := WorktreeRef{Owner: "o", Repo: "r", Num: "6"}
	gh := fakeGhScript(t, "", "PR Title", true)

	title, err := ResolveIssueTitle(context.Background(), dir, ref, time.Now(), gh)
	if err != nil {
		t.Fatalf("ResolveIssueTitle error: %v", err)
	}
	if title != "PR Title" {
		t.Errorf("title = %q, want %q", title, "PR Title")
	}
}

func TestWorktreeCWD(t *testing.T) {
	home := t.TempDir()
	// No $home/github yet: falls back to home.
	if got := WorktreeCWD(home, "herdr-sesh-bro"); got != home {
		t.Errorf("WorktreeCWD with no github dir = %q, want %q", got, home)
	}

	if err := os.MkdirAll(filepath.Join(home, "github"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "github", "herdr-sesh-bro")
	if got := WorktreeCWD(home, "herdr-sesh-bro"); got != want {
		t.Errorf("WorktreeCWD with a github dir = %q, want %q (repo subdir need not exist)", got, want)
	}
	// The repo subdirectory itself must NOT be required to exist — this
	// is BEHAVIOUR.md §2.7's documented bug ([code]: only the parent is
	// checked), reproduced not fixed.
	if _, err := os.Stat(want); err == nil {
		t.Fatal("test setup error: the repo subdir should not exist yet")
	}
}

// WorktreeCWD with a plain FILE (not a directory) named "github" must also
// fall back to home — bash's `[[ -d ... ]]` is a directory test, not mere
// existence.
func TestWorktreeCWD_GithubIsAFile(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "github"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := WorktreeCWD(home, "repo"); got != home {
		t.Errorf("WorktreeCWD with github as a file = %q, want %q", got, home)
	}
}

func TestWorktreeLabel(t *testing.T) {
	if got := WorktreeLabel("owner", "repo", "409", ""); got != "owner/repo#409" {
		t.Errorf("WorktreeLabel with no title = %q, want %q", got, "owner/repo#409")
	}
	want := "owner/repo#409 — Fix the thing" // EM DASH, U+2014
	if got := WorktreeLabel("owner", "repo", "409", "Fix the thing"); got != want {
		t.Errorf("WorktreeLabel with a title = %q, want %q", got, want)
	}
}

// The failure this guard exists for: a machine with no ~/github whose owner has
// run `git init` in their home directory (dotfiles-in-$HOME, yadm-style).
// WorktreeCWD then falls back to $HOME, which IS a git repo — so an unverified
// guess would branch and materialise a worktree inside their dotfiles.
func TestRepoCheckoutRefusesAGitInitHomeDirectory(t *testing.T) {
	home := t.TempDir()
	gitInit(t, home) // no remote, directory name is not "somerepo"

	got, ok := RepoCheckout(context.Background(), "", home, "owner", "somerepo")
	if ok {
		t.Errorf("accepted %q as a checkout of owner/somerepo — a worktree would be created in the user's dotfiles (got %q)", home, got)
	}
}

// A plain directory that is not a repository at all must also be refused,
// rather than handed to worktree.create to interpret.
func TestRepoCheckoutRefusesANonRepo(t *testing.T) {
	dir := t.TempDir()
	if _, ok := RepoCheckout(context.Background(), "", dir, "owner", "repo"); ok {
		t.Error("accepted a directory that is not a git repository")
	}
	if _, ok := RepoCheckout(context.Background(), "", filepath.Join(dir, "nope"), "owner", "repo"); ok {
		t.Error("accepted a path that does not exist")
	}
	if _, ok := RepoCheckout(context.Background(), "", "", "owner", "repo"); ok {
		t.Error("accepted an empty candidate path")
	}
}

// The ordinary case still works: a checkout whose directory name matches, with
// no remote configured.
func TestRepoCheckoutAcceptsMatchingDirectoryName(t *testing.T) {
	parent := t.TempDir()
	repo := filepath.Join(parent, "myrepo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, repo)

	got, ok := RepoCheckout(context.Background(), "", repo, "owner", "myrepo")
	if !ok {
		t.Fatal("refused a checkout whose directory name matches the repo")
	}
	if filepath.Base(got) != "myrepo" {
		t.Errorf("resolved root = %q, want the repo root", got)
	}
}

// The remote is authoritative, so a renamed directory still resolves — that is
// the case a directory-name check alone would get wrong.
func TestRepoCheckoutAcceptsRenamedDirectoryWithMatchingRemote(t *testing.T) {
	parent := t.TempDir()
	repo := filepath.Join(parent, "renamed-locally")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, repo)
	run(t, repo, "git", "remote", "add", "origin", "git@github.com:owner/realname.git")

	if _, ok := RepoCheckout(context.Background(), "", repo, "owner", "realname"); !ok {
		t.Error("refused a checkout whose remote matches, only its directory was renamed")
	}
	// And it must not accept a DIFFERENT repo that happens to sit there.
	if _, ok := RepoCheckout(context.Background(), "", repo, "owner", "somethingelse"); ok {
		t.Error("accepted a checkout whose remote points at a different repo")
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "git", "init", "-q")
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
