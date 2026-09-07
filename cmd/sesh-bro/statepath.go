package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cyperx84/herdr-sesh-bro/internal/attention"
)

// statePath is where the recorded attention state lives.
//
// herdr hands a plugin a durable per-plugin directory in
// $HERDR_PLUGIN_STATE_DIR (~/.local/state/herdr/plugins/<id>), which is the
// right home: this data should survive a reboot, since "idle for 3h" is a
// perfectly good thing to be told after lunch.
//
// The second branch matters more than it looks. Only commands herdr itself
// invokes get that variable, so a `sesh-bro list` typed into an ordinary shell
// would otherwise read a DIFFERENT, empty file and silently show no badges —
// the same binary behaving differently depending on who launched it, which is
// exactly the sort of thing that gets diagnosed as "the feature is broken".
// herdr's layout is stable and documented, so reconstruct the same path rather
// than diverge from it. This is XDG's state directory, not a stray dotdir, and
// in this branch it is only ever read: the writer is the hook, which herdr
// always runs with the variable set.
//
// The last branch is for a machine with no $HOME at all, where writing
// anything under a home directory would be wrong.
func statePath(getenv func(string) string) string {
	if dir := getenv("HERDR_PLUGIN_STATE_DIR"); dir != "" {
		return filepath.Join(dir, "state.json")
	}

	pluginID := getenv("HERDR_PLUGIN_ID")
	if pluginID == "" {
		pluginID = "sesh-bro"
	}
	if home := getenv("HOME"); home != "" {
		return filepath.Join(home, ".local", "state", "herdr", "plugins", pluginID, "state.json")
	}

	base := getenv("TMPDIR")
	if base == "" {
		base = "/tmp"
	}
	return filepath.Join(base, fmt.Sprintf("sesh-bro-%d", os.Getuid()), "state.json")
}

// attentionLoad and attentionSave are indirections so tests can observe the
// recording path without a real state directory.
var (
	attentionLoad   = attention.Load
	attentionSave   = attention.Save
	attentionUpdate = attention.Update
)
