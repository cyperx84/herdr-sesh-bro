// The pane-list file cache bash's pane_list() implements (sesh-bro:107-117,
// BEHAVIOUR.md §7.3). No package in internal/ claims it — herdrx's own doc
// comment flags it explicitly as unowned — because it sits between "talk to
// herdr" (internal/herdrx) and "CLI process lifecycle" (this command layer),
// and its freshness test (S4, BEHAVIOUR.md §9) is command-line-tool
// behaviour (`find -mmin`), not herdr protocol behaviour.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	herdr "github.com/cyperx84/herdr-api"

	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx"
)

// failedPaneListMsg is pane_list()'s own failure message (sesh-bro:114,
// BEHAVIOUR.md §7.3, Appendix B). It is invariant across every call site —
// what differs is whether a given caller prints it at all (list and
// connect-dir suppress it via `2>/dev/null`; root does not, BEHAVIOUR.md
// §2.6) — so paneList itself never writes to stderr; callers that want the
// message print this constant themselves.
const failedPaneListMsg = "sesh-bro: herdr pane list failed"

// paneCachePath reproduces the cache file location (sesh-bro:108,
// BEHAVIOUR.md §7.3): $SESH_BRO_PANE_CACHE verbatim if set, else
// "$TMPDIR-or-/tmp/sesh-bro-<uid>-panes.json". The UID is bash's own $UID
// builtin (the calling process's real uid), not an environment variable —
// os.Getuid() is the direct Go equivalent, not a getenv("UID") lookup.
func paneCachePath(getenv func(string) string) string {
	if v := getenv("SESH_BRO_PANE_CACHE"); v != "" {
		return v
	}
	tmp := getenv("TMPDIR")
	if tmp == "" {
		tmp = "/tmp"
	}
	return filepath.Join(tmp, fmt.Sprintf("sesh-bro-%d-panes.json", os.Getuid()))
}

// paneCacheEnvelope mirrors the herdr CLI's own JSON-RPC response shape
// (`{"result":{"panes":[...]}}`) rather than a bare array. bash's cache file
// literally IS that raw CLI stdout (sesh-bro:115: `printf '%s\n' "$out"
// >"$cache"`); writing the same envelope here means a cache file left behind
// by an installed bash sesh-bro (same path, same $SESH_BRO_PANE_CACHE
// convention) still parses cleanly across an in-place upgrade to this
// binary, instead of being silently discarded as unparseable on the first
// run after the switch.
type paneCacheEnvelope struct {
	Result struct {
		Panes []herdr.Pane `json:"panes"`
	} `json:"result"`
}

// cacheTTLBucket reproduces `find -mmin <ttl>`'s truncated-minute bucket
// test against age (BEHAVIOUR.md §9 S4), for whichever sign ttl carries:
//
//   - bare N ("2"):  true when the truncated integer minute count of age
//     equals N exactly — this is what makes the default cache "almost never
//     hit" (S4): a cache written 0-119s ago misses, 120-179s hits, 180s+
//     misses again.
//   - "-N":          true when the truncated minute count is LESS than N
//     ("younger than N minutes" — the semantics an unsigned reading of the
//     bash would have assumed, and the one bug-free callers should use).
//   - "+N":          true when the truncated minute count is GREATER than N.
//
// ttl must already have passed config.CacheTTLValid's syntax check
// (cacheTTLBucket does not itself validate — see readPaneCache).
func cacheTTLBucket(ttl string, age time.Duration) bool {
	sign := byte(0)
	numStr := ttl
	if len(ttl) > 0 && (ttl[0] == '+' || ttl[0] == '-') {
		sign = ttl[0]
		numStr = ttl[1:]
	}
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return false
	}
	ageMin := int(age / time.Minute)
	switch sign {
	case '-':
		return ageMin < n
	case '+':
		return ageMin > n
	default:
		return ageMin == n
	}
}

// readPaneCache reports the cached panes and whether they are fresh enough
// to use, reproducing pane_list()'s cache-hit branch (sesh-bro:109-112):
// the file must be non-empty (`[[ -s $cache ]]`) AND pass the `find -mmin`
// bucket test (cacheTTLBucket). ttlValid is config.Config.CacheTTLValid():
// a syntactically invalid $SESH_BRO_CACHE_TTL makes `find` itself fail with
// its stderr suppressed, so the cache misses PERMANENTLY with no
// diagnostic — reproduced here as an unconditional false, never reaching
// cacheTTLBucket at all.
func readPaneCache(path, ttl string, ttlValid bool, now time.Time) ([]herdr.Pane, bool) {
	if !ttlValid {
		return nil, false
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		return nil, false
	}
	if !cacheTTLBucket(ttl, now.Sub(info.ModTime())) {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var env paneCacheEnvelope
	// A cache file this process cannot even parse (corrupted, or written by
	// something else entirely) is a miss, not a fatal error — bash's `cat
	// "$cache"` would dump the garbage bytes straight into the downstream jq
	// pipeline, which is not a distinction worth reproducing: no writer this
	// binary controls ever produces unparseable JSON here.
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, false
	}
	return env.Result.Panes, true
}

// writePaneCache reproduces sesh-bro:116 (`printf '%s\n' "$out" >"$cache"`):
// an unconditional, non-atomic overwrite with default (non-restrictive)
// permissions — concurrent pickers can interleave writes exactly as they
// can in bash, and that is left as-is rather than "fixed" with a
// write-then-rename, per the port's own prime directive.
func writePaneCache(path string, panes []herdr.Pane) {
	var env paneCacheEnvelope
	env.Result.Panes = panes
	data, err := json.Marshal(env)
	if err != nil {
		return
	}
	data = append(data, '\n')
	_ = os.WriteFile(path, data, 0o644)
}

// paneList reproduces pane_list() end to end (sesh-bro:107-117): a cache hit
// returns cached panes with a nil error; a miss calls ListPanes(ctx, "") —
// itself subject to openErr exactly like every other ungated herdr call in
// this port (herdrcalls.go) — and, on success, writes through to the cache
// before returning. A failed fetch returns (nil, err) with NOTHING printed:
// failedPaneListMsg is the caller's to print or swallow, matching bash's
// per-call-site `2>/dev/null` (list, connect dir) vs. unsuppressed (root)
// split (BEHAVIOUR.md §7.3, §2.6).
func paneList(ctx context.Context, client *herdrx.Client, openErr error, path, ttl string, ttlValid bool, now time.Time) ([]herdr.Pane, error) {
	if panes, ok := readPaneCache(path, ttl, ttlValid, now); ok {
		return panes, nil
	}
	if openErr != nil {
		return nil, openErr
	}
	panes, err := client.ListPanes(ctx, "")
	if err != nil {
		return nil, err
	}
	writePaneCache(path, panes)
	return panes, nil
}
