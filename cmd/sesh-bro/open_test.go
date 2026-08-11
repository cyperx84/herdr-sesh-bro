package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestCmdOpen_UnknownFlag reproduces sesh-bro:864-868: an unrecognised flag
// is rejected BEFORE the herdr exec is ever attempted — exit 2, its own
// message. Note --json is deliberately not in openFlags (BEHAVIOUR.md
// §2.2.9: "open rejects it, exit 2" — `list` accepts --json, `open` does
// not).
func TestCmdOpen_UnknownFlag(t *testing.T) {
	for _, bad := range []string{"--json", "--nope", "--all"} {
		var stderr bytes.Buffer
		env := &appEnv{getenv: fakeEnv(nil), stdout: &bytes.Buffer{}, stderr: &stderr}
		code := cmdOpen(env, []string{bad})
		if code != 2 {
			t.Errorf("%s: code = %d, want 2", bad, code)
		}
		want := "sesh-bro open: unknown flag " + bad + "\n"
		if stderr.String() != want {
			t.Errorf("%s: stderr = %q, want %q", bad, stderr.String(), want)
		}
	}
}

// TestCmdOpen_UnknownFlagStopsAtFirstBad mirrors bash's left-to-right scan:
// the FIRST bad flag is reported even when a later, valid-looking flag also
// appears — bash's loop exits on the first mismatch, it does not collect
// every bad flag.
func TestCmdOpen_UnknownFlagStopsAtFirstBad(t *testing.T) {
	var stderr bytes.Buffer
	env := &appEnv{getenv: fakeEnv(nil), stdout: &bytes.Buffer{}, stderr: &stderr}
	code := cmdOpen(env, []string{"--workspaces", "--nope", "--agents"})
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "--nope") {
		t.Fatalf("stderr = %q, want it to name --nope", stderr.String())
	}
}

// TestOpenFlags_AllDocumentedFlagsAccepted proves every flag BEHAVIOUR.md
// §2.9 lists as valid for `open` is actually in the accepted set, and
// --json is definitively excluded.
func TestOpenFlags_AllDocumentedFlagsAccepted(t *testing.T) {
	want := []string{"--workspaces", "--agents", "--dirs", "--blocked", "--working", "--done", "--idle", "--hide-current"}
	for _, f := range want {
		if !openFlags[f] {
			t.Errorf("openFlags[%q] = false, want true", f)
		}
	}
	if openFlags["--json"] {
		t.Error(`openFlags["--json"] = true, want false`)
	}
}
