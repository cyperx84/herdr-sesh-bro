// The live layer: keep an open picker in step with herdr without polling it
// and without the user pressing anything.
//
// The shape is push, not poll. A goroutine subscribes to herdr's events, and
// when something changes it re-renders every view into files and posts one
// `reload` into the running fzf through its --listen socket. A timer-based
// design would have to re-query the daemon on a fixed interval forever to
// discover that nothing happened, which is exactly the kind of idle cost this
// tool should not add to a machine already running a dozen agents.
//
// Everything here is optional. Any failure — old fzf, unwritable TMPDIR,
// daemon refusing a subscription — leaves a picker that behaves exactly as it
// did in 0.3.0: a snapshot of the moment you opened it.
package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cyperx84/herdr-sesh-bro/internal/config"
	"github.com/cyperx84/herdr-sesh-bro/internal/fzfctl"
	"github.com/cyperx84/herdr-sesh-bro/internal/live"
)

// startLiveUpdates renders the initial view files and starts the watcher that
// keeps them, and the running picker, current. The returned function stops it.
func startLiveUpdates(ctx context.Context, env *appEnv, cfg config.Config, dir string, hideCurrent bool, initial listSources) func() {
	ctx, cancel := context.WithCancel(ctx)

	r := &renderer{
		env:         env,
		cfg:         cfg,
		dir:         dir,
		hideCurrent: hideCurrent,
		fzf:         fzfctl.New(listenSocketPath(dir)),
		lastPushed:  map[string]string{},
		prev:        &initial,
	}

	// Render the view files once up front so a filter keypress has something
	// to read even if no event ever arrives — but do NOT push. fzf has not
	// created its listen socket yet at this point, so a push here always
	// failed and was always swallowed, which is a confusing thing to leave in
	// place for anyone reading the logs.
	panes, _ := r.renderAll(ctx, false)

	client, openErr := openHerdr(env.getenv)
	if openErr != nil {
		return cancel
	}

	w := &live.Watcher{Render: func(ctx context.Context) error {
		_, err := r.renderAll(ctx, true)
		return err
	}}
	go func() { _ = w.Run(ctx, client.Raw(), panes) }()

	startForeignTicker(ctx, cfg, initial.allSessions, r)

	return cancel
}

// startForeignTicker re-renders on a clock so other sessions' rows go stale.
//
// This is the project's one poll, and it exists only because the alternative
// is worse: foreign sessions emit their events to their own sockets, so
// following them means one global subscription per session plus one per-pane
// subscription per foreign agent — O(sessions × panes) held connections for
// rows that are read-only anyway. internal/live's package comment carries the
// full argument.
//
// It starts only when there is something to poll FOR. On the ordinary
// one-session machine with the feature off, no goroutine and no timer exists,
// so the cost of the feature to someone not using it is zero rather than
// small.
func startForeignTicker(ctx context.Context, cfg config.Config, allSessions bool, r *renderer) {
	if !allSessions {
		return
	}
	interval, err := cfg.ForeignInterval()
	if err != nil || interval <= 0 {
		return
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				// Tick renders are FOREIGN-ONLY in intent, so they must not
				// pay the local costs a real event pays. renderAll's source
				// reuse is what makes that true: the git cache and zoxide list
				// come back from prev untouched, and only the foreign TTL has
				// lapsed. Without that, a five-second tick would respawn
				// `git status` per workspace forever, because gitCacheTTL is
				// also five seconds — a background process quietly running git
				// on every repo you have open, for a row in another session.
				_, _ = r.render(ctx, true, true)
			}
		}
	}()
}

// renderer writes every view's rows to disk and pushes the visible one into
// the picker.
type renderer struct {
	env         *appEnv
	cfg         config.Config
	dir         string
	hideCurrent bool
	fzf         *fzfctl.Client

	// prev carries the previous render's sources so a re-render reuses the
	// zoxide list and git cache instead of re-shelling them per event.
	prev *listSources

	// lastPushed is the bytes most recently written per view, so an event
	// that changes nothing the user can see costs a snapshot read and stops
	// there. Without it, every pane_updated — which herdr emits freely —
	// would reload the list under the user's cursor for no visible reason.
	lastPushed map[string]string
}

// renderAll refreshes every view file from one snapshot and, when the visible
// view actually changed, pushes a reload. It returns the agent panes seen, for
// the watcher's initial subscription set.
func (r *renderer) renderAll(ctx context.Context, push bool) ([]string, error) {
	return r.render(ctx, push, false)
}

// render is renderAll with the foreign-tick distinction made explicit. See
// refreshSources' keepGit parameter for what the distinction buys.
func (r *renderer) render(ctx context.Context, push, foreignTick bool) ([]string, error) {
	// One snapshot serves every view: sources are read for the union of what
	// any view needs, and each view is then a pure re-render of it. The
	// daemon-liveness gate is deliberately skipped — this is driven by an
	// event that came FROM the daemon, so re-proving it is alive costs a
	// round trip to learn nothing.
	unionFlags := listFlags{wantWS: true, wantAgent: true, wantDir: true, wantWorktree: true, header: true}
	src, err := refreshSources(ctx, r.env, r.cfg, unionFlags, r.prev, foreignTick)
	if err != nil {
		return nil, err
	}
	r.prev = &src

	current := readView(r.dir)
	changed := false
	for _, view := range pickerViews {
		flags, err := parseListFlags(append(viewFlags(view), r.hideFlag()...))
		if err != nil {
			continue
		}
		flags.header = true

		var buf stringBuilder
		if err := renderRows(ctx, r.cfg, flags, src, &buf); err != nil {
			continue
		}
		out := buf.String()
		if r.lastPushed[view] == out {
			continue
		}
		// Write to a sibling and rename: fzf may `cat` this file at any
		// moment, and a partially written one would show a truncated list.
		tmp := filepath.Join(r.dir, view+".tsv.tmp")
		if err := os.WriteFile(tmp, []byte(out), 0o600); err != nil {
			continue
		}
		if err := os.Rename(tmp, rowsFile(r.dir, view)); err != nil {
			continue
		}
		r.lastPushed[view] = out
		if view == current {
			changed = true
		}
	}

	if changed && push {
		// If the row the cursor is tracking is about to disappear, tell fzf to
		// land on the first row instead. --track otherwise hunts for an
		// identity that will never arrive; measured against fzf 0.74.3 it
		// recovers once the stream ends rather than hanging, but it lands
		// somewhere arbitrary, and "the thing I was looking at is gone" is
		// better answered by the top of the list than by its neighbour.
		//
		// Every error here means the picker is gone, which is the normal end
		// of its life rather than a fault worth reporting.
		first := false
		if st, err := r.fzf.Query(ctx); err == nil {
			if target := st.CurrentTarget(); target != "" {
				first = !strings.Contains(r.lastPushed[current], "\t"+target+"\t")
			}
		}
		_ = r.fzf.Reload(ctx, rowsFile(r.dir, current), first)
	}
	return src.snap.AgentPanes(), nil
}

func (r *renderer) hideFlag() []string {
	if r.hideCurrent {
		return []string{"--hide-current"}
	}
	return nil
}

// stringBuilder is strings.Builder as an io.Writer, named so renderRows'
// signature stays io.Writer rather than growing a second form.
type stringBuilder struct{ b []byte }

func (s *stringBuilder) Write(p []byte) (int, error) {
	s.b = append(s.b, p...)
	return len(p), nil
}
func (s *stringBuilder) String() string { return string(s.b) }
