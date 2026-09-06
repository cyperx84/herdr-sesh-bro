package live

import (
	"context"
	"sync"
	"testing"
	"time"

	herdr "github.com/cyperx84/herdr-api"

	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx/herdrtest"
)

// renders counts Render calls, so a test can assert coalescing rather than
// just "something happened".
type renders struct {
	mu sync.Mutex
	n  int
	ch chan struct{}
}

func newRenders() *renders { return &renders{ch: make(chan struct{}, 64)} }

func (r *renders) fn(context.Context) error {
	r.mu.Lock()
	r.n++
	r.mu.Unlock()
	select {
	case r.ch <- struct{}{}:
	default:
	}
	return nil
}

func (r *renders) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

// waitFor polls until cond holds or the deadline passes, so timing assertions
// do not depend on a single sleep being long enough on a loaded machine.
func waitFor(t *testing.T, d time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

func startWatcher(t *testing.T, w *Watcher, panes []string) (*herdrtest.Server, context.CancelFunc) {
	t.Helper()
	s := herdrtest.Start(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = w.Run(ctx, herdr.New(s.Path()), panes) }()
	return s, cancel
}

// A burst of events produces ONE render: a single user action routinely emits
// several events, and re-rendering per event would push three reloads for one
// logical change.
func TestWatcherCoalescesBurst(t *testing.T) {
	r := newRenders()
	w := &Watcher{Render: r.fn, Debounce: 60 * time.Millisecond, ReplayGrace: time.Nanosecond}
	s, cancel := startWatcher(t, w, nil)
	defer cancel()

	if !waitFor(t, 2*time.Second, func() bool { return len(s.Subscriptions()) > 0 }) {
		t.Fatal("watcher never subscribed")
	}
	time.Sleep(20 * time.Millisecond) // let the replay grace lapse

	for i := 0; i < 5; i++ {
		s.Emit("pane_agent_status_changed", map[string]any{"pane_id": "w1:p1", "agent_status": "blocked"})
	}
	if !waitFor(t, 2*time.Second, func() bool { return r.count() >= 1 }) {
		t.Fatal("no render after a burst of events")
	}
	time.Sleep(150 * time.Millisecond)
	if n := r.count(); n != 1 {
		t.Errorf("renders = %d, want exactly 1 for a burst inside one debounce window", n)
	}
}

// herdr replays event history to every new subscriber. Nothing here derives
// state from events, so a replay can only cost a redundant render — the grace
// window removes even that.
func TestWatcherIgnoresReplayedBacklog(t *testing.T) {
	r := newRenders()
	w := &Watcher{Render: r.fn, Debounce: 20 * time.Millisecond, ReplayGrace: 400 * time.Millisecond}
	s, cancel := startWatcher(t, w, nil)
	defer cancel()

	if !waitFor(t, 2*time.Second, func() bool { return len(s.Subscriptions()) > 0 }) {
		t.Fatal("watcher never subscribed")
	}
	for i := 0; i < 3; i++ {
		s.Emit("pane_exited", map[string]any{"pane_id": "w1:pold"})
	}
	time.Sleep(200 * time.Millisecond)
	if n := r.count(); n != 0 {
		t.Errorf("renders = %d during the replay window, want 0", n)
	}
}

// The global subscription cannot carry per-pane status, so every agent pane
// needs its own — at startup and as panes appear.
func TestWatcherSubscribesPerPane(t *testing.T) {
	r := newRenders()
	w := &Watcher{Render: r.fn, Debounce: 20 * time.Millisecond, ReplayGrace: time.Nanosecond}
	s, cancel := startWatcher(t, w, []string{"w1:p1"})
	defer cancel()

	hasPane := func(id string) bool {
		for _, sub := range s.Subscriptions() {
			if sub.Type == "pane.agent_status_changed" && sub.PaneID == id {
				return true
			}
		}
		return false
	}
	if !waitFor(t, 2*time.Second, func() bool { return hasPane("w1:p1") }) {
		t.Fatalf("no status subscription for the initial pane, got %+v", s.Subscriptions())
	}

	// A pane that appears later must be picked up without restarting.
	s.Emit("pane_created", map[string]any{"pane": map[string]any{"pane_id": "w1:p9"}})
	if !waitFor(t, 2*time.Second, func() bool { return hasPane("w1:p9") }) {
		t.Errorf("no status subscription for a pane created later, got %+v", s.Subscriptions())
	}
}

// The global subscription set must include the topology events, or a workspace
// being renamed or closed would never reach the list.
func TestWatcherSubscribesGlobally(t *testing.T) {
	r := newRenders()
	w := &Watcher{Render: r.fn, ReplayGrace: time.Nanosecond}
	s, cancel := startWatcher(t, w, nil)
	defer cancel()

	if !waitFor(t, 2*time.Second, func() bool { return len(s.Subscriptions()) >= len(globalSubscriptions) }) {
		t.Fatalf("subscriptions = %+v", s.Subscriptions())
	}
	seen := map[string]bool{}
	for _, sub := range s.Subscriptions() {
		seen[sub.Type] = true
	}
	for _, want := range []string{"pane.created", "pane.closed", "workspace.focused", "pane.agent_detected"} {
		if !seen[want] {
			t.Errorf("missing global subscription %q", want)
		}
	}
}

func TestPaneIDOfBothShapes(t *testing.T) {
	nested := herdr.Event{Kind: "pane_created", Data: []byte(`{"pane":{"pane_id":"w1:p3"}}`)}
	if got := paneIDOf(nested); got != "w1:p3" {
		t.Errorf("nested shape = %q, want w1:p3", got)
	}
	flat := herdr.Event{Kind: "pane_agent_status_changed", Data: []byte(`{"pane_id":"w1:p4"}`)}
	if got := paneIDOf(flat); got != "w1:p4" {
		t.Errorf("flat shape = %q, want w1:p4", got)
	}
	if got := paneIDOf(herdr.Event{Kind: "workspace_focused", Data: []byte(`{"workspace_id":"w1"}`)}); got != "" {
		t.Errorf("event with no pane = %q, want empty", got)
	}
}
