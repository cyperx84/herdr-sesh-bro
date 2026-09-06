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

	"github.com/cyperx84/herdr-sesh-bro/internal/config"
	"github.com/cyperx84/herdr-sesh-bro/internal/fzfctl"
	"github.com/cyperx84/herdr-sesh-bro/internal/live"
)

// startLiveUpdates renders the initial view files and starts the watcher that
// keeps them, and the running picker, current. The returned function stops it.
func startLiveUpdates(ctx context.Context, env *appEnv, cfg config.Config, dir string, hideCurrent bool) func() {
	ctx, cancel := context.WithCancel(ctx)

	r := &renderer{
		env:         env,
		cfg:         cfg,
		dir:         dir,
		hideCurrent: hideCurrent,
		fzf:         fzfctl.New(listenSocketPath(dir)),
		lastPushed:  map[string]string{},
	}

	// Render once up front so a filter keypress has a file to read even if no
	// event ever arrives.
	panes, _ := r.renderAll(ctx)

	client, openErr := openHerdr(env.getenv)
	if openErr != nil {
		return cancel
	}

	w := &live.Watcher{Render: func(ctx context.Context) error {
		_, err := r.renderAll(ctx)
		return err
	}}
	go func() { _ = w.Run(ctx, client.Raw(), panes) }()

	return cancel
}

// renderer writes every view's rows to disk and pushes the visible one into
// the picker.
type renderer struct {
	env         *appEnv
	cfg         config.Config
	dir         string
	hideCurrent bool
	fzf         *fzfctl.Client

	// lastPushed is the bytes most recently written per view, so an event
	// that changes nothing the user can see costs a snapshot read and stops
	// there. Without it, every pane_updated — which herdr emits freely —
	// would reload the list under the user's cursor for no visible reason.
	lastPushed map[string]string
}

// renderAll refreshes every view file from one snapshot and, when the visible
// view actually changed, pushes a reload. It returns the agent panes seen, for
// the watcher's initial subscription set.
func (r *renderer) renderAll(ctx context.Context) ([]string, error) {
	// One snapshot serves every view: loadSources is asked for the union of
	// what any view needs, and each view is then a pure re-render of it.
	unionFlags := listFlags{wantWS: true, wantAgent: true, wantDir: true, header: true}
	src, err := loadSources(ctx, r.env, r.cfg, unionFlags)
	if err != nil {
		return nil, err
	}

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

	if changed {
		// Errors here mean the picker is gone, which is the normal end of its
		// life rather than a fault worth reporting.
		_ = r.fzf.Reload(ctx, rowsFile(r.dir, current), false)
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
