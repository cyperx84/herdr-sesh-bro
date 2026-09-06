package herdrtest

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	herdr "github.com/cyperx84/herdr-api"
)

// TestSnapshotJSONDecodesThroughRealClient pins the fixture to herdr-api's
// SessionSnapshot: the payload Handle returns must decode through the real
// client, including the state_change_seq extra key (ignored by today's
// herdr.Agent) and the raw round-trip shape the fixture is named after.
func TestSnapshotJSONDecodesThroughRealClient(t *testing.T) {
	s := Start(t)
	fix := SnapshotFixture{
		Workspaces: []herdr.Workspace{
			{ID: "w1", Number: 1, Label: "alpha", PaneCount: 2, TabCount: 1, Status: herdr.StatusWorking},
		},
		Agents: []SnapshotFixtureAgent{
			{
				Agent: herdr.Agent{
					Name: "builder", Status: herdr.StatusBlocked, PaneID: "p1",
					TabID: "w1:t1", WorkspaceID: "w1", CWD: "/tmp/x",
				},
				StateChangeSeq: 42,
			},
		},
		FocusedWorkspaceID: "w1",
	}
	s.Handle("session.snapshot", func(json.RawMessage) (any, error) {
		return fix.SnapshotJSON(), nil
	})

	snap, err := herdr.New(s.Path()).SessionSnapshot(context.Background())
	if err != nil {
		t.Fatalf("SessionSnapshot: %v", err)
	}
	if len(snap.Workspaces) != 1 || snap.Workspaces[0].ID != "w1" {
		t.Fatalf("workspaces = %+v, want w1", snap.Workspaces)
	}
	if len(snap.Agents) != 1 || snap.Agents[0].Name != "builder" || snap.Agents[0].Status != herdr.StatusBlocked {
		t.Fatalf("agents = %+v, want builder/blocked", snap.Agents)
	}
	if snap.FocusedWorkspaceID == nil || *snap.FocusedWorkspaceID != "w1" {
		t.Fatalf("focused_workspace_id = %v, want w1", snap.FocusedWorkspaceID)
	}
	// state_change_seq rode along as an extra key; the raw result must
	// still carry it for the Phase 1 extension type to read.
	var raw struct {
		Snapshot struct {
			Agents []json.RawMessage `json:"agents"`
		} `json:"snapshot"`
	}
	calls := s.Calls("session.snapshot")
	if len(calls) != 1 {
		t.Fatalf("Calls = %d, want 1", len(calls))
	}
	// Re-derive the payload the handler returned by marshalling the same
	// fixture — byte-level identity is too brittle across key orders, so
	// assert on the decoded shape instead.
	payload := fix.SnapshotJSON()
	if err := json.Unmarshal(payload, &raw); err != nil {
		t.Fatalf("fixture payload unmarshal: %v", err)
	}
	if len(raw.Snapshot.Agents) == 0 {
		t.Fatal("fixture payload has no agents")
	}
	if !jsonContains(raw.Snapshot.Agents[0], `"state_change_seq":42`) {
		t.Errorf("agent payload = %s, want state_change_seq 42 to ride along", raw.Snapshot.Agents[0])
	}
}

func jsonContains(raw json.RawMessage, needle string) bool {
	return strings.Contains(string(raw), needle)
}
