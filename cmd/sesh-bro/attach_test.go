package main

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func envGet(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestAttachCommandSubstitutesTheSession(t *testing.T) {
	got, err := attachCommand(envGet(map[string]string{AttachCmdVar: "wezterm start -- herdr session attach {session}"}), "work")
	if err != nil {
		t.Fatal(err)
	}
	if got != "wezterm start -- herdr session attach work" {
		t.Fatalf("got %q", got)
	}
}

// TestAttachCommandRequiresThePlaceholder: a template without it would open a
// terminal attached to whatever session the command happened to name, which is
// a silently wrong destination rather than a visible error.
func TestAttachCommandRequiresThePlaceholder(t *testing.T) {
	_, err := attachCommand(envGet(map[string]string{AttachCmdVar: "wezterm start"}), "work")
	if err == nil || !strings.Contains(err.Error(), attachPlaceholder) {
		t.Fatalf("err = %v, want one naming the placeholder", err)
	}
}

// TestAttachCommandRefusesShellMetacharacters: the name is spliced into a
// command line. Session names come from herdr, not from a stranger, but the
// gap between "trusted enough" and "trusted" is where injections live.
func TestAttachCommandRefusesShellMetacharacters(t *testing.T) {
	for _, bad := range []string{"a;rm -rf /", "a`id`", "a$(id)", "a|b", "a&b", "a\nb", `a"b`, "a'b"} {
		if _, err := attachCommand(envGet(map[string]string{AttachCmdVar: "t {session}"}), bad); err == nil {
			t.Errorf("attachCommand accepted %q", bad)
		}
	}
}

// TestAttachCommandDefaults: macOS gets the shape already proven on this
// machine; every other platform gets a refusal naming the variable, because
// terminal emulators vary too much for a guess to be right and the failure
// mode of a wrong guess is a command that appears to do nothing.
func TestAttachCommandDefaults(t *testing.T) {
	got, err := attachCommand(envGet(nil), "work")
	if runtime.GOOS == "darwin" {
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "herdr session attach work") {
			t.Fatalf("darwin default = %q", got)
		}
		return
	}
	if err == nil {
		t.Fatalf("got %q, want a refusal naming %s", got, AttachCmdVar)
	}
	if !strings.Contains(err.Error(), AttachCmdVar) {
		t.Fatalf("err = %v, want it to name the variable to set", err)
	}
}

// TestAttachEnvStripsHerdrVariables is the load-bearing test of the whole
// mechanism, and it is here because the bug it prevents is invisible in code
// review.
//
// macOS `open` hands the caller's environment to the application it launches,
// so a terminal spawned from inside a herdr pane inherits HERDR_ENV=1 with the
// rest — and herdr refuses to start nested. Verified against a live daemon:
// the spawn failed with "nested herdr is disabled by default" until these were
// removed, and succeeded immediately afterwards.
func TestAttachEnvStripsHerdrVariables(t *testing.T) {
	in := []string{
		"PATH=/usr/bin",
		"HERDR_ENV=1",
		"HERDR_SOCKET_PATH=/tmp/herdr.sock",
		"HERDR_PANE_ID=w1:p1",
		"HOME=/home/x",
		"SESH_BRO_ALL_SESSIONS=1",
	}
	got := attachEnv(in)
	for _, kv := range got {
		if strings.HasPrefix(kv, "HERDR_") {
			t.Errorf("%q survived; herdr will refuse to start nested", kv)
		}
	}
	// Everything else must survive: the spawned terminal still needs a PATH to
	// find herdr with.
	for _, want := range []string{"PATH=/usr/bin", "HOME=/home/x", "SESH_BRO_ALL_SESSIONS=1"} {
		found := false
		for _, kv := range got {
			if kv == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%q was stripped and should not have been", want)
		}
	}
}

// TestAttachSessionSpawnsNothingInTests proves the seam: the command and the
// stripped environment are observable without a process ever being started.
func TestAttachSessionSpawnsNothingInTests(t *testing.T) {
	var gotCmd string
	var gotEnv []string
	orig := runAttach
	runAttach = func(_ context.Context, command string, environ []string) error {
		gotCmd, gotEnv = command, environ
		return nil
	}
	t.Cleanup(func() { runAttach = orig })

	t.Setenv("HERDR_ENV", "1")
	env := &appEnv{getenv: envGet(map[string]string{AttachCmdVar: "term -e herdr session attach {session}"})}
	if err := attachSession(t.Context(), env, "work"); err != nil {
		t.Fatal(err)
	}
	if gotCmd != "term -e herdr session attach work" {
		t.Fatalf("command = %q", gotCmd)
	}
	for _, kv := range gotEnv {
		if strings.HasPrefix(kv, "HERDR_") {
			t.Fatalf("%q reached the spawn", kv)
		}
	}
}
