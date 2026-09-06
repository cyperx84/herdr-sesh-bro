// Package live keeps an open picker in step with herdr, by subscribing to the
// daemon's events rather than polling it.
//
// The eventBus here is adapted from herdr-loop's cmd/herdr-loop/eventbus.go
// (same author, MIT), which solved the same problem first: herdr's per-pane
// subscriptions require a pane_id and offer no wildcard, so following a whole
// session means one global subscription for topology changes plus one per-pane
// subscription for every pane that has an agent, opened and closed as panes
// come and go.
package live

import (
	"context"
	"encoding/json"
	"sync"

	herdr "github.com/cyperx84/herdr-api"
)

// globalSubscriptions are the session-wide events worth a re-render: anything
// that changes which workspaces or panes exist, which one has focus, or
// whether a pane has acquired an agent.
//
// pane.agent_status_changed is deliberately NOT here — it does not exist as a
// global subscription, which is the whole reason per-pane watches are needed.
var globalSubscriptions = []herdr.Subscription{
	{Type: "pane.created"},
	{Type: "pane.updated"},
	{Type: "pane.closed"},
	{Type: "pane.exited"},
	{Type: "pane.agent_detected"},
	{Type: "pane.focused"},
	{Type: "workspace.created"},
	{Type: "workspace.updated"},
	{Type: "workspace.renamed"},
	{Type: "workspace.moved"},
	{Type: "workspace.reordered"},
	{Type: "workspace.closed"},
	{Type: "workspace.focused"},
}

// subscriber is the part of herdr-api's client this package needs, named as an
// interface so tests can drive the bus without a daemon.
type subscriber interface {
	Subscribe(ctx context.Context, subs ...herdr.Subscription) (*herdr.Stream, error)
}

// eventBus fans every subscription's events into one channel.
type eventBus struct {
	client subscriber
	out    chan herdr.Event

	mu      sync.Mutex
	global  *herdr.Stream
	perPane map[string]*herdr.Stream
}

func newEventBus(ctx context.Context, client subscriber) (*eventBus, error) {
	b := &eventBus{
		client:  client,
		out:     make(chan herdr.Event, 64),
		perPane: map[string]*herdr.Stream{},
	}
	s, err := client.Subscribe(ctx, globalSubscriptions...)
	if err != nil {
		return nil, err
	}
	b.global = s
	b.pump(s)
	return b, nil
}

// pump forwards one stream's events into the shared channel. Every stream gets
// its own goroutine and ordering across streams is not guaranteed, which is
// fine: nothing here derives state from events. An event only ever means "go
// look again", and the authoritative answer always comes from a fresh
// snapshot.
func (b *eventBus) pump(s *herdr.Stream) {
	go func() {
		for ev := range s.Events() {
			select {
			case b.out <- ev:
			default:
				// A full buffer means re-renders are already queued faster
				// than they can be served. Dropping is correct rather than
				// lossy: the next render reads current state regardless of
				// how many events prompted it.
			}
		}
	}()
}

// watch opens a pane.agent_status_changed subscription for paneID unless one
// is already open. Idempotent because pane.created and pane.agent_detected can
// both name the same pane, and a re-render can rediscover a pane already
// watched.
func (b *eventBus) watch(ctx context.Context, paneID string) {
	if paneID == "" {
		return
	}
	b.mu.Lock()
	_, already := b.perPane[paneID]
	b.mu.Unlock()
	if already {
		return
	}

	s, err := b.client.Subscribe(ctx, herdr.Subscription{
		Type:   "pane.agent_status_changed",
		PaneID: paneID,
	})
	if err != nil {
		// Not fatal. A pane whose watch failed still surfaces through the
		// global topology events and through whatever re-render happens next;
		// it just will not trigger one by itself.
		return
	}
	b.mu.Lock()
	b.perPane[paneID] = s
	b.mu.Unlock()
	b.pump(s)
}

// unwatch closes and forgets a pane's subscription once the pane is gone.
func (b *eventBus) unwatch(paneID string) {
	b.mu.Lock()
	s, ok := b.perPane[paneID]
	delete(b.perPane, paneID)
	b.mu.Unlock()
	if ok {
		s.Close()
	}
}

// Events delivers every event from every stream this bus owns.
func (b *eventBus) Events() <-chan herdr.Event { return b.out }

// Close ends every subscription. The shared channel is deliberately never
// closed: pump goroutines exit on their own once their streams close, and
// closing a channel other goroutines may still send on is a race.
func (b *eventBus) Close() {
	if b.global != nil {
		b.global.Close()
	}
	b.mu.Lock()
	streams := make([]*herdr.Stream, 0, len(b.perPane))
	for _, s := range b.perPane {
		streams = append(streams, s)
	}
	b.perPane = map[string]*herdr.Stream{}
	b.mu.Unlock()
	for _, s := range streams {
		s.Close()
	}
}

// paneIDOf digs the pane id out of a raw event.
//
// Two shapes, matching herdr's own inconsistency: pane_created and
// pane_updated nest a full pane object under "pane", while every other event
// puts pane_id at the top level.
func paneIDOf(ev herdr.Event) string {
	switch ev.Kind {
	case "pane_created", "pane_updated":
		var d struct {
			Pane struct {
				ID string `json:"pane_id"`
			} `json:"pane"`
		}
		if err := json.Unmarshal(ev.Data, &d); err == nil {
			return d.Pane.ID
		}
	default:
		var d struct {
			PaneID string `json:"pane_id"`
		}
		if err := json.Unmarshal(ev.Data, &d); err == nil {
			return d.PaneID
		}
	}
	return ""
}
