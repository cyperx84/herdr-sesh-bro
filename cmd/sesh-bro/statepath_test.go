package main

import (
	"strings"
	"testing"
)

// herdr's own variable wins when present — that is the authoritative answer
// and the only one the writing hook ever sees.
func TestStatePathPrefersHerdrStateDir(t *testing.T) {
	got := statePath(fakeEnv(map[string]string{
		"HERDR_PLUGIN_STATE_DIR": "/var/state/herdr/plugins/sesh-bro",
		"HOME":                   "/Users/x",
	}))
	if got != "/var/state/herdr/plugins/sesh-bro/state.json" {
		t.Errorf("path = %q", got)
	}
}

// Run from an ordinary shell the binary must find the SAME file the hook
// wrote, or badges silently vanish depending on who launched the command.
func TestStatePathReconstructsHerdrLayout(t *testing.T) {
	got := statePath(fakeEnv(map[string]string{"HOME": "/Users/x"}))
	want := "/Users/x/.local/state/herdr/plugins/sesh-bro/state.json"
	if got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func TestStatePathHonoursPluginID(t *testing.T) {
	got := statePath(fakeEnv(map[string]string{"HOME": "/Users/x", "HERDR_PLUGIN_ID": "sesh-bro-dev"}))
	if !strings.Contains(got, "/plugins/sesh-bro-dev/") {
		t.Errorf("path = %q, want it to use the plugin id", got)
	}
}

// With no home there is nowhere correct to put it under one.
func TestStatePathFallsBackToTemp(t *testing.T) {
	got := statePath(fakeEnv(map[string]string{"TMPDIR": "/scratch"}))
	if !strings.HasPrefix(got, "/scratch/sesh-bro-") || !strings.HasSuffix(got, "/state.json") {
		t.Errorf("path = %q, want a temp fallback", got)
	}
}
