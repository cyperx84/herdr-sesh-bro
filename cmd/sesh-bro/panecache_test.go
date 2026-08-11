package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	herdr "github.com/cyperx84/herdr-api"
)

// TestCacheTTLBucket_BareNIsExactBucket is S4's headline finding
// (BEHAVIOUR.md §9): bare "2" means "truncated minute count equals exactly
// 2" — NOT "less than 2 minutes old". A cache written 0-119s ago misses,
// 120-179s hits, 180s+ misses again. This is measured against Darwin 27's
// `find -mmin` bucket boundaries cited in BEHAVIOUR.md §9 S4.
func TestCacheTTLBucket_BareNIsExactBucket(t *testing.T) {
	cases := []struct {
		age  time.Duration
		want bool
	}{
		{30 * time.Second, false},
		{119 * time.Second, false},
		{120 * time.Second, true},
		{179 * time.Second, true},
		{180 * time.Second, false},
		{4 * time.Minute, false},
	}
	for _, c := range cases {
		if got := cacheTTLBucket("2", c.age); got != c.want {
			t.Errorf("cacheTTLBucket(%q, %v) = %v, want %v", "2", c.age, got, c.want)
		}
	}
}

// TestCacheTTLBucket_SignedOperands covers the "-N"/"+N" forms this port
// offers as the bug-free reading of the same config value (BEHAVIOUR.md §9
// S4's own recommendation: reproduce the bucket semantics, or offer the
// fixed one explicitly — this does both, gated on the sign the user typed).
func TestCacheTTLBucket_SignedOperands(t *testing.T) {
	if !cacheTTLBucket("-2", 90*time.Second) {
		t.Error(`cacheTTLBucket("-2", 90s) = false, want true (younger than 2m)`)
	}
	if cacheTTLBucket("-2", 150*time.Second) {
		t.Error(`cacheTTLBucket("-2", 150s) = true, want false (older than 2m)`)
	}
	// Real `find -mmin` truncates age to whole minutes BEFORE comparing —
	// per BEHAVIOUR.md §9 S4's own Darwin measurement, this is not merely
	// this port's choice, it's what the tool being reproduced actually
	// does. 150s truncates to 2 whole minutes, so "+2" (strictly greater
	// than the truncated 2) is false at 150s and needs a genuinely
	// 3-minutes-truncated age (200s) to read true.
	if !cacheTTLBucket("+2", 200*time.Second) {
		t.Error(`cacheTTLBucket("+2", 200s) = false, want true (truncates to 3m, 3 > 2)`)
	}
	if cacheTTLBucket("+2", 150*time.Second) {
		t.Error(`cacheTTLBucket("+2", 150s) = true, want false (truncates to 2m, 2 is not > 2)`)
	}
}

// TestCacheTTLBucket_NonNumericNeverMatches reproduces the "find fails,
// stderr suppressed, permanent silent miss" half of S4.
func TestCacheTTLBucket_NonNumericNeverMatches(t *testing.T) {
	if cacheTTLBucket("banana", time.Minute) {
		t.Error("non-numeric TTL matched — want permanent miss")
	}
}

// TestReadPaneCache_InvalidTTLIsPermanentMiss reproduces
// config.CacheTTLValid's role: a syntactically invalid TTL means the cache
// is NEVER read, regardless of how fresh the file actually is.
func TestReadPaneCache_InvalidTTLIsPermanentMiss(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panes.json")
	writePaneCache(path, []herdr.Pane{{ID: "w1:p1"}})
	if _, ok := readPaneCache(path, "2", false /* ttlValid */, time.Now()); ok {
		t.Fatal("readPaneCache hit despite ttlValid=false")
	}
}

// TestReadPaneCache_RoundTrip proves a fresh, valid-TTL cache round-trips
// through the CLI-envelope shape (BEHAVIOUR.md §7.3: bash's cache file IS
// the raw `herdr pane list` stdout).
func TestReadPaneCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panes.json")
	want := []herdr.Pane{{ID: "w1:p1", WorkspaceID: "w1", CWD: "/a"}}
	writePaneCache(path, want)

	// mtime is "now" (just written) — age is ~0s, which misses bare "2"'s
	// bucket (per TestCacheTTLBucket_BareNIsExactBucket) but hits "-2"'s
	// (younger-than semantics).
	got, ok := readPaneCache(path, "-2", true, time.Now())
	if !ok {
		t.Fatal("expected a cache hit")
	}
	if len(got) != 1 || got[0].ID != "w1:p1" || got[0].CWD != "/a" {
		t.Fatalf("readPaneCache() = %+v, want %+v", got, want)
	}
}

// TestReadPaneCache_EmptyFileIsMiss reproduces `[[ -s $cache ]]`
// (sesh-bro:109): a zero-byte cache file is always a miss.
func TestReadPaneCache_EmptyFileIsMiss(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panes.json")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := readPaneCache(path, "-100", true, time.Now()); ok {
		t.Fatal("empty file produced a cache hit")
	}
}

// TestReadPaneCache_MissingFileIsMiss covers the ordinary "never cached yet"
// case, without a stat error propagating anywhere.
func TestReadPaneCache_MissingFileIsMiss(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.json")
	if _, ok := readPaneCache(path, "-100", true, time.Now()); ok {
		t.Fatal("missing file produced a cache hit")
	}
}

// TestReadPaneCache_UnparseableIsMissNotPanic covers a corrupted cache file:
// a miss, not a crash — see readPaneCache's own doc comment on why this is
// a deliberate, low-risk divergence from bash's "dump the garbage into jq"
// behaviour.
func TestReadPaneCache_UnparseableIsMissNotPanic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panes.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := readPaneCache(path, "-100", true, time.Now()); ok {
		t.Fatal("unparseable cache produced a hit")
	}
}

// TestWritePaneCache_EnvelopeShape proves the on-disk shape is the CLI's
// own `{"result":{"panes":[...]}}` envelope, not a bare array — so a cache
// file an installed bash sesh-bro left behind still parses across an
// in-place upgrade to this binary.
func TestWritePaneCache_EnvelopeShape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panes.json")
	writePaneCache(path, []herdr.Pane{{ID: "w1:p1"}})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Result struct {
			Panes []struct {
				ID string `json:"pane_id"`
			} `json:"panes"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("cache file is not the expected envelope shape: %v (%s)", err, data)
	}
	if len(env.Result.Panes) != 1 || env.Result.Panes[0].ID != "w1:p1" {
		t.Fatalf("decoded envelope = %+v", env)
	}
	if data[len(data)-1] != '\n' {
		t.Error("cache file does not end in a newline (sesh-bro:116's printf appends one)")
	}
}

// TestPaneCachePath_OverrideWins reproduces sesh-bro:108: SESH_BRO_PANE_CACHE
// wins over the TMPDIR/UID-derived default entirely.
func TestPaneCachePath_OverrideWins(t *testing.T) {
	got := paneCachePath(fakeEnv(map[string]string{"SESH_BRO_PANE_CACHE": "/custom/path.json"}))
	if got != "/custom/path.json" {
		t.Fatalf("paneCachePath() = %q, want /custom/path.json", got)
	}
}

// TestPaneCachePath_DefaultUsesTMPDIRAndUID reproduces the default shape:
// "$TMPDIR-or-/tmp/sesh-bro-<uid>-panes.json".
func TestPaneCachePath_DefaultUsesTMPDIRAndUID(t *testing.T) {
	got := paneCachePath(fakeEnv(map[string]string{"TMPDIR": "/scratch"}))
	want := filepath.Join("/scratch", "sesh-bro-"+strconv.Itoa(os.Getuid())+"-panes.json")
	if got != want {
		t.Fatalf("paneCachePath() = %q, want %q", got, want)
	}
}
