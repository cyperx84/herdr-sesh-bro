// Package herdrx is sesh-bro's only bridge to the herdr daemon.
//
// Every `herdr` CLI invocation and every `jq` subprocess the bash used
// (docs/BEHAVIOUR.md §6) collapses to a direct call through
// github.com/cyperx84/herdr-api's socket client. This file wraps the eleven
// calls sesh-bro actually makes (§6's mapping table, rows #1-#11), plus
// deliberate feature additions beyond that parity set that each carry their
// own justification at the call site: CloseWorkspace
// (docs/COMPETITIVE-DEMAND.md #1) and CreateWorktree
// (cmd/sesh-bro/worktree.go's doc comment — turning `worktree` into a real
// `git worktree` instead of an issue-labelled plain workspace). Two calls
// from §6's original table are deliberately NOT here:
//
//   - #12 plugin.pane.open (`open`'s implementation) has no herdr-api
//     method — it stays a `herdr` CLI shell-out, owned by the command
//     layer, per §6's own note ("do not silently change what open does").
//   - #13 (`command -v $HERDR`) is moot for a socket-dialing binary; Alive,
//     below, is its replacement.
//
// row.go and select.go hold the pure selection/formatting logic (no herdr
// I/O) that reproduces the bash's jq pipelines over the data these calls
// return.
//
// NOT owned here, and not yet owned by anything as far as this package's
// author could tell: the pane-list file cache (§7.3) that let the bash's
// fzf reload binds avoid re-querying the daemon on every keypress, including
// its documented-buggy TTL bucket semantics (§9 S4). It sits between "talks
// to herdr" (this package) and "CLI process lifecycle" (the command layer),
// and belongs to whichever package owns `list`'s reload path — flag this
// loudly to whoever claims that package.
package herdrx

import (
	"context"
	"fmt"

	herdr "github.com/cyperx84/herdr-api"
)

// strippedANSI pins strip_ansi=false explicitly for every read this package
// makes. herdr-api's PaneReadParams and AgentReadParams both default
// strip_ansi to true server-side, so a nil/omitted pointer here would
// silently strip the colour the preview exists to show — exactly the trap
// BEHAVIOUR.md §6 flags for this port ("--ansi corresponds to format=ansi
// PLUS strip_ansi=false"). Package-level so every read call shares one
// address instead of allocating a fresh bool per call.
var strippedANSI = false

// Client is sesh-bro's typed view of the herdr socket API: the eleven calls
// named in BEHAVIOUR.md §6's mapping table, plus two read helpers that pin
// the --ansi flag's actual wire params (Source/Format/StripANSI) so a caller
// can't forget the StripANSI pointer and silently lose colour.
//
// It holds no state beyond the underlying herdr.Client, which itself holds
// no connection — herdr-api dials per call, matching the protocol's
// one-request-per-connection model (its own doc comment explains why).
type Client struct {
	c *herdr.Client
}

// New wraps an existing herdr-api client. Use this when the caller already
// has one (e.g. shared with an event subscription elsewhere in the
// process); use Open for the common case of "the socket this process was
// launched into".
func New(c *herdr.Client) *Client { return &Client{c: c} }

// Open returns a Client dialing $HERDR_SOCKET_PATH, the socket herdr
// injects into every pane it spawns.
func Open() (*Client, error) {
	c, err := herdr.Open()
	if err != nil {
		return nil, fmt.Errorf("herdrx: open: %w", err)
	}
	return New(c), nil
}

// OpenPath is Open's explicit-path counterpart: a Client dialing the socket
// at path instead of the one the environment names. Production code never
// needs it (herdr injects HERDR_SOCKET_PATH into every pane it spawns); it
// exists so the command layer can resolve that variable through its own
// threaded getenv seam and point at the tests-only fake server
// (internal/herdrx/herdrtest) without mutating process-wide state — see
// cmd/sesh-bro/herdrconn.go's openHerdr for the one production use. The
// error return exists to mirror Open's signature (callers treat the two
// uniformly); the only failure is an empty path, which would otherwise
// silently dial herdr-api's no-socket error from a confusing call site.
func OpenPath(path string) (*Client, error) {
	if path == "" {
		return nil, fmt.Errorf("herdrx: open: empty socket path")
	}
	return New(herdr.New(path)), nil
}

// Alive reports whether the herdr daemon responds. It matches the bash's
// herdr_ok() literally (BEHAVIOUR.md §7.1): a workspace.list call, result
// discarded. This is deliberately NOT a Ping/CheckProtocol call — the bash
// never verified protocol compatibility before proceeding, and adding that
// gate here would be a new failure mode this port doesn't need. (A protocol
// check may be a good idea; if so, it's a separate, deliberate change, not
// an accident of "doing it properly" during a rewrite.)
func (c *Client) Alive(ctx context.Context) bool {
	_, err := c.c.WorkspaceList(ctx)
	return err == nil
}

// ListWorkspaces is `herdr workspace list` (BEHAVIOUR.md §6 #1).
func (c *Client) ListWorkspaces(ctx context.Context) ([]herdr.Workspace, error) {
	ws, err := c.c.WorkspaceList(ctx)
	if err != nil {
		return nil, fmt.Errorf("herdrx: list workspaces: %w", err)
	}
	return ws, nil
}

// GetWorkspace is `herdr workspace get <id>` (BEHAVIOUR.md §6 #2).
func (c *Client) GetWorkspace(ctx context.Context, id string) (herdr.Workspace, error) {
	w, err := c.c.WorkspaceGet(ctx, id)
	if err != nil {
		return herdr.Workspace{}, fmt.Errorf("herdrx: get workspace %s: %w", id, err)
	}
	return w, nil
}

// FocusWorkspace is `herdr workspace focus <id>` (BEHAVIOUR.md §6 #3).
func (c *Client) FocusWorkspace(ctx context.Context, id string) error {
	if err := c.c.WorkspaceFocus(ctx, id); err != nil {
		return fmt.Errorf("herdrx: focus workspace %s: %w", id, err)
	}
	return nil
}

// CloseWorkspace is `workspace.close` — closing a workspace from the picker
// (docs/COMPETITIVE-DEMAND.md #1: "Close / remove a workspace from the
// picker"). The bash has no equivalent to cite; four independent competitor
// plugins converge on this being table-stakes and sesh-bro had no close
// action at all, so this is net-new rather than ported.
//
// herdr-api's WorkspaceClose has the same "no dedicated response payload"
// shape as WorkspaceFocus (see herdr-api's own doc comment on it) — success
// is nil error, and a workspace.closed event, not a return value, is what
// tells any other observer it happened. Closing every tab/pane inside the
// workspace is the daemon's job, not this package's.
func (c *Client) CloseWorkspace(ctx context.Context, id string) error {
	if err := c.c.WorkspaceClose(ctx, id); err != nil {
		return fmt.Errorf("herdrx: close workspace %s: %w", id, err)
	}
	return nil
}

// CreateWorkspace is `herdr workspace create --cwd <cwd> --label <label>
// --focus` (BEHAVIOUR.md §6 #4). Focus is not a parameter here because the
// bash never creates an unfocused workspace — all four call sites (connect
// dir, create, root, worktree) pass --focus unconditionally.
func (c *Client) CreateWorkspace(ctx context.Context, cwd, label string) (herdr.WorkspaceCreated, error) {
	res, err := c.c.WorkspaceCreate(ctx, herdr.WorkspaceCreateParams{
		CWD:   &cwd,
		Label: &label,
		Focus: true,
	})
	if err != nil {
		return herdr.WorkspaceCreated{}, fmt.Errorf("herdrx: create workspace for %s: %w", cwd, err)
	}
	return res, nil
}

// CreateWorktree is `worktree.create` — creates a real git worktree on
// branch, rooted in the repo at cwd, and opens it as a focused workspace.
// This is `worktree`'s (cmd/sesh-bro/worktree.go) upgrade from BEHAVIOUR.md
// §2.7's original "issue-labelled plain workspace, no git worktree ever
// created" bash behaviour — see that file's doc comment for the full
// rationale.
//
// cwd is NOT optional the way WorktreeCreateParams' pointer field might
// suggest. herdr-api's own WorktreeCreateParams doc comment: "CWD picks
// which repo... if WorkspaceID doesn't already imply one" — leave both
// unset and worktree.create resolves against whichever workspace the
// daemon currently has focused, which silently creates the worktree in
// an unrelated repo whenever this process isn't itself running inside the
// target repo's workspace. That is the common case for `worktree`: it is
// invoked from a ctrl-click in ANY pane, not necessarily one already
// sitting in the issue's repo. This was observed for real in a sibling
// project. Every caller of this method must pass the repo it means.
func (c *Client) CreateWorktree(ctx context.Context, cwd, branch, label string) (herdr.WorktreeCreated, error) {
	res, err := c.c.WorktreeCreate(ctx, herdr.WorktreeCreateParams{
		CWD:    &cwd,
		Branch: &branch,
		Label:  &label,
		Focus:  true,
	})
	if err != nil {
		return herdr.WorktreeCreated{}, fmt.Errorf("herdrx: create worktree for %s on %s: %w", cwd, branch, err)
	}
	return res, nil
}

// ListAgents is `herdr agent list` (BEHAVIOUR.md §6 #5).
func (c *Client) ListAgents(ctx context.Context) ([]herdr.Agent, error) {
	ag, err := c.c.AgentList(ctx)
	if err != nil {
		return nil, fmt.Errorf("herdrx: list agents: %w", err)
	}
	return ag, nil
}

// GetAgent is `herdr agent get <target>` (BEHAVIOUR.md §6 #6).
func (c *Client) GetAgent(ctx context.Context, target string) (herdr.Agent, error) {
	a, err := c.c.AgentGet(ctx, target)
	if err != nil {
		return herdr.Agent{}, fmt.Errorf("herdrx: get agent %s: %w", target, err)
	}
	return a, nil
}

// FocusAgent is `herdr agent focus <target>` (BEHAVIOUR.md §6 #7).
func (c *Client) FocusAgent(ctx context.Context, target string) error {
	if err := c.c.AgentFocus(ctx, target); err != nil {
		return fmt.Errorf("herdrx: focus agent %s: %w", target, err)
	}
	return nil
}

// ReadAgentVisibleANSI is `herdr agent read <target> --source visible
// --ansi` (BEHAVIOUR.md §6 #8): the agent pane's visible screen, ANSI codes
// intact. Returns Text only — Truncated/Revision aren't part of what the
// preview shows.
func (c *Client) ReadAgentVisibleANSI(ctx context.Context, target string) (string, error) {
	res, err := c.c.AgentRead(ctx, herdr.AgentReadParams{
		Target:    target,
		Source:    herdr.ReadSourceVisible,
		Format:    herdr.ReadFormatANSI,
		StripANSI: &strippedANSI,
	})
	if err != nil {
		return "", fmt.Errorf("herdrx: read agent %s: %w", target, err)
	}
	return res.Text, nil
}

// ListPanes is `herdr pane list` (all workspaces, when workspaceID == "")
// or `herdr pane list --workspace <id>` (BEHAVIOUR.md §6 #9/#10 — the same
// underlying call, scoped differently). See this file's package doc comment
// for why the bash's pane-cache wrapper around the all-workspaces call
// isn't reproduced here.
func (c *Client) ListPanes(ctx context.Context, workspaceID string) ([]herdr.Pane, error) {
	panes, err := c.c.PaneList(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("herdrx: list panes: %w", err)
	}
	return panes, nil
}

// ReadPaneVisibleANSI is `herdr pane read <pane> --source visible --ansi`
// (BEHAVIOUR.md §6 #11): a pane's visible screen, ANSI codes intact.
func (c *Client) ReadPaneVisibleANSI(ctx context.Context, paneID string) (string, error) {
	res, err := c.c.PaneRead(ctx, herdr.PaneReadParams{
		PaneID:    paneID,
		Source:    herdr.ReadSourceVisible,
		Format:    herdr.ReadFormatANSI,
		StripANSI: &strippedANSI,
	})
	if err != nil {
		return "", fmt.Errorf("herdrx: read pane %s: %w", paneID, err)
	}
	return res.Text, nil
}

// ShowToast is `notification.show` — how a background command tells the human
// what it just did, since a keybinding's output goes nowhere the user is
// looking.
//
// The returned reason matters more than the bool: herdr suppresses
// notifications for several reasons, and the one that bites here is that it
// will not toast the tab you are already looking at. So a caller that both
// toasts and focuses must toast FIRST — focus makes the destination the active
// tab, after which the toast about it is dropped. `shown == false` with reason
// "busy" or "disabled" is not an error, it just means the message needs
// another channel.
func (c *Client) ShowToast(ctx context.Context, title, body string) (herdr.NotificationShowResult, error) {
	params := herdr.NotificationShowParams{Title: title}
	if body != "" {
		params.Body = &body
	}
	res, err := c.c.NotificationShow(ctx, params)
	if err != nil {
		return res, fmt.Errorf("herdrx: show notification: %w", err)
	}
	return res, nil
}
