package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// fakeEnv builds a getenv closure from a map, for tests that don't need the
// real process environment.
func fakeEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// TestRun_DefaultCommandIsPicker mirrors sesh-bro:638-640: no argument at
// all dispatches to `picker`. Rather than actually running fzf, this proves
// dispatch happened by pointing HERDR_BIN_PATH at a binary that cannot
// exist and observing cmdPicker's own "herdr binary not found" failure —
// the only command that gate belongs to with zero arguments is `picker`.
func TestRun_DefaultCommandIsPicker(t *testing.T) {
	var stderr bytes.Buffer
	code := run(nil, strings.NewReader(""), &bytes.Buffer{}, &stderr, fakeEnv(map[string]string{
		"HERDR_BIN_PATH": "/nonexistent-herdr-binary-for-tests",
	}))
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "herdr binary not found") {
		t.Fatalf("stderr = %q, want a herdr-not-found message (proves cmdPicker ran)", stderr.String())
	}
}

// TestRun_UnknownCommand reproduces BEHAVIOUR.md §2.0: message on stderr,
// FULL usage text also on stderr, exit 2. `sesh-bro --workspaces` is an
// unknown COMMAND — there is no global flag parsing before the command
// word, so a `list` flag typed at this position does not reach `list` at
// all (BEHAVIOUR.md §2.0: "commands are matched exactly").
func TestRun_UnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--workspaces"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(nil))
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty (usage goes to stderr for an unknown command)", stdout.String())
	}
	if !strings.HasPrefix(stderr.String(), "sesh-bro: unknown command --workspaces\n") {
		t.Fatalf("stderr = %q, want it to start with the unknown-command line", stderr.String())
	}
	if !strings.Contains(stderr.String(), "usage: sesh-bro <command> [flags]") {
		t.Fatalf("stderr missing full usage text: %q", stderr.String())
	}
}

// TestRun_HelpOnStdout reproduces §2.0: -h/--help/help all print usage on
// STDOUT and exit 0 — the opposite stream from the unknown-command case.
func TestRun_HelpOnStdout(t *testing.T) {
	for _, arg := range []string{"-h", "--help", "help"} {
		var stdout, stderr bytes.Buffer
		code := run([]string{arg}, strings.NewReader(""), &stdout, &stderr, fakeEnv(nil))
		if code != 0 {
			t.Errorf("%s: code = %d, want 0", arg, code)
		}
		if stderr.Len() != 0 {
			t.Errorf("%s: stderr = %q, want empty", arg, stderr.String())
		}
		if !strings.Contains(stdout.String(), "usage: sesh-bro <command> [flags]") {
			t.Errorf("%s: stdout missing usage text: %q", arg, stdout.String())
		}
	}
}

// TestRun_Version reproduces §2.0: `sesh-bro <version>\n` on stdout, exit 0,
// for both -v and --version.
func TestRun_Version(t *testing.T) {
	for _, arg := range []string{"-v", "--version"} {
		var stdout bytes.Buffer
		code := run([]string{arg}, strings.NewReader(""), &stdout, &bytes.Buffer{}, fakeEnv(nil))
		if code != 0 {
			t.Errorf("%s: code = %d, want 0", arg, code)
		}
		if got := stdout.String(); !strings.HasPrefix(got, "sesh-bro ") || !strings.HasSuffix(got, "\n") {
			t.Errorf("%s: stdout = %q, want \"sesh-bro <version>\\n\"", arg, got)
		}
	}
}

// TestRun_ConnectMissingArgs reproduces BEHAVIOUR.md §9 S9: `connect` with
// fewer than 2 arguments fails with exit 1 — NOT exit 2 (that's reserved
// for an unknown TYPE, a different failure bash reaches via a different
// code path entirely — see BEHAVIOUR.md §2.3).
func TestRun_ConnectMissingArgs(t *testing.T) {
	for _, args := range [][]string{{"connect"}, {"connect", "workspace"}} {
		var stderr bytes.Buffer
		code := run(args, strings.NewReader(""), &bytes.Buffer{}, &stderr, fakeEnv(nil))
		if code != 1 {
			t.Errorf("args=%v: code = %d, want 1", args, code)
		}
	}
}

// TestRun_PreviewMissingArgs is preview's identical S9 case.
func TestRun_PreviewMissingArgs(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{"preview", "workspace"}, strings.NewReader(""), &bytes.Buffer{}, &stderr, fakeEnv(nil))
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
}

// TestResolveVersion_FromManifestBesideSelf covers the development layout:
// self sits directly beside herdr-plugin.toml (a `go build -o sesh-bro`
// from the repo root), matching bash's own single dirname(SELF) lookup.
func TestResolveVersion_FromManifestBesideSelf(t *testing.T) {
	files := map[string][]byte{
		"/plugin/herdr-plugin.toml": []byte("id = \"sesh-bro\"\nversion = \"9.9.9\"\nmin_herdr_version = \"0.8.0\"\n"),
	}
	got := resolveVersion("/plugin/sesh-bro", fakeReadFile(files))
	if got != "9.9.9" {
		t.Fatalf("resolveVersion() = %q, want %q", got, "9.9.9")
	}
}

// TestResolveVersion_FromInstalledBinLayout covers herdr-plugin.toml's real
// [[build]] output path: <plugin-root>/bin/sesh-bro, one directory BELOW
// the manifest — the second candidate resolveVersion probes.
func TestResolveVersion_FromInstalledBinLayout(t *testing.T) {
	files := map[string][]byte{
		"/plugin/herdr-plugin.toml": []byte("version = \"0.3.0\"\n"),
	}
	got := resolveVersion("/plugin/bin/sesh-bro", fakeReadFile(files))
	if got != "0.3.0" {
		t.Fatalf("resolveVersion() = %q, want %q", got, "0.3.0")
	}
}

// TestResolveVersion_ManifestVersionLineAnchored reproduces sesh-bro:88's
// anchor: `min_herdr_version = "0.8.0"` must NOT match even though it
// appears first and contains the same `= "..."` shape — only a line
// starting with exactly "version = \"" does.
func TestResolveVersion_ManifestVersionLineAnchored(t *testing.T) {
	files := map[string][]byte{
		"/plugin/herdr-plugin.toml": []byte("min_herdr_version = \"0.8.0\"\nversion = \"1.2.3\"\n"),
	}
	got := resolveVersion("/plugin/sesh-bro", fakeReadFile(files))
	if got != "1.2.3" {
		t.Fatalf("resolveVersion() = %q, want %q (min_herdr_version must not match)", got, "1.2.3")
	}
}

// TestResolveVersion_FallbackOnMissingManifest reproduces sesh-bro:89's
// "0.1.0" fallback and BEHAVIOUR.md §1.2's "self unresolvable" case.
func TestResolveVersion_FallbackOnMissingManifest(t *testing.T) {
	if got := resolveVersion("/plugin/sesh-bro", fakeReadFile(nil)); got != "0.1.0" {
		t.Errorf("missing manifest: resolveVersion() = %q, want \"0.1.0\"", got)
	}
	if got := resolveVersion("", fakeReadFile(nil)); got != "0.1.0" {
		t.Errorf("empty self: resolveVersion() = %q, want \"0.1.0\"", got)
	}
}

func fakeReadFile(files map[string][]byte) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		if data, ok := files[path]; ok {
			return data, nil
		}
		return nil, errors.New("not found")
	}
}
