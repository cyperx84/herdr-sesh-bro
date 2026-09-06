package herdrtest

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	herdr "github.com/cyperx84/herdr-api"
)

// The fidelity pin for this whole package: every test here drives the server
// with herdr-api's REAL client, not a hand-rolled one. If client.go's wire
// behaviour ever changes, these tests break here — at the fake — instead of
// as mysterious failures in every downstream command test.

// TestCallRoundTrip: an ordinary method answers with an id-matched result
// frame on a fresh connection, which is the one-request-per-connection
// contract Call dials per request for.
func TestCallRoundTrip(t *testing.T) {
	s := Start(t)
	s.Handle("workspace.list", func(json.RawMessage) (any, error) {
		return map[string]any{"workspaces": []map[string]any{
			{"workspace_id": "w1", "label": "alpha"},
		}}, nil
	})

	c := herdr.New(s.Path())
	ctx := context.Background()
	ws, err := c.WorkspaceList(ctx)
	if err != nil {
		t.Fatalf("WorkspaceList: %v", err)
	}
	if len(ws) != 1 || ws[0].ID != "w1" || ws[0].Label != "alpha" {
		t.Fatalf("workspaces = %+v, want one workspace w1/alpha", ws)
	}

	// Calls recorded what the client actually sent.
	calls := s.Calls("workspace.list")
	if len(calls) != 1 {
		t.Fatalf("Calls(workspace.list) = %d entries, want 1", len(calls))
	}
	if string(calls[0].Params) != "{}" {
		t.Errorf("params = %s, want {} — writeRequest substitutes {} for a nil params", string(calls[0].Params))
	}
}

// TestCallErrorRoundTrip: a Handler error becomes the error frame
// {"code","message"} and surfaces as herdr-api's *APIError, which callers
// branch on by Code.
func TestCallErrorRoundTrip(t *testing.T) {
	s := Start(t)
	s.Handle("workspace.close", func(json.RawMessage) (any, error) {
		return nil, &APIError{Code: "workspace_is_current", Message: "refusing"}
	})

	c := herdr.New(s.Path())
	err := c.WorkspaceClose(context.Background(), "w1")
	var api *herdr.APIError
	if !errors.As(err, &api) {
		t.Fatalf("error = %v, want *herdr.APIError", err)
	}
	if api.Code != "workspace_is_current" || api.Message != "refusing" {
		t.Fatalf("APIError = %+v, want code workspace_is_current / refusing", api)
	}
}

// TestUnhandledMethodFailsLoudly: the deliberate absence of a silent-empty
// fallback — a test that mis-models the wire traffic must fail with the
// method name, not pass green on a zero result.
func TestUnhandledMethodFailsLoudly(t *testing.T) {
	s := Start(t)
	_, err := herdr.New(s.Path()).WorkspaceList(context.Background())
	if err == nil {
		t.Fatal("call to an unhandled method succeeded, want a loud error")
	}
	if !strings.Contains(err.Error(), "test_unhandled") || !strings.Contains(err.Error(), "workspace.list") {
		t.Fatalf("error = %v, want the unhandled-method frame naming workspace.list", err)
	}
}

// TestSubscribeAckAndEvent: events.subscribe acks (id-matched response,
// which Subscribe consumes before returning) and then the connection stays
// open, delivering Emit's frames as normalized Events.
func TestSubscribeAckAndEvent(t *testing.T) {
	s := Start(t)
	c := herdr.New(s.Path())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := c.Subscribe(ctx, herdr.Subscription{Type: "pane.agent_status_changed", PaneID: "p1"})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer stream.Close()

	// Emit the dotted spelling — the server passes it through verbatim and
	// the CLIENT normalizes (normalizeEventKind), which is the division of
	// labour these tests pin.
	s.Emit("pane.agent_status_changed", map[string]any{
		"pane_id":      "p1",
		"agent_status": "blocked",
	})

	select {
	case ev, ok := <-stream.Events():
		if !ok {
			t.Fatal("Events closed before the emitted event arrived")
		}
		if ev.Kind != "pane_agent_status_changed" {
			t.Errorf("Kind = %q, want the underscored spelling the client normalizes to", ev.Kind)
		}
		var data struct {
			PaneID string `json:"pane_id"`
			Status string `json:"agent_status"`
		}
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			t.Fatalf("event data unmarshal: %v", err)
		}
		if data.PaneID != "p1" || data.Status != "blocked" {
			t.Errorf("data = %+v, want p1/blocked", data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no event arrived within 5s — the subscribe connection is not being held open for Emit")
	}

	// The subscriptions were recorded, for tests that assert what a
	// command subscribed to.
	subs := s.Subscriptions()
	if len(subs) != 1 || subs[0].Type != "pane.agent_status_changed" || subs[0].PaneID != "p1" {
		t.Fatalf("Subscriptions() = %+v, want one pane.agent_status_changed/p1", subs)
	}
}

// TestConcurrentCalls: Call dials a fresh connection per request, so the
// server must accept and serve connections concurrently — this test is the
// race-detector bait for the calls/subs bookkeeping.
func TestConcurrentCalls(t *testing.T) {
	s := Start(t)
	s.Handle("workspace.get", func(p json.RawMessage) (any, error) {
		var params struct {
			WorkspaceID string `json:"workspace_id"`
		}
		_ = json.Unmarshal(p, &params)
		return map[string]any{"workspace": map[string]any{
			"workspace_id": params.WorkspaceID,
		}}, nil
	})

	c := herdr.New(s.Path())
	const n = 16
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			id := "w" + strconv.Itoa(i)
			w, err := c.WorkspaceGet(context.Background(), id)
			if err != nil {
				errs <- err
				return
			}
			if w.ID != id {
				errs <- errors.New("wrong workspace id: " + w.ID)
				return
			}
			errs <- nil
		}(i)
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent call %d: %v", i, err)
		}
	}
	if got := len(s.Calls("workspace.get")); got != n {
		t.Errorf("Calls(workspace.get) = %d, want %d", got, n)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
