package herdrtest

import (
	"encoding/json"

	herdr "github.com/cyperx84/herdr-api"
)

// SnapshotFixtureAgent is a fixture agent carrying state_change_seq, the
// ordering field the 0.4.0 attention features key on. herdr's own Agent
// type does not expose it (herdr exposes no status timestamps — herdr
// discussions #707/#3619), so it rides along as an extra JSON key: today's
// herdr.Agent ignores it on decode, and the Phase 1 extension type
// (herdrx.Agent{herdr.Agent; StateChangeSeq}) will read it.
type SnapshotFixtureAgent struct {
	herdr.Agent
	StateChangeSeq uint64 `json:"state_change_seq"`
}

// SnapshotFixture is the fixture input for SnapshotJSON.
type SnapshotFixture struct {
	Workspaces []herdr.Workspace
	Agents     []SnapshotFixtureAgent
	// FocusedWorkspaceID may be "" — nothing focused, which the client
	// models as Snapshot's nil *string fields.
	FocusedWorkspaceID string
}

// SnapshotJSON marshals the fixture into a session.snapshot RESULT payload,
// ready to hand to Handle("session.snapshot", …). The envelope and field
// names match herdr-api's session.go exactly — its SessionSnapshot decodes
// {"snapshot": Snapshot}, and Snapshot reuses the Workspace/Pane/Agent
// shapes of workspace.list/pane.list/agent.list — so a fixture produced
// here decodes through the real client with no test-side adapter.
//
// Agents are marshalled as flat maps rather than typed structs so
// state_change_seq rides along next to the herdr.Agent wire fields (an
// embedded struct's tags and a sibling key cannot be expressed in one
// struct literal without changing the shape the client decodes).
func (f SnapshotFixture) SnapshotJSON() json.RawMessage {
	var focused *string
	if f.FocusedWorkspaceID != "" {
		id := f.FocusedWorkspaceID
		focused = &id
	}

	agents := make([]map[string]any, 0, len(f.Agents))
	for _, a := range f.Agents {
		agentJSON, err := json.Marshal(a.Agent)
		if err != nil {
			// Marshal of a test-controlled struct; a failure is a test bug
			// and panicking names the fixture, not a mysterious empty
			// snapshot on the client side.
			panic("herdrtest: fixture agent marshal: " + err.Error())
		}
		m := map[string]any{}
		if err := json.Unmarshal(agentJSON, &m); err != nil {
			panic("herdrtest: fixture agent unmarshal: " + err.Error())
		}
		m["state_change_seq"] = a.StateChangeSeq
		agents = append(agents, m)
	}

	payload := struct {
		Snapshot struct {
			Version            string            `json:"version"`
			Protocol           int               `json:"protocol"`
			Workspaces         []herdr.Workspace `json:"workspaces"`
			Agents             []map[string]any  `json:"agents"`
			FocusedWorkspaceID *string           `json:"focused_workspace_id"`
		} `json:"snapshot"`
	}{}
	payload.Snapshot.Version = "0.8.2"
	payload.Snapshot.Protocol = herdr.Protocol
	payload.Snapshot.Workspaces = f.Workspaces
	payload.Snapshot.Agents = agents
	payload.Snapshot.FocusedWorkspaceID = focused

	b, err := json.Marshal(payload)
	if err != nil {
		panic("herdrtest: fixture marshal: " + err.Error())
	}
	return b
}
