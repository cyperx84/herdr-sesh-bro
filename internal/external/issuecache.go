package external

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// issueCacheVersion is the on-disk schema version. A file from a future
// version is ignored rather than misread, the same rule stars and attention
// follow: degrading costs the block until it is refetched, never the command.
const issueCacheVersion = 1

// IssueCacheTTL is how stale a cached issue list may be.
//
// Ten minutes, which is very long by this project's standards and right for
// this data: issues change on human timescales, and the alternative is a
// network round trip on the picker's path. It is also the number that makes
// the warm-up goroutine worth having — a picker opened repeatedly through a
// working session hits the cache every time after the first.
const IssueCacheTTL = 10 * time.Minute

type issueCacheFile struct {
	Version   int     `json:"version"`
	FetchedAt int64   `json:"fetched_unix_ms"`
	Repo      string  `json:"repo"`
	Issues    []Issue `json:"issues"`
}

// IssueCachePath is where one repository's cached issues live.
//
// Keyed by the repository ROOT PATH, not by owner/repo: the root is what
// ListIssues is given and what `gh` resolves from, so keying on anything else
// would let two checkouts of the same repository — a worktree and its parent,
// which this plugin creates routinely — disagree about which cache entry they
// own while both writing to it.
//
// The path is flattened rather than nested so the cache directory stays one
// level deep and a stale entry is obvious to anyone who looks.
func IssueCachePath(cacheDir, repoRoot string) string {
	safe := strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(strings.TrimPrefix(repoRoot, "/"))
	if safe == "" {
		safe = "root"
	}
	return filepath.Join(cacheDir, "issues-"+safe+".json")
}

// ReadIssueCache returns the cached issues if the file is fresh.
//
// Missing, unreadable, malformed, future-versioned or stale all yield ok=false
// with no error. There is nothing a caller could usefully do differently for
// any of them: every one means "ask gh".
func ReadIssueCache(path string, now time.Time) ([]Issue, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var f issueCacheFile
	if err := json.Unmarshal(b, &f); err != nil || f.Version != issueCacheVersion {
		return nil, false
	}
	if now.Sub(time.UnixMilli(f.FetchedAt)) >= IssueCacheTTL {
		return nil, false
	}
	return f.Issues, true
}

// WriteIssueCache replaces the cache file atomically.
//
// Temp-and-rename because the reader is a different, short-lived process — the
// picker warms the cache in the background while a `list` may be reading it —
// and a half-written file would be indistinguishable from a corrupt one.
//
// An empty list IS cached, deliberately. "This repo has no open issues" is a
// real answer that took a network call to learn, and not caching it would mean
// paying for that call on every single render.
func WriteIssueCache(path, repoRoot string, issues []Issue, now time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if issues == nil {
		issues = []Issue{}
	}
	b, err := json.Marshal(issueCacheFile{
		Version:   issueCacheVersion,
		FetchedAt: now.UnixMilli(),
		Repo:      repoRoot,
		Issues:    issues,
	})
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// osWriteFile is a tiny indirection so tests can write fixture cache files
// without importing os for one call.
func osWriteFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o600)
}
