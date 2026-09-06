package main

// Command-level success-path tests against the fake herdr socket server
// (internal/herdrx/herdrtest) — the tests this repo could never have before
// 0.4.0. The old bash harness's mock-herdr faked the herdr CLI, which cannot
// drive a binary that dials the socket directly; these instead inject
// HERDR_SOCKET_PATH through the same fakeEnv seam every other env var uses
// (openHerdr resolves it — see herdrconn.go), so the FULL production path
// runs: run() → command → dependency gate → herdrx → herdr-api client →
// unix socket → herdrtest. Nothing is stubbed inside the process.

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx/herdrtest"
)

// fakeHerdrEnv is the environment a test needs to point the binary at a
// herdrtest server: the socket path, and a HERDR_BIN_PATH that exists so
// the dependency gates' exec.LookPath (external.RequireHerdr) passes. A
// real "herdr" is NOT assumed on PATH — not on dev machines, not on CI
// runners. /bin/sh is the one executable both CI matrices guarantee.
func fakeHerdrEnv(s *herdrtest.Server) map[string]string {
	return map[string]string{
		"HERDR_SOCKET_PATH": s.Path(),
		"HERDR_BIN_PATH":    "/bin/sh",
	}
}

// TestListWorkspacesAgainstFakeHerdr is the full success path for `list`:
// dependency gate (herdr binary found + daemon Alive), the real
// workspace.list RPC through the socket, row assembly, and --json output —
// every layer real except the daemon itself.
func TestListWorkspacesAgainstFakeHerdr(t *testing.T) {
	s := herdrtest.Start(t)
	s.Handle("workspace.list", func(json.RawMessage) (any, error) {
		return map[string]any{"workspaces": []map[string]any{{
			"workspace_id": "w1",
			"number":       1,
			"label":        "alpha",
			"pane_count":   2,
			"tab_count":    1,
			"agent_status": "working",
		}}}, nil
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"list", "--workspaces", "--json"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	want := `{
  "type": "workspace",
  "target": "w1",
  "status": "working",
  "label": "alpha",
  "detail": "2p/1t"
}
`
	if got := stdout.String(); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}

	// The server saw the traffic it should have: Alive() gates `list` with
	// one workspace.list and listWorkspaces makes another (BEHAVIOUR.md
	// §7.1's herdr_ok is literally a workspace.list with the result
	// discarded), so exactly two calls — the RPC count is part of what
	// these tests pin.
	if calls := s.Calls("workspace.list"); len(calls) != 2 {
		t.Errorf("Calls(workspace.list) = %d, want 2 (Alive gate + listWorkspaces)", len(calls))
	}
}

// TestListAgentsAgainstFakeHerdr pins the agent block of the same path:
// agent.list through the socket and the agent row shape (target is the
// NAME, detail is "<kind> · <title|cwd>" — §2.2.5's jq pipeline).
func TestListAgentsAgainstFakeHerdr(t *testing.T) {
	s := herdrtest.Start(t)
	kind := "claude"
	// `list` gates on the daemon being reachable before it reads anything, and
	// that probe is a workspace.list whose result is discarded (herdrx.Alive,
	// BEHAVIOUR.md §7.1). Without a handler for it the fake answers "unknown
	// method", Alive reports false, and the command exits 1 with "herdr daemon
	// is not responding" before agent.list is ever called.
	s.Handle("workspace.list", func(json.RawMessage) (any, error) {
		return map[string]any{"workspaces": []map[string]any{}}, nil
	})
	s.Handle("agent.list", func(json.RawMessage) (any, error) {
		return map[string]any{"agents": []map[string]any{{
			"name":        "builder",
			"agent":       kind,
			"agent_status": "blocked",
			"pane_id":     "p1",
			"workspace_id": "w1",
			"cwd":         "/tmp/x",
		}}}, nil
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"list", "--agents", "--json"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	want := `{
  "type": "agent",
  "target": "builder",
  "status": "blocked",
  "label": "builder",
  "detail": "claude · /tmp/x"
}
`
	if got := stdout.String(); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if calls := s.Calls("agent.list"); len(calls) != 1 {
		t.Errorf("Calls(agent.list) = %d, want 1", len(calls))
	}
}

// TestConnectWorkspaceAgainstFakeHerdr is `connect`'s success path: no
// up-front dependency gate (§7.2 — connect never checks, it just fails if
// the call fails), one workspace.focus RPC with the workspace id as
// params, exit 0.
func TestConnectWorkspaceAgainstFakeHerdr(t *testing.T) {
	s := herdrtest.Start(t)
	s.Handle("workspace.focus", func(params json.RawMessage) (any, error) {
		// Echo the workspace_id back through Calls for the assertion below;
		// the real server's response is event-only (no result payload).
		return map[string]any{"ok": true}, nil
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"connect", "workspace", "w1"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty (connect is silent on success)", stdout.String())
	}
	calls := s.Calls("workspace.focus")
	if len(calls) != 1 {
		t.Fatalf("Calls(workspace.focus) = %d, want 1", len(calls))
	}
	// workspace.focus's params are {"workspace_id": "w1"} — assert on the
	// decoded shape rather than raw bytes, so a key-order change in
	// herdr-api doesn't break this test for no reason.
	var params struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal(calls[0].Params, &params); err != nil {
		t.Fatalf("focus params unmarshal: %v", err)
	}
	if params.WorkspaceID != "w1" {
		t.Errorf("focus params = %+v, want workspace_id w1", params)
	}
}

// TestListDaemonDownReportsGate keeps the failure side honest: with the
// socket path pointing at nothing, `list` must fail through the dependency
// gate ("daemon is not responding"), not crash — proving the getenv
// threading didn't invent a new failure mode (BEHAVIOUR.md §7.1).
func TestListDaemonDownReportsGate(t *testing.T) {
	var stdout, stderr bytes.Buffer
	env := map[string]string{
		"HERDR_SOCKET_PATH": "/nonexistent-herdr-socket-for-tests/sock",
		"HERDR_BIN_PATH":    "/bin/sh",
	}
	code := run([]string{"list"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(env))
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "daemon is not responding") {
		t.Fatalf("stderr = %q, want the daemon-not-responding gate message", stderr.String())
	}
}
