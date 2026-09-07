package main

import (
	"os"
	"regexp"
	"testing"
)

// scripts/build.sh's fallback path downloads
// sesh-bro-${VERSION}-${target}.tar.gz from the GitHub release named by
// VERSION. Nothing at runtime cross-checks that against herdr-plugin.toml, so
// the two drift silently: at the time this test was written the script pinned
// v0.3.0 — a tag that was never created — while the manifest said 0.4.0.
// Every toolchain-less install would have 404'd, and nothing would have said
// so until a user reported it.
//
// The manifest is the authority: it is what herdr shows, what the release
// workflow gates the tag against, and what a human bumps first. This test
// only asserts the script agrees with it.
//
// The manifest side reuses main.go's own manifestVersionRe rather than a
// second copy of the pattern, so this test reads the manifest exactly the way
// the running binary does.

// buildScriptVersionRe matches VERSION="v0.4.0". The v prefix is part of the
// tag name, so it is part of the download URL, and it is NOT part of the
// manifest version. Kept in step with the release workflow's own
// `sed -n 's/^VERSION="\(.*\)"$/\1/p'`.
var buildScriptVersionRe = regexp.MustCompile(`(?m)^VERSION="([^"]*)"`)

// Paths are relative to this package directory, which is where `go test`
// runs. Reaching up to the repo root is deliberate: the thing under test is
// the agreement between two files that live outside Go entirely.
const (
	buildScriptPath = "../../scripts/build.sh"
	manifestPath    = "../../herdr-plugin.toml"
)

func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

func TestBuildScriptVersionMatchesManifest(t *testing.T) {
	script := readRepoFile(t, buildScriptPath)
	manifest := readRepoFile(t, manifestPath)

	scriptMatch := buildScriptVersionRe.FindStringSubmatch(script)
	if scriptMatch == nil {
		t.Fatalf(`no VERSION="..." line in %s — the release pin moved or was renamed, and `+
			`the release workflow's own extraction of it will have broken too`, buildScriptPath)
	}
	manifestMatch := manifestVersionRe.FindStringSubmatch(manifest)
	if manifestMatch == nil {
		t.Fatalf(`no version = "..." line in %s`, manifestPath)
	}

	pinned, declared := scriptMatch[1], manifestMatch[1]

	// The script pins a tag name, the manifest declares a semver. They are the
	// same number with a "v" in front, and the release workflow relies on
	// exactly that relationship when it gates the tag.
	if pinned != "v"+declared {
		t.Errorf("scripts/build.sh pins VERSION=%q but herdr-plugin.toml declares version=%q\n"+
			"want VERSION=%q. A prebuilt install downloads from the release named by VERSION, so a\n"+
			"stale pin sends every machine without a Go toolchain to a 404 (or, worse, to the previous\n"+
			"release's binary). Bump both in the commit the tag will point at — see docs/RELEASING.md.",
			pinned, declared, "v"+declared)
	}
}

// The release workflow rebuilds each of these targets and asserts its SHA256
// against the matching arm below. An arm that is missing, renamed or
// half-pasted breaks that assertion at tag time, when the only remedy is to
// move a tag; catching it in the ordinary test suite is much cheaper.
func TestBuildScriptPinsEveryTarget(t *testing.T) {
	script := readRepoFile(t, buildScriptPath)

	// UNRELEASED is the deliberate placeholder for "no tag has produced this
	// asset yet"; build.sh turns it into a refusal to install, and the release
	// workflow turns it into a refusal to publish. Anything else must look
	// like a real SHA256, which is what catches a truncated or typo'd paste.
	pinRe := regexp.MustCompile(`^(?:UNRELEASED|[0-9a-f]{64})$`)

	// Must stay in step with platform_target() in the same script and with the
	// GOOS/GOARCH matrix in .github/workflows/release.yml.
	tests := []struct {
		name   string
		target string
	}{
		{"apple silicon", "darwin-arm64"},
		{"intel mac", "darwin-amd64"},
		{"linux x86-64", "linux-amd64"},
		{"linux arm64", "linux-arm64"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Matches an arm of the expected_sha256() case statement:
			//     darwin-arm64) echo "UNRELEASED" ;;
			armRe := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(tc.target) + `\)\s*echo\s*"([^"]*)"`)
			m := armRe.FindStringSubmatch(script)
			if m == nil {
				t.Fatalf("no expected_sha256() arm for %q in %s — an install on that platform would be\n"+
					"refused outright, and the release workflow's pin check would fail the release.",
					tc.target, buildScriptPath)
			}
			if !pinRe.MatchString(m[1]) {
				t.Errorf("expected_sha256() pins %q for %q, want a 64-character lowercase hex SHA256\n"+
					"or the UNRELEASED placeholder. A pin of the wrong shape can never match the artifact.",
					m[1], tc.target)
			}
		})
	}
}
