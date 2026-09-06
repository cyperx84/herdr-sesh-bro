// `record-event` is the herdr [[events]] hook that gives sesh-bro a clock.
//
// herdr exposes no timestamp for when an agent entered its state — see
// internal/attention's package comment for the discussion history — so the
// only way to answer "blocked for how long" is to notice the transition when
// it happens. herdr invokes a plugin command per event and, verified against
// 0.8.2, does so for every pane with no per-pane registration, unlike
// events.subscribe whose status subscription demands a pane_id. So this is a
// short-lived process per state change rather than a resident daemon.
//
// It never fails. A hook that exits non-zero fills herdr's plugin log with
// noise about a feature whose entire value is a small grey badge, and there is
// nothing a user could do about it. Every error path here degrades to "no
// recording", which degrades to "no badge".
package main

import (
	"context"
	"encoding/json"
	"time"
)

// eventEnvelope is the shape of $HERDR_PLUGIN_EVENT_JSON. herdr names the
// event underscored inside the payload even though the manifest's `on` key
// must be dotted; only the data is read here, so the discrepancy does not
// matter beyond being worth knowing.
type eventEnvelope struct {
	Event string `json:"event"`
	Data  struct {
		Type        string `json:"type"`
		PaneID      string `json:"pane_id"`
		WorkspaceID string `json:"workspace_id"`
		AgentStatus string `json:"agent_status"`
	} `json:"data"`
}

func cmdRecordEvent(ctx context.Context, env *appEnv) int {
	raw := env.getenv("HERDR_PLUGIN_EVENT_JSON")
	if raw == "" {
		return 0
	}
	var ev eventEnvelope
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		return 0
	}

	path := statePath(env.getenv)
	if path == "" {
		return 0
	}

	state := attentionLoad(path)
	now := time.Now()

	switch {
	case ev.Data.AgentStatus != "" && ev.Data.PaneID != "":
		state.ApplyStatus(ev.Data.PaneID, ev.Data.WorkspaceID, ev.Data.AgentStatus, now)
	case ev.Data.WorkspaceID != "":
		// A workspace focus with no agent status: this is what makes `last`
		// a real most-recently-used jump rather than "the highest-numbered
		// other workspace", which is what it silently was before.
		state.ApplyFocus(ev.Data.WorkspaceID, now)
	default:
		return 0
	}

	_ = attentionSave(path, state)
	return 0
}
