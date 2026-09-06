package herdrx

import (
	"context"
	"fmt"

	herdr "github.com/cyperx84/herdr-api"
)

// Agent is herdr's AgentInfo as sesh-bro needs it: herdr-api's Agent plus the
// two fields its struct does not decode.
//
// herdr-api pins the field set it needed and stopped there; `agent.list` and
// `session.snapshot` both return more. StateChangeSeq is the one that matters
// here — see its own comment. Embedding rather than forking keeps every
// existing helper (agentTarget, agentLabel, agentDetail, NormalizeStatus)
// working on the promoted fields, so this type costs nothing at the call
// sites that don't care about the additions.
type Agent struct {
	herdr.Agent

	// StateChangeSeq is herdr's global monotonic counter, stamped on an agent
	// each time its state changes. It is NOT a timestamp and NOT per-pane: two
	// agents' values are comparable only as "which changed more recently".
	// That is exactly enough to break ties within a status rank so the agent
	// that just became blocked leads the ones that have been blocked a while,
	// and it is the only recency signal herdr exposes — there is no
	// state_changed_at anywhere in the 0.8.2 schema (herdr discussions #707,
	// #3619). Anything that needs an actual duration has to keep its own clock.
	StateChangeSeq uint64 `json:"state_change_seq"`

	// TerminalTitle is the raw title, retained alongside herdr-api's
	// TerminalTitleStripped so a future preview/label can show the unstripped
	// form. Row assembly deliberately keeps using the stripped one
	// (BEHAVIOUR.md §2.2.5).
	TerminalTitle string `json:"terminal_title"`
}

// Snapshot is session.snapshot's result: the whole session in one round trip.
//
// The fields are declared rather than embedding herdr.Snapshot because Agents
// has to be []Agent (above). Embedding and shadowing would leave two fields
// competing for the same `agents` JSON tag, which encoding/json resolves by
// depth — correct, but a rule nobody should have to recall while reading this.
type Snapshot struct {
	Version  string `json:"version"`
	Protocol int    `json:"protocol"`

	Workspaces []herdr.Workspace `json:"workspaces"`
	Tabs       []herdr.Tab       `json:"tabs"`
	Panes      []herdr.Pane      `json:"panes"`
	Agents     []Agent           `json:"agents"`

	// Focused*ID is nil when nothing in that scope has focus.
	FocusedWorkspaceID *string `json:"focused_workspace_id"`
	FocusedTabID       *string `json:"focused_tab_id"`
	FocusedPaneID      *string `json:"focused_pane_id"`
}

// FocusedWorkspace is the focused workspace id, or "" when nothing is focused.
func (s Snapshot) FocusedWorkspace() string { return deref(s.FocusedWorkspaceID) }

// FocusedPane is the focused pane id, or "" when nothing is focused.
func (s Snapshot) FocusedPane() string { return deref(s.FocusedPaneID) }

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// AgentPanes lists the pane ids currently hosting an agent.
//
// This exists for the live picker's event bus: herdr's per-pane
// `pane.agent_status_changed` subscription requires a pane_id and offers no
// wildcard, so following a whole session means opening one subscription per
// agent pane and adding more as panes appear.
func (s Snapshot) AgentPanes() []string {
	panes := make([]string, 0, len(s.Agents))
	for _, a := range s.Agents {
		if a.PaneID != "" {
			panes = append(panes, a.PaneID)
		}
	}
	return panes
}

// SessionSnapshot is `session.snapshot` — the entire session state in one
// call, replacing the four separate reads (`workspace.list`, `agent.list`, and
// `pane.list` twice) that `list` used to make, plus the file cache that
// existed to soften them.
//
// It goes through Client.Call rather than herdr-api's own SessionSnapshot
// because that returns herdr.Snapshot, whose Agents are herdr.Agent and so
// drop state_change_seq — the one field the ordering depends on.
func (c *Client) SessionSnapshot(ctx context.Context) (Snapshot, error) {
	var res struct {
		Snapshot Snapshot `json:"snapshot"`
	}
	if err := c.c.Call(ctx, "session.snapshot", struct{}{}, &res); err != nil {
		return Snapshot{}, fmt.Errorf("herdrx: session snapshot: %w", err)
	}
	return res.Snapshot, nil
}
