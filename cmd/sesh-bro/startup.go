// cmdStartup is the manifest's [[startup]] hook (BEHAVIOUR.md §2.1): validate
// deps, tidy up, and report what it found. Any arguments are silently ignored
// (the bash never read $1/$2 here either).
//
// It used to also clear a stale pane cache. That cache was removed in 0.4.0
// when session.snapshot replaced the four reads it existed to soften
// (BEHAVIOUR.md §10.1).
//
// Its output lands in `herdr plugin log list`, which is where someone asking
// "why doesn't my picker update?" will actually look — so it reports the fzf
// version and which live features that build supports, rather than leaving
// them detectable only from inside the picker that is failing to be live.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/cyperx84/herdr-sesh-bro/internal/attention"
	"github.com/cyperx84/herdr-sesh-bro/internal/external"
	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx"
	"github.com/cyperx84/herdr-sesh-bro/internal/picker"
)

// detectFzf is a seam so tests can report a chosen fzf version without one
// being installed.
var detectFzf = func() picker.Features {
	return picker.Detect(func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).Output()
	})
}

func cmdStartup(ctx context.Context, env *appEnv) int {
	client, openErr := openHerdr(env.getenv)
	if err := external.CheckDeps(ctx, env.herdrBin, aliverFor(client, openErr)); err != nil {
		fmt.Fprintln(env.stderr, err)
		return 1
	}
	fmt.Fprintln(env.stdout, "sesh-bro: deps ok")

	fmt.Fprintln(env.stdout, "sesh-bro: "+describeFzf(detectFzf()))

	// Server start is the right cadence for both sweeps: once, when nothing
	// is open yet, so neither can race a running picker.
	pruneStaleRuntimeDirs(env.getenv, os.Getpid())
	pruneState(ctx, env, client, openErr)
	return 0
}

// describeFzf renders the feature report. It names what is missing rather than
// only what is present, because "live updates: no" is the answer to the
// question someone is actually asking.
func describeFzf(f picker.Features) string {
	if f.Version == "" {
		return "fzf not found or unrecognised — the picker cannot open"
	}
	yn := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	return fmt.Sprintf("fzf %s (live updates: %s, cursor tracking: %s, footer: %s)",
		f.Version, yn(f.Listen), yn(f.TrackID), yn(f.Footer))
}

// pruneState drops recorded panes and workspaces that no longer exist.
//
// Nothing else ever removes them: the event hook only ever adds, so the file
// otherwise accumulates an entry for every pane the machine has ever run an
// agent in. A failure here is not worth reporting — the data is a convenience,
// and a stale entry costs a badge that attention.Since already refuses to
// render when the status disagrees.
func pruneState(ctx context.Context, env *appEnv, client *herdrx.Client, openErr error) {
	snap, err := loadSnapshot(ctx, client, openErr)
	if err != nil {
		return
	}
	// Union both, because either alone can under-report and the cost of a
	// false negative is deleting a live agent's recorded clock. Agents carry
	// a pane id definitionally, so an agent's pane is live whatever the pane
	// list happens to contain.
	panes := make(map[string]bool, len(snap.Panes)+len(snap.Agents))
	for _, p := range snap.Panes {
		panes[p.ID] = true
	}
	for _, a := range snap.Agents {
		if a.PaneID != "" {
			panes[a.PaneID] = true
		}
	}
	workspaces := make(map[string]bool, len(snap.Workspaces))
	for _, w := range snap.Workspaces {
		workspaces[w.ID] = true
	}
	for _, a := range snap.Agents {
		if a.WorkspaceID != "" {
			workspaces[a.WorkspaceID] = true
		}
	}
	_ = attentionUpdate(statePath(env.getenv), func(s *attention.State) {
		s.Prune(panes, workspaces)
	})
}
