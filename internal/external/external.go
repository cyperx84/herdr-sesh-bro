// Package external is sesh-bro's bridge to everything that is neither the
// herdr socket (internal/herdrx) nor fzf (internal/picker): the herdr
// binary/daemon dependency checks bash's check_deps performs, and the three
// soft external-tool integrations — git, zoxide, gh — that back git-branch
// enrichment (BEHAVIOUR.md §2.2.8), `root`'s git-toplevel lookup (§2.6), the
// zoxide directory source (§2.2.6) and `create`/`connect dir`'s zoxide-add
// side effect (§2.3, §2.4), and the `worktree` command's GitHub issue/PR
// title resolution (§2.7).
//
// Every exec.Command call in this package takes its arguments as a Go
// argv slice, never through a shell — this is what makes the two bugs the
// bash needed two separate fixes for (paths with spaces breaking word
// splitting, symlinked installs breaking relative lookups) structurally
// unreproducible here rather than merely patched: there is no shell in the
// loop to word-split or glob-expand a path in the first place.
//
// What this package deliberately does NOT own:
//
//   - The jq dependency check (require_jq, BEHAVIOUR.md §7.1). jq
//     disappears entirely in this port — 22 subprocess spawns become
//     in-process JSON decoding via herdr-api's typed responses — so there
//     is nothing left to check. Appendix B's "sesh-bro: jq is required
//     (install with your package manager)" message has no Go equivalent
//     and is not reproduced anywhere in this port. This is the one
//     Appendix B message that cannot survive; flag it, don't fake it.
//   - The herdr daemon reachability probe's actual call (herdrx.Client.Alive
//     wraps `workspace list` — BEHAVIOUR.md §6 note #13, "moot for a
//     socket-dialing binary"). CheckDeps below needs that probe but does
//     not perform it: it takes a narrow Aliver interface instead of
//     importing internal/herdrx, so the two packages stay decoupled.
//   - fzf ("sesh-bro: fzf is required") — internal/picker already owns
//     that exact message (picker.Run).
//   - Row/preview formatting, herdr-api calls, WorkspaceForIssueNumber's
//     existing-workspace label match, and every stdout success message
//     ("sesh-bro: focused workspace <id>", "sesh-bro: created workspace
//     for <owner>/<repo>#<num>") — internal/herdrx and the command layer
//     that composes it with this package.
//   - eza/bat (`preview dir`'s listing/README rendering) and the pane-list
//     file cache (§7.3, the TTL bucket bash's fzf reload binds rely on).
//     Neither is named in this package's brief and neither has an owner
//     yet as far as this file's author could tell — flag loudly for
//     whoever claims `list`'s reload path and `preview dir`.
package external

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ---------------------------------------------------------------- deps ----

// HerdrNotFoundError mirrors require_herdr's exact failure text
// (sesh-bro:74-79, BEHAVIOUR.md §7.1, Appendix B). Bin is the resolved
// $HERDR value (HERDR_BIN_PATH or the literal "herdr") so the message
// names exactly what bash's `command -v "$HERDR"` looked for.
//
// The herdr CLI remains a real dependency of this port for exactly one
// thing: `open` shells out to `herdr plugin pane open` because
// plugin.pane.open has no herdr-api method (BEHAVIOUR.md §6 note #12,
// "do not silently change what open does"). RequireHerdr exists for that
// call site and for check_deps/list's inline gate, which check the binary
// before ever dialing the socket, exactly as bash does.
type HerdrNotFoundError struct{ Bin string }

func (e *HerdrNotFoundError) Error() string {
	return fmt.Sprintf("sesh-bro: herdr binary not found (HERDR_BIN_PATH=%s)", e.Bin)
}

// DaemonNotRespondingError mirrors herdr_ok's failure. Bash prints two
// different texts for this same condition depending on the call site
// (BEHAVIOUR.md §7.1): `startup`'s check_deps appends
// " (is 'herdr' running?)"; `list`'s inline gate does not. Suffix carries
// that difference; CheckDeps and CheckListDeps below set it correctly so
// callers never have to remember which literal string belongs where.
type DaemonNotRespondingError struct{ Suffix string }

func (e *DaemonNotRespondingError) Error() string {
	return "sesh-bro: herdr daemon is not responding" + e.Suffix
}

// daemonSuffixStartup is check_deps' message suffix (sesh-bro:98,
// BEHAVIOUR.md §7.1) — startup only.
const daemonSuffixStartup = " (is 'herdr' running?)"

// RequireHerdr reproduces require_herdr (sesh-bro:74-79): bin must resolve
// via PATH lookup, exactly like bash's `command -v "$HERDR"` — which also
// succeeds for an absolute path to an executable file (BEHAVIOUR.md §7.1's
// own note; exec.LookPath has the identical property for any name
// containing a path separator). A nil return means the binary was found;
// it says nothing about the daemon behind it.
func RequireHerdr(bin string) error {
	if _, err := exec.LookPath(bin); err != nil {
		return &HerdrNotFoundError{Bin: bin}
	}
	return nil
}

// Aliver is the one herdr-daemon fact CheckDeps/CheckListDeps need: "does
// the daemon respond right now". It is defined here, narrowly, rather than
// importing internal/herdrx's Client — herdrx.Client.Alive already
// satisfies this interface (BEHAVIOUR.md §6 note #13: it wraps `workspace
// list`, exactly bash's herdr_ok), so callers pass one straight through
// with no adapter needed, and this package never has to know how the
// daemon is actually reached.
type Aliver interface {
	Alive(ctx context.Context) bool
}

// CheckDeps reproduces check_deps (sesh-bro:94-101, BEHAVIOUR.md §7.1):
// herdr binary present, then daemon responds — in that order, short-
// circuiting on the first failure, exactly like bash's `||`-chained
// `return 1`s. This is `startup`'s gate; note it uses the LONGER daemon
// message (daemonSuffixStartup). There is no jq step — see the package doc
// comment for why that's a deliberate omission, not an oversight.
func CheckDeps(ctx context.Context, bin string, alive Aliver) error {
	if err := RequireHerdr(bin); err != nil {
		return err
	}
	if !alive.Alive(ctx) {
		return &DaemonNotRespondingError{Suffix: daemonSuffixStartup}
	}
	return nil
}

// CheckListDeps reproduces `list`'s inline dependency gate (sesh-bro:148-150,
// BEHAVIOUR.md §2.2.2): the same two checks as CheckDeps, in the same
// order, but with the SHORTER daemon message — BEHAVIOUR.md is explicit
// that "this is not check_deps... both texts must be preserved". There is
// no jq step here either, for the same reason noted in CheckDeps.
func CheckListDeps(ctx context.Context, bin string, alive Aliver) error {
	if err := RequireHerdr(bin); err != nil {
		return err
	}
	if !alive.Alive(ctx) {
		return &DaemonNotRespondingError{}
	}
	return nil
}

// ----------------------------------------------------------------- git ----

// GitAvailable reports whether git is on PATH — the gate BEHAVIOUR.md
// §2.2.8 and §2.6 both apply (`command -v git`) before running any git
// integration below. Callers check this once per operation, matching
// bash's own single `command -v git` per call site rather than per
// workspace.
func GitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// GitStatus is one workspace's branch-enrichment result: the branch name
// sesh-bro's list rows append via render.GitSuffix, and whether the
// working tree is dirty (BEHAVIOUR.md §2.2.8, §4.2's "git suffix").
type GitStatus struct {
	Branch string
	Dirty  bool
}

// branchHeaderRe extracts the branch name from a `git status --porcelain
// --branch` header line, reproducing bash's
// `sed -n 's/^## \([^.]*\).*/\1/p'` (sesh-bro:245, BEHAVIOUR.md §2.2.8).
//
// [^.]* is greedy over non-dot characters and there is nothing after it in
// this pattern requiring backtracking, so — exactly like the bash regex —
// it captures everything after "## " up to (not including) the FIRST '.'
// in the line, or the whole remainder if there is no '.'. This is S7,
// load-bearing and reproduced deliberately: "## release/v1.2...origin/..."
// yields "release/v1", not "release/v1.2". Fixing it would be a different
// tool.
var branchHeaderRe = regexp.MustCompile(`^## ([^.]*)`)

// ParseGitStatusBranch extracts (branch, dirty) from the combined output of
// a single `git status --porcelain --branch` call.
//
// Bash makes TWO git calls per workspace: `status --porcelain --branch |
// head -1` for the branch header, then a second, separate `status
// --porcelain` (no --branch) purely to test for any output at all
// (dirtiness). BEHAVIOUR.md §2.2.8 notes explicitly that "the first call's
// output already contains the dirty information; the code does not use
// it... a Go port reproducing the observable result only needs the branch
// name and a boolean" — and the task brief for this port confirms
// "one call per workspace". This function is that collapse: line 1 of the
// SAME output is the branch header; every line after it is exactly what
// bare `--porcelain` would have printed (file status entries), so "more
// than one line" is dirtiness by construction, not an approximation.
//
// An empty/unparseable header (branch=="") signals "not a git repo, or git
// produced no header" — the caller should treat that as no enrichment,
// matching bash's `[[ -n $branch ]] && gitmap+=...` guard.
func ParseGitStatusBranch(output string) (branch string, dirty bool) {
	lines := strings.Split(output, "\n")
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return "", false
	}
	if m := branchHeaderRe.FindStringSubmatch(lines[0]); m != nil {
		branch = m[1]
	}
	dirty = len(lines) > 1
	return branch, dirty
}

// runGitStatusBranch runs `git -C <cwd> --no-optional-locks status
// --porcelain --branch` and returns its raw stdout. --no-optional-locks on
// both original bash calls avoids fighting an editor holding the index
// lock (BEHAVIOUR.md §2.2.8); a failed exec (not a repo, git missing, cwd
// gone) yields ("", false) — bash swallows this identically via
// `2>/dev/null || true`.
//
// gitBin overrides the git binary invoked; empty means "git" (PATH
// lookup). cwd is passed as a single argv element via -C, never
// interpolated into a shell string, so a cwd containing spaces or shell
// metacharacters needs no quoting here — this is the structural fix for
// the paths-with-spaces class of bug the bash needed patching for.
func runGitStatusBranch(ctx context.Context, gitBin, cwd string) (string, bool) {
	if gitBin == "" {
		gitBin = "git"
	}
	out, err := exec.CommandContext(ctx, gitBin, "-C", cwd, "--no-optional-locks",
		"status", "--porcelain", "--branch").Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

// WorkspaceCWDs picks one representative cwd per distinct workspace id from
// panes, reproducing the jq pipeline `group_by(.workspace_id) |
// map({ws: .[0].workspace_id, cwd: .[0].cwd})` (BEHAVIOUR.md §2.2.8).
//
// jq's group_by sorts by the key using a stable sort (row.go in
// internal/herdrx documents the same fact about sort_by, verified live),
// so for any given workspace_id the group's first element after sorting is
// exactly that workspace_id's FIRST occurrence in panes' original order —
// group_by never reorders panes that share a key. This function exploits
// that equivalence directly: a single forward pass remembering the first
// pane seen per workspace_id, no sort required.
//
// SUBTLE BUG, REPRODUCED NOT FIXED: the representative is picked BEFORE
// bash's emptiness filter runs (that filter — `[[ -z $w || -z $targetcwd
// ]] && continue`, sesh-bro:239-249 — applies to the (ws, cwd) PAIR
// group_by already chose, not to individual panes during selection). So if
// a workspace's first-listed pane happens to have an empty cwd, that
// workspace gets NO git enrichment even if a later pane in the same
// workspace has a perfectly good cwd — the empty-cwd pane's presence
// "poisons" the whole workspace's representative slot. This function
// reproduces exactly that: it commits to the first pane per workspace id
// unconditionally, and only the FINAL map is filtered for empty ws/cwd.
func WorkspaceCWDs(panes []Pane) map[string]string {
	firstCWD := make(map[string]string)
	seen := make(map[string]bool, len(panes))
	for _, p := range panes {
		if seen[p.WorkspaceID] {
			continue
		}
		seen[p.WorkspaceID] = true
		firstCWD[p.WorkspaceID] = p.CWD
	}
	out := make(map[string]string, len(firstCWD))
	for ws, cwd := range firstCWD {
		if ws == "" || cwd == "" {
			continue
		}
		out[ws] = cwd
	}
	return out
}

// Pane is the narrow slice of herdr-api's Pane this package needs to pick
// git-enrichment representatives — just enough that this package does not
// need to import herdr-api itself for a single struct shape. A caller
// holding []herdr.Pane can build []external.Pane with one loop, or (since
// the two struct shapes have identical field names and types for these
// three fields) pass herdr.Pane values through unchanged wherever Go's
// structural typing allows it.
type Pane struct {
	WorkspaceID string
	CWD         string
}

// GitEnrichment runs one git call per distinct workspace (via
// WorkspaceCWDs), returning branch+dirty for every workspace whose
// representative cwd is a git repository (BEHAVIOUR.md §2.2.8). A
// workspace whose git call fails (not a repo, cwd gone, git itself
// missing) or whose output carries no parseable branch gets no entry —
// mirroring bash's `[[ -n $branch ]] && gitmap+=...` guard.
//
// Callers must check GitAvailable() themselves before calling this — that
// mirrors bash's single `command -v git` gate for the WHOLE enrichment
// pass (BEHAVIOUR.md §2.2.8's gate list), not a per-workspace check. This
// function does not check it itself so that a caller who already knows git
// is present (e.g. having just checked for a sibling reason) doesn't pay
// for a redundant PATH lookup.
func GitEnrichment(ctx context.Context, gitBin string, wsCWD map[string]string) map[string]GitStatus {
	out := make(map[string]GitStatus, len(wsCWD))
	for ws, cwd := range wsCWD {
		output, ok := runGitStatusBranch(ctx, gitBin, cwd)
		if !ok {
			continue
		}
		branch, dirty := ParseGitStatusBranch(output)
		if branch == "" {
			continue
		}
		out[ws] = GitStatus{Branch: branch, Dirty: dirty}
	}
	return out
}

// GitRoot runs `git -C <cwd> rev-parse --show-toplevel`, reproducing
// `root`'s repo-detection call (sesh-bro:374, BEHAVIOUR.md §2.6). ok is
// false for anything git itself treats as failure (not a repo, cwd gone,
// git missing) — bash swallows all of these identically via `2>/dev/null
// || true` and then tests `[[ -n $root ]]`; printing "sesh-bro: not in a
// git repo" on that emptiness is the caller's job (§2.6's message belongs
// to `root`'s command handler, not this package — see the package doc
// comment).
//
// gitBin overrides the git binary; empty means "git".
func GitRoot(ctx context.Context, gitBin, cwd string) (string, bool) {
	if gitBin == "" {
		gitBin = "git"
	}
	out, err := exec.CommandContext(ctx, gitBin, "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", false
	}
	root := strings.TrimRight(string(out), "\n")
	if root == "" {
		return "", false
	}
	return root, true
}

// -------------------------------------------------------------- zoxide ----

// ZoxideAvailable reports whether zoxide is on PATH — the gate every
// zoxide call site checks first (`command -v zoxide`, BEHAVIOUR.md §2.2.6,
// §2.3, §2.4).
func ZoxideAvailable() bool {
	_, err := exec.LookPath("zoxide")
	return err == nil
}

// ZoxideList runs `zoxide query --list`, returning entries in the frecency-
// descending order zoxide itself produces (BEHAVIOUR.md §2.2.6: "not
// sorted here, preserved"). Any failure — zoxide missing, non-zero exit —
// yields a nil slice, matching bash's `zoxide query --list 2>/dev/null ||
// true` (a swallowed error becomes an empty variable, which the bash's
// `while read` loop over then simply iterates zero times).
func ZoxideList(ctx context.Context) []string {
	out, err := exec.CommandContext(ctx, "zoxide", "query", "--list").Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(string(out), "\n")
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		if l != "" {
			result = append(result, l)
		}
	}
	return result
}

// ZoxideAdd runs `zoxide add <path>`, silently doing nothing if zoxide is
// unavailable or the call fails — reproducing every call site's
// `command -v zoxide >/dev/null 2>&1 && zoxide add "$path" 2>/dev/null ||
// true` pattern (BEHAVIOUR.md §2.2.6 point 3 of §2.3, §2.4: "failure
// ignored"). path is passed as a single argv element, so spaces need no
// escaping.
func ZoxideAdd(ctx context.Context, path string) {
	if !ZoxideAvailable() {
		return
	}
	_ = exec.CommandContext(ctx, "zoxide", "add", path).Run()
}

// --------------------------------------------------------- worktree/gh ----

// WorktreeRef is the parsed identity of a GitHub issue or PR — owner,
// repo, and issue/PR number as bash's two `=~` patterns capture them
// (BEHAVIOUR.md §2.7). Num is a string, not an int: bash never does
// arithmetic on it, only string interpolation (cache filenames, gh
// arguments, labels) and regex membership tests, so keeping it a string
// avoids a round-trip that could only ever lose information (e.g. a
// leading zero, though `[0-9]+` never captures one meaningfully here).
type WorktreeRef struct {
	Owner, Repo, Num string
}

// ErrUnrecognizedWorktreeRef is the sentinel UnrecognizedWorktreeRefError
// wraps — test failures against it with errors.Is when the specific URL
// doesn't matter.
var ErrUnrecognizedWorktreeRef = errors.New("external: unrecognized worktree ref")

// UnrecognizedWorktreeRefError reports that a `worktree` URL matched
// neither accepted form (BEHAVIOUR.md §2.7, Appendix B). Its Error() text
// IS the verbatim bash stderr line — not a wrapped/annotated version of
// it — because a caller's contract here is to print this error directly,
// the same way HerdrNotFoundError and DaemonNotRespondingError above do.
type UnrecognizedWorktreeRefError struct{ URL string }

func (e *UnrecognizedWorktreeRefError) Error() string {
	return fmt.Sprintf("sesh-bro worktree: unrecognized URL %s", e.URL)
}

func (e *UnrecognizedWorktreeRefError) Unwrap() error { return ErrUnrecognizedWorktreeRef }

// worktreeURLPattern and worktreeShortPattern are bash's two `=~` patterns
// verbatim (sesh-bro:396, :398, BEHAVIOUR.md §2.7). Both are DELIBERATELY
// unanchored at the end — bash's are too (only `^` appears, no `$`) — so
// "https://github.com/o/r/issues/409/comments" and "owner/repo#409x" are
// BOTH accepted, matching only the leading digits as the number. Anchoring
// these to `$` would reject inputs bash accepts; that would be a silent
// behaviour change, not a fix, and is not made here.
var (
	worktreeURLPattern   = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+)/(issues|pull)/([0-9]+)`)
	worktreeShortPattern = regexp.MustCompile(`^([^/]+)/([^/]+)#([0-9]+)`)
)

// ParseWorktreeRef parses url into a WorktreeRef, trying the full GitHub
// URL form first and the short "owner/repo#N" form second — bash's own
// if/elif order (sesh-bro:396-402, BEHAVIOUR.md §2.7). An unrecognised url
// returns ErrUnrecognizedWorktreeRef wrapped with the exact stderr text
// (Appendix B): "sesh-bro worktree: unrecognized URL <url>".
func ParseWorktreeRef(url string) (WorktreeRef, error) {
	if m := worktreeURLPattern.FindStringSubmatch(url); m != nil {
		return WorktreeRef{Owner: m[1], Repo: m[2], Num: m[4]}, nil
	}
	if m := worktreeShortPattern.FindStringSubmatch(url); m != nil {
		return WorktreeRef{Owner: m[1], Repo: m[2], Num: m[3]}, nil
	}
	return WorktreeRef{}, &UnrecognizedWorktreeRefError{URL: url}
}

// titleCacheTTL is 1440 minutes — the 24-hour freshness window `find
// "$cache_file" -mmin -1440` tests (BEHAVIOUR.md §2.7). Unlike the pane
// cache's TTL bug (S4, no minus sign — almost never a hit), this one has
// the CORRECT negative sign: "modified less than 1440 minutes ago".
const titleCacheTTL = 24 * time.Hour

// TitleCachePath builds the gh-title cache file path for ref under
// cacheDir: "<cacheDir>/gh-title-<owner>-<repo>-<num>" (sesh-bro:407).
func TitleCachePath(cacheDir string, ref WorktreeRef) string {
	return filepath.Join(cacheDir, fmt.Sprintf("gh-title-%s-%s-%s", ref.Owner, ref.Repo, ref.Num))
}

// readTitleCache reports the cached title and whether it is fresh: the
// file must be non-empty AND modified less than titleCacheTTL before now.
// now is a parameter so tests don't depend on wall-clock timing.
func readTitleCache(path string, now time.Time) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		return "", false
	}
	if now.Sub(info.ModTime()) >= titleCacheTTL {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// ghView runs `gh <kind> view <num> --repo <owner>/<repo> --json title -q
// .title` (sesh-bro:412-413) and returns its trimmed stdout, or "" on any
// failure — matching bash's `2>/dev/null` per call plus the `||`
// fallback chain this function's caller drives.
func ghView(ctx context.Context, ghBin, kind string, ref WorktreeRef) string {
	out, err := exec.CommandContext(ctx, ghBin, kind, "view", ref.Num,
		"--repo", ref.Owner+"/"+ref.Repo, "--json", "title", "-q", ".title").Output()
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(out), "\n")
}

// ResolveIssueTitle resolves ref's issue/PR title through the 24-hour
// cache under cacheDir, matching BEHAVIOUR.md §2.7's title-resolution
// section field for field:
//
//  1. mkdir -p cacheDir unconditionally (bash does this before checking the
//     cache file at all — sesh-bro:406).
//  2. A fresh cache hit (readTitleCache) short-circuits: gh is never run.
//  3. On a miss, gh is required (checked via PATH lookup, exactly like
//     every other soft-dependency gate in this package); if gh is not on
//     PATH, the result is "" with a nil error — this is bash's `command -v
//     gh` guard, not a failure (§2.7: "gh missing -> empty title, no
//     error").
//  4. `gh issue view` is tried FIRST, even for a /pull/ URL — bash's own
//     ordering (§2.7: "Note issue view is tried first even for /pull/
//     URLs"), reproduced here as documented bash behaviour, not corrected.
//     `gh pr view` is the fallback.
//  5. A non-empty resolved title is written to the cache file with NO
//     trailing newline (os.WriteFile writes exactly the bytes given) —
//     bash's `printf '%s' "$title" >"$cache_file"` has the same property,
//     and §2.7 warns explicitly that a Go port appending one would be
//     wrong: `$(cat …)` strips a trailing newline on read, masking the bug
//     in bash but not here, so this function must not introduce one.
//
// ghBin overrides the gh binary; empty means "gh". now is injectable for
// deterministic cache-freshness tests.
func ResolveIssueTitle(ctx context.Context, cacheDir string, ref WorktreeRef, now time.Time, ghBin string) (string, error) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("external: create gh-title cache dir %s: %w", cacheDir, err)
	}
	cacheFile := TitleCachePath(cacheDir, ref)
	if title, ok := readTitleCache(cacheFile, now); ok {
		return title, nil
	}

	if ghBin == "" {
		ghBin = "gh"
	}
	if _, err := exec.LookPath(ghBin); err != nil {
		return "", nil
	}

	title := ghView(ctx, ghBin, "issue", ref)
	if title == "" {
		title = ghView(ctx, ghBin, "pr", ref)
	}
	if title != "" {
		if err := os.WriteFile(cacheFile, []byte(title), 0o644); err != nil {
			return "", fmt.Errorf("external: write gh-title cache %s: %w", cacheFile, err)
		}
	}
	return title, nil
}

// WorktreeCWD picks the cwd for a newly-created worktree workspace
// (sesh-bro:430-433, BEHAVIOUR.md §2.7): "$home/github/<repo>" if
// "$home/github" exists AS A DIRECTORY, else home.
//
// BUG, REPRODUCED NOT FIXED: only the PARENT ("$home/github") is checked
// for existence — "$home/github/<repo>" itself may not exist, and herdr is
// handed a cwd that isn't there. §2.7's own [code] annotation flags this;
// fixing it (e.g. falling back further when the repo subdir is also
// missing) would be a different tool.
func WorktreeCWD(home, repo string) string {
	if info, err := os.Stat(filepath.Join(home, "github")); err == nil && info.IsDir() {
		return filepath.Join(home, "github", repo)
	}
	return home
}

// WorktreeLabel formats a newly-created worktree workspace's label
// (sesh-bro:428-429, BEHAVIOUR.md §2.7): "<num>" alone, or "<num> —
// <title>" — space, U+2014 EM DASH, space — when a non-empty title was
// resolved.
func WorktreeLabel(num, title string) string {
	if title == "" {
		return num
	}
	return num + " — " + title
}
