package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx/herdrtest"
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

// fakeHerdrScript writes a stand-in `herdr` binary that prints what a real one
// would and exits with the given code.
func fakeHerdrScript(t *testing.T, stdout, stderr string, code int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "herdr")
	script := "#!/bin/sh\n"
	if stdout != "" {
		script += "printf '%s' " + shellSingleQuote(stdout) + "\n"
	}
	if stderr != "" {
		script += "printf '%s' " + shellSingleQuote(stderr) + " >&2\n"
	}
	script += "exit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Pressing the open chord while the popup is open closes it. herdr refuses to
// stack a second popup, and before 0.4.0 that refusal simply surfaced as an
// error while the popup stayed put — so opening and closing were different
// gestures for one thing.
// Both wordings, because herdr 0.9.0 reworded the refusal and the manifest
// still supports 0.8.2. Recognising only the 0.8.x phrasing leaves the popup
// permanently up on 0.9.0.
func TestOpenTogglesClosedWhenPopupAlreadyOpen(t *testing.T) {
	for _, tc := range []struct {
		name    string
		refusal string
	}{
		{
			name:    "herdr 0.8.x",
			refusal: `{"error":{"code":"plugin_pane_open_failed","message":"popup already open"},"id":"cli:plugin"}`,
		},
		{
			name:    "herdr 0.9.0",
			refusal: `{"error":{"code":"ui_busy","message":"a popup pane is already open"},"id":"cli:plugin"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := herdrtest.Start(t)
			s.Handle("popup.close", func(json.RawMessage) (any, error) { return map[string]any{}, nil })

			herdrBin := fakeHerdrScript(t, "", tc.refusal, 1)

			var stdout, stderr bytes.Buffer
			code := run([]string{"open"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(map[string]string{
				"HERDR_SOCKET_PATH": s.Path(),
				"HERDR_BIN_PATH":    herdrBin,
			}))
			if code != 0 {
				t.Fatalf("code = %d, want 0 — a toggle is a success (stderr: %q)", code, stderr.String())
			}
			if len(s.Calls("popup.close")) != 1 {
				t.Errorf("popup.close calls = %d, want 1", len(s.Calls("popup.close")))
			}
			if strings.Contains(stderr.String(), "already open") {
				t.Errorf("herdr's refusal leaked to the user: %q", stderr.String())
			}
		})
	}
}

// Any OTHER open failure must still surface. Neither version's error code is
// specific to "already open" — 0.8.x reused plugin_pane_open_failed for
// unrelated failures and 0.9.0's ui_busy covers other UI-busy refusals — so
// matching on the code rather than the message would turn every failed open
// into a close.
func TestOpenForwardsOtherFailures(t *testing.T) {
	s := herdrtest.Start(t)
	s.Handle("popup.close", func(json.RawMessage) (any, error) { return map[string]any{}, nil })

	herdrBin := fakeHerdrScript(t, "",
		`{"error":{"code":"plugin_pane_open_failed","message":"pane spawn failed"},"id":"cli:plugin"}`, 1)

	var stdout, stderr bytes.Buffer
	code := run([]string{"open"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(map[string]string{
		"HERDR_SOCKET_PATH": s.Path(),
		"HERDR_BIN_PATH":    herdrBin,
	}))
	if code == 0 {
		t.Error("code = 0, want the child's failure passed through")
	}
	if !strings.Contains(stderr.String(), "pane spawn failed") {
		t.Errorf("stderr = %q, want herdr's own error forwarded", stderr.String())
	}
	if len(s.Calls("popup.close")) != 0 {
		t.Error("closed the popup on an unrelated open failure")
	}
}

// A successful open forwards herdr's output and exit code, exactly as the
// previous exec-based implementation did.
func TestOpenForwardsSuccess(t *testing.T) {
	s := herdrtest.Start(t)
	herdrBin := fakeHerdrScript(t, `{"result":{"opened":true}}`, "", 0)

	var stdout, stderr bytes.Buffer
	code := run([]string{"open"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(map[string]string{
		"HERDR_SOCKET_PATH": s.Path(),
		"HERDR_BIN_PATH":    herdrBin,
	}))
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "opened") {
		t.Errorf("stdout = %q, want herdr's output forwarded", stdout.String())
	}
}
