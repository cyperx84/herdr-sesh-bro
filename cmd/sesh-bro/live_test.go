package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	herdr "github.com/cyperx84/herdr-api"

	"github.com/cyperx84/herdr-sesh-bro/internal/config"
	"github.com/cyperx84/herdr-sesh-bro/internal/fzfctl"
	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx/herdrtest"
)

func liveRenderer(t *testing.T, agents []herdrtest.SnapshotFixtureAgent) (*renderer, *herdrtest.Server) {
	t.Helper()
	s := herdrtest.Start(t)
	s.Handle("workspace.list", func(json.RawMessage) (any, error) {
		return map[string]any{"workspaces": []map[string]any{}}, nil
	})
	s.Handle("session.snapshot", func(json.RawMessage) (any, error) {
		return json.RawMessage(herdrtest.SnapshotFixture{Agents: agents}.SnapshotJSON()), nil
	})

	dir, err := os.MkdirTemp("", "lv")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	env := &appEnv{
		getenv:   fakeEnv(fakeHerdrEnv(s)),
		herdrBin: "/bin/sh",
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
		self:     "/nonexistent/sesh-bro",
	}
	return &renderer{
		env:        env,
		cfg:        config.Load(env.getenv),
		dir:        dir,
		fzf:        fzfctl.New(listenSocketPath(dir)),
		lastPushed: map[string]string{},
	}, s
}

func liveAgent(name, pane string, status herdr.AgentStatus, seq uint64) herdrtest.SnapshotFixtureAgent {
	kind := "claude"
	return herdrtest.SnapshotFixtureAgent{
		Agent: herdr.Agent{
			Name: name, PaneID: pane, WorkspaceID: "w1",
			Status: status, Agent: &kind, CWD: "/tmp/" + name,
		},
		StateChangeSeq: seq,
	}
}

// Every filter key must have a file waiting before it is pressed — that is
// what makes a filter keypress a file read instead of a process start and a
// daemon round trip.
func TestRenderAllWritesEveryView(t *testing.T) {
	r, _ := liveRenderer(t, []herdrtest.SnapshotFixtureAgent{
		liveAgent("stuck", "w1:p1", herdr.StatusBlocked, 10),
		liveAgent("calm", "w1:p2", herdr.StatusIdle, 20),
	})
	if _, err := r.renderAll(context.Background(), false); err != nil {
		t.Fatalf("renderAll: %v", err)
	}
	for _, view := range pickerViews {
		if _, err := os.Stat(rowsFile(r.dir, view)); err != nil {
			t.Errorf("view %q has no rows file: %v", view, err)
		}
	}

	blocked, err := os.ReadFile(rowsFile(r.dir, "blocked"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(blocked), "stuck") {
		t.Errorf("blocked view missing the blocked agent:\n%s", blocked)
	}
	if strings.Contains(string(blocked), "calm") {
		t.Errorf("blocked view leaked an idle agent:\n%s", blocked)
	}
}

// Every view carries the pinned counts row, since fzf's --header-lines=1
// applies to whatever stream is loaded — a view without one would promote a
// real candidate into the header and make it unselectable.
func TestRenderAllViewsCarryHeaderRow(t *testing.T) {
	r, _ := liveRenderer(t, []herdrtest.SnapshotFixtureAgent{
		liveAgent("stuck", "w1:p1", herdr.StatusBlocked, 10),
	})
	if _, err := r.renderAll(context.Background(), false); err != nil {
		t.Fatalf("renderAll: %v", err)
	}
	for _, view := range pickerViews {
		b, err := os.ReadFile(rowsFile(r.dir, view))
		if err != nil {
			t.Fatal(err)
		}
		first, _, _ := strings.Cut(string(b), "\n")
		if !strings.HasPrefix(first, "header\t") {
			t.Errorf("view %q first line = %q, want a header row", view, first)
		}
	}
}

// A re-render that produces identical bytes must not push anything: herdr
// emits pane_updated freely, and reloading the list under the user's cursor
// for an invisible change is exactly the jank this design exists to avoid.
func TestRenderAllSkipsUnchangedViews(t *testing.T) {
	r, _ := liveRenderer(t, []herdrtest.SnapshotFixtureAgent{
		liveAgent("stuck", "w1:p1", herdr.StatusBlocked, 10),
	})
	ctx := context.Background()
	if _, err := r.renderAll(ctx, false); err != nil {
		t.Fatalf("renderAll: %v", err)
	}
	before := r.lastPushed["all"]
	if before == "" {
		t.Fatal("nothing recorded as pushed for the all view")
	}

	// Make the file obviously stale; an unchanged render must NOT rewrite it.
	if err := os.WriteFile(rowsFile(r.dir, "all"), []byte("sentinel\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.renderAll(ctx, false); err != nil {
		t.Fatalf("renderAll: %v", err)
	}
	b, err := os.ReadFile(rowsFile(r.dir, "all"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "sentinel\n" {
		t.Errorf("unchanged view was rewritten, want the write skipped entirely")
	}
}

// The agent panes returned seed the watcher's per-pane subscriptions, and a
// missing one means that agent's status changes never reach the picker.
func TestRenderAllReportsAgentPanes(t *testing.T) {
	r, _ := liveRenderer(t, []herdrtest.SnapshotFixtureAgent{
		liveAgent("a", "w1:p1", herdr.StatusBlocked, 10),
		liveAgent("b", "w1:p2", herdr.StatusIdle, 20),
	})
	panes, err := r.renderAll(context.Background(), false)
	if err != nil {
		t.Fatalf("renderAll: %v", err)
	}
	if len(panes) != 2 {
		t.Fatalf("panes = %v, want both agent panes", panes)
	}
}

// readView defaults to the view the picker opens in, and rejects anything not
// on the known list rather than trusting a file the user could edit.
func TestReadViewDefaultsAndValidates(t *testing.T) {
	dir, err := os.MkdirTemp("", "vw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	if got := readView(dir); got != "all" {
		t.Errorf("missing marker = %q, want all", got)
	}
	if err := os.WriteFile(viewMarkerPath(dir), []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readView(dir); got != "blocked" {
		t.Errorf("marker = %q, want blocked", got)
	}
	if err := os.WriteFile(viewMarkerPath(dir), []byte("../../etc/passwd"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readView(dir); got != "all" {
		t.Errorf("unknown marker = %q, want the all fallback", got)
	}
}

// fzf parses a --listen value without a .sock suffix as a PORT, which would
// silently open a TCP listener instead of a unix socket.
func TestListenSocketPathIsASock(t *testing.T) {
	if got := listenSocketPath("/tmp/x"); !strings.HasSuffix(got, ".sock") {
		t.Errorf("listen socket = %q, want a .sock suffix", got)
	}
}

// A herdr event cannot change the zoxide ranking or a git branch, so a
// re-render must not re-shell either. Before this, every debounced event cost
// one zoxide subprocess plus a `git status` PER WORKSPACE CWD — on a machine
// with a dozen workspaces, a burst of agent activity meant a burst of
// subprocesses for data that had not moved.
func TestRenderAllReusesZoxideAcrossEvents(t *testing.T) {
	r, _ := liveRenderer(t, []herdrtest.SnapshotFixtureAgent{
		liveAgent("a", "w1:p1", herdr.StatusBlocked, 10),
	})
	ctx := context.Background()

	if _, err := r.renderAll(ctx, false); err != nil {
		t.Fatalf("renderAll: %v", err)
	}
	first := r.prev
	if first == nil {
		t.Fatal("renderAll did not retain its sources for reuse")
	}
	// Plant a sentinel the next render must reuse rather than recompute.
	sentinel := []string{"/sentinel/dir"}
	r.prev.zoxide = sentinel
	gitBefore := r.prev.git

	if _, err := r.renderAll(ctx, false); err != nil {
		t.Fatalf("renderAll: %v", err)
	}
	if len(r.prev.zoxide) != 1 || r.prev.zoxide[0] != "/sentinel/dir" {
		t.Errorf("zoxide list was recomputed instead of reused: %v", r.prev.zoxide)
	}
	if r.prev.git != gitBefore {
		t.Error("git cache was rebuilt on a re-render; every workspace would be re-probed")
	}
}

// The first render happens before fzf has created its listen socket, so a push
// there could only ever fail. It used to be attempted and swallowed, which
// left a permanent misleading failure in any log anyone turned on.
func TestRenderAllDoesNotPushWhenAskedNotTo(t *testing.T) {
	r, _ := liveRenderer(t, []herdrtest.SnapshotFixtureAgent{
		liveAgent("a", "w1:p1", herdr.StatusBlocked, 10),
	})
	// listenSocketPath(dir) does not exist — a push would error. renderAll
	// must not attempt one, and must still succeed.
	if _, err := r.renderAll(context.Background(), false); err != nil {
		t.Fatalf("renderAll(push=false) = %v, want success with no push attempted", err)
	}
	if _, err := os.Stat(listenSocketPath(r.dir)); !os.IsNotExist(err) {
		t.Fatalf("test precondition: expected no listen socket at %s", listenSocketPath(r.dir))
	}
}

// The daemon-liveness gate belongs at picker open, not on every event: an
// event arriving IS evidence the daemon is alive, so re-proving it costs a
// round trip to learn nothing.
func TestRenderAllSkipsTheLivenessProbe(t *testing.T) {
	r, s := liveRenderer(t, []herdrtest.SnapshotFixtureAgent{
		liveAgent("a", "w1:p1", herdr.StatusBlocked, 10),
	})
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := r.renderAll(ctx, false); err != nil {
			t.Fatalf("renderAll: %v", err)
		}
	}
	if n := len(s.Calls("workspace.list")); n != 0 {
		t.Errorf("renderAll made %d liveness probes across 3 renders, want 0", n)
	}
	if n := len(s.Calls("session.snapshot")); n != 3 {
		t.Errorf("session.snapshot calls = %d, want one per render", n)
	}
}

// HERDR_WORKSPACE_ID is captured when the popup spawns and never changes, so a
// picker left open while the user moves around kept sorting the launch
// workspace first while the "· current" label followed the daemon. A re-render
// must prefer the live focus so the two agree.
func TestRenderAllPrefersLiveFocusOverLaunchEnv(t *testing.T) {
	s := herdrtest.Start(t)
	s.Handle("workspace.list", func(json.RawMessage) (any, error) {
		return map[string]any{"workspaces": []map[string]any{}}, nil
	})
	s.Handle("session.snapshot", func(json.RawMessage) (any, error) {
		raw := herdrtest.SnapshotFixture{
			Workspaces:         []herdr.Workspace{{ID: "w1", Number: 1, Label: "one"}, {ID: "w2", Number: 2, Label: "two"}},
			FocusedWorkspaceID: "w2",
		}.SnapshotJSON()
		return json.RawMessage(raw), nil
	})

	dir, err := os.MkdirTemp("", "lf")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	env := &appEnv{
		// Launched from w1; the daemon has since focused w2.
		getenv:   fakeEnv(map[string]string{"HERDR_SOCKET_PATH": s.Path(), "HERDR_BIN_PATH": "/bin/sh", "HERDR_WORKSPACE_ID": "w1"}),
		herdrBin: "/bin/sh",
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
		self:     "/nonexistent/sesh-bro",
	}
	r := &renderer{env: env, cfg: config.Load(env.getenv), dir: dir,
		fzf: fzfctl.New(listenSocketPath(dir)), lastPushed: map[string]string{}}

	// First render is the "one-shot" shape (no prev): the env var wins.
	if _, err := r.renderAll(context.Background(), false); err != nil {
		t.Fatalf("renderAll: %v", err)
	}
	if r.prev.current != "w1" {
		t.Errorf("first render current = %q, want the launch workspace w1", r.prev.current)
	}

	// Second render is a re-render: the daemon's focus wins.
	if _, err := r.renderAll(context.Background(), false); err != nil {
		t.Fatalf("renderAll: %v", err)
	}
	if r.prev.current != "w2" {
		t.Errorf("re-render current = %q, want the live focus w2", r.prev.current)
	}
}
