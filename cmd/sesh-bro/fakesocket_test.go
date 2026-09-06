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

	herdr "github.com/cyperx84/herdr-api"
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
// session.snapshot RPC through the socket, row assembly, and --json output —
// every layer real except the daemon itself.
func TestListWorkspacesAgainstFakeHerdr(t *testing.T) {
	s := herdrtest.Start(t)
	// `list` gates on the daemon being reachable before reading anything, and
	// that probe is a workspace.list whose result is discarded (herdrx.Alive,
	// BEHAVIOUR.md §7.1). It stayed a separate call when the data read
	// collapsed into one session.snapshot (§10), so the fake needs both.
	s.Handle("workspace.list", func(json.RawMessage) (any, error) {
		return map[string]any{"workspaces": []map[string]any{}}, nil
	})
	s.Handle("session.snapshot", func(json.RawMessage) (any, error) {
		return json.RawMessage(herdrtest.SnapshotFixture{
			Workspaces: []herdr.Workspace{{
				ID:        "w1",
				Number:    1,
				Label:     "alpha",
				PaneCount: 2,
				TabCount:  1,
				Status:    herdr.StatusWorking,
			}},
		}.SnapshotJSON()), nil
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
	if stdout.String() != want {
		t.Errorf("stdout =\n%q\nwant\n%q", stdout.String(), want)
	}
}

// The agent path, plus the ordering 0.4.0 exists for: `list --agents` must put
// the blocked agent first even though it sorts last alphabetically, and within
// the blocked rank the newest state_change_seq wins.
func TestListAgentsAgainstFakeHerdr(t *testing.T) {
	s := herdrtest.Start(t)
	kind := "claude"
	s.Handle("workspace.list", func(json.RawMessage) (any, error) {
		return map[string]any{"workspaces": []map[string]any{}}, nil
	})
	s.Handle("session.snapshot", func(json.RawMessage) (any, error) {
		return json.RawMessage(herdrtest.SnapshotFixture{
			Agents: []herdrtest.SnapshotFixtureAgent{
				{Agent: herdr.Agent{
					Name: "alpha", Agent: &kind, Status: herdr.StatusIdle,
					PaneID: "w1:p1", WorkspaceID: "w1", CWD: "/tmp/a",
				}, StateChangeSeq: 50},
				{Agent: herdr.Agent{
					Name: "zulu", Agent: &kind, Status: herdr.StatusBlocked,
					PaneID: "w1:p2", WorkspaceID: "w1", CWD: "/tmp/z",
				}, StateChangeSeq: 10},
				{Agent: herdr.Agent{
					Name: "mike", Agent: &kind, Status: herdr.StatusBlocked,
					PaneID: "w1:p3", WorkspaceID: "w1", CWD: "/tmp/m",
				}, StateChangeSeq: 20},
			},
		}.SnapshotJSON()), nil
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"list", "--agents", "--json"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	// mike before zulu: both blocked, mike changed more recently. alpha last:
	// idle outranks nothing.
	wantOrder := []string{`"target": "mike"`, `"target": "zulu"`, `"target": "alpha"`}
	pos := -1
	for _, frag := range wantOrder {
		i := strings.Index(stdout.String(), frag)
		if i < 0 {
			t.Fatalf("stdout missing %s:\n%s", frag, stdout.String())
		}
		if i < pos {
			t.Errorf("out of order at %s — want %v:\n%s", frag, wantOrder, stdout.String())
		}
		pos = i
	}
	if !strings.Contains(stdout.String(), `"detail": "claude · /tmp/m"`) {
		t.Errorf("agent detail wrong:\n%s", stdout.String())
	}
}

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
