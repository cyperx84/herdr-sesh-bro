package live

import (
	"context"
	"time"

	herdr "github.com/cyperx84/herdr-api"
)

// Defaults for the two timings that shape how a live picker feels.
const (
	// DefaultDebounce is how long to wait for the flurry to end before
	// re-rendering. A single user action routinely produces several events —
	// a pane is created, then gets an agent, then that agent reports a status
	// — and re-rendering each time would push three reloads for one logical
	// change. A quarter second is below the threshold where a list feels
	// stale and comfortably above the spacing of a burst.
	DefaultDebounce = 250 * time.Millisecond

	// DefaultReplayGrace is how long to ignore events after subscribing.
	//
	// herdr REPLAYS event history to a new subscriber before streaming live
	// ones, so the first thing any subscription delivers is a backlog that may
	// describe panes that died hours ago. Nothing here derives state from
	// events — a render always reads a fresh snapshot — so a replayed backlog
	// can only cost one redundant render, never a wrong one. This window
	// removes even that.
	DefaultReplayGrace = 300 * time.Millisecond
)

// Watcher re-renders on herdr activity: it subscribes, debounces the noise,
// and calls Render.
type Watcher struct {
	// Render is invoked when something may have changed. It must read
	// authoritative state itself; the events say only "look again". An error
	// is not fatal — the daemon may be briefly unavailable, and the next
	// event will try again.
	Render func(ctx context.Context) error

	// Debounce and ReplayGrace default to the constants above when zero.
	Debounce    time.Duration
	ReplayGrace time.Duration

	// Now is injected for tests.
	Now func() time.Time
}

// Run subscribes and re-renders until ctx is cancelled.
//
// initialPanes are the agent panes known at start, each of which gets its own
// status subscription; more are added as panes appear.
func (w *Watcher) Run(ctx context.Context, client subscriber, initialPanes []string) error {
	debounce := w.Debounce
	if debounce <= 0 {
		debounce = DefaultDebounce
	}
	grace := w.ReplayGrace
	if grace < 0 {
		grace = DefaultReplayGrace
	} else if grace == 0 && w.ReplayGrace == 0 {
		grace = DefaultReplayGrace
	}
	now := w.Now
	if now == nil {
		now = time.Now
	}

	bus, err := newEventBus(ctx, client)
	if err != nil {
		return err
	}
	defer bus.Close()

	for _, pane := range initialPanes {
		bus.watch(ctx, pane)
	}

	liveAt := now().Add(grace)

	// A nil channel blocks forever, which is how "no render pending" is
	// expressed without a second flag.
	var timer *time.Timer
	var fire <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case ev := <-bus.Events():
			// Keep subscriptions in step with the panes that exist, even
			// during the replay window: a pane the backlog mentions may well
			// still be alive, and watching it early costs nothing.
			switch ev.Kind {
			case "pane_created", "pane_agent_detected", "pane_updated":
				bus.watch(ctx, paneIDOf(ev))
			case "pane_closed", "pane_exited":
				bus.unwatch(paneIDOf(ev))
			}

			if now().Before(liveAt) {
				continue // replayed history, not news
			}
			if timer == nil {
				timer = time.NewTimer(debounce)
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(debounce)
			}
			fire = timer.C

		case <-fire:
			fire = nil
			if w.Render != nil {
				// A render failure is not worth tearing the picker down for;
				// the list simply stays as it was until the next event.
				_ = w.Render(ctx)
			}
		}
	}
}

// AgentPanesOf is a small helper for callers assembling the initial watch set
// from a snapshot's agents.
func AgentPanesOf(agents []herdr.Agent) []string {
	out := make([]string, 0, len(agents))
	for _, a := range agents {
		if a.PaneID != "" {
			out = append(out, a.PaneID)
		}
	}
	return out
}
