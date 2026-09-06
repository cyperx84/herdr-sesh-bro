package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	herdr "github.com/cyperx84/herdr-api"

	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx/herdrtest"
)

// countsFixture registers the two calls `counts` makes: the liveness probe
// and the snapshot.
func countsFixture(t *testing.T, agents []herdrtest.SnapshotFixtureAgent) *herdrtest.Server {
	t.Helper()
	s := herdrtest.Start(t)
	s.Handle("workspace.list", func(json.RawMessage) (any, error) {
		return map[string]any{"workspaces": []map[string]any{}}, nil
	})
	s.Handle("session.snapshot", func(json.RawMessage) (any, error) {
		return json.RawMessage(herdrtest.SnapshotFixture{Agents: agents}.SnapshotJSON()), nil
	})
	return s
}

func agentWith(status herdr.AgentStatus, pane string) herdrtest.SnapshotFixtureAgent {
	return herdrtest.SnapshotFixtureAgent{
		Agent: herdr.Agent{PaneID: pane, WorkspaceID: "w1", Status: status},
	}
}

// The plain line is what a herdr tab_bar_right entry renders, so it must be
// exactly one line with no escapes, and zero-count statuses must be absent —
// the whole point is being readable at a glance.
func TestCountsPlainOmitsZeros(t *testing.T) {
	s := countsFixture(t, []herdrtest.SnapshotFixtureAgent{
		agentWith(herdr.StatusBlocked, "p1"),
		agentWith(herdr.StatusBlocked, "p2"),
		agentWith(herdr.StatusIdle, "p3"),
	})

	var stdout, stderr bytes.Buffer
	if code := run([]string{"counts"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s))); code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	got := stdout.String()
	if strings.Count(got, "\n") != 1 {
		t.Errorf("output must be exactly one line, got %q", got)
	}
	if strings.Contains(got, "\x1b") {
		t.Errorf("plain style must carry no ANSI escapes, got %q", got)
	}
	if !strings.Contains(got, "🔴2") || !strings.Contains(got, "⚪1") {
		t.Errorf("counts = %q, want blocked 2 and idle 1", got)
	}
	if strings.Contains(got, "🟡") || strings.Contains(got, "🔵") {
		t.Errorf("zero-count statuses must be omitted, got %q", got)
	}
}

// --all is the escape hatch for a fixed-width display that wants every slot
// present even at zero.
func TestCountsAllIncludesZeros(t *testing.T) {
	s := countsFixture(t, []herdrtest.SnapshotFixtureAgent{agentWith(herdr.StatusIdle, "p1")})
	var stdout, stderr bytes.Buffer
	if code := run([]string{"counts", "--all"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s))); code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	for _, glyph := range []string{"🔴0", "🟡0", "🔵0", "⚪1", "⚫0"} {
		if !strings.Contains(stdout.String(), glyph) {
			t.Errorf("--all output %q missing %q", stdout.String(), glyph)
		}
	}
}

// An empty session says so rather than printing an empty line: a blank tab-bar
// entry reads as a broken command, not as "nothing to do".
func TestCountsEmptySessionSaysNoAgents(t *testing.T) {
	s := countsFixture(t, nil)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"counts"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s))); code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "no agents" {
		t.Errorf("counts = %q, want %q", stdout.String(), "no agents")
	}
}

// JSON keeps every status, zeros included, so a consumer indexing the object
// never has to tell absent from zero.
func TestCountsJSONKeepsZeros(t *testing.T) {
	s := countsFixture(t, []herdrtest.SnapshotFixtureAgent{agentWith(herdr.StatusWorking, "p1")})
	var stdout, stderr bytes.Buffer
	if code := run([]string{"counts", "--json"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s))); code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	var got map[string]int
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v (%q)", err, stdout.String())
	}
	want := map[string]int{"blocked": 0, "working": 1, "done": 0, "idle": 0, "unknown": 0}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("counts[%q] = %d, want %d (full: %v)", k, got[k], v, got)
		}
	}
}

// An agent herdr sent with no status counts as unknown, so the totals always
// add up to the number of agents (matching AgentRows' normalization).
func TestCountsNormalizesEmptyStatus(t *testing.T) {
	s := countsFixture(t, []herdrtest.SnapshotFixtureAgent{agentWith("", "p1")})
	var stdout, stderr bytes.Buffer
	if code := run([]string{"counts", "--json"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s))); code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	var got map[string]int
	_ = json.Unmarshal(stdout.Bytes(), &got)
	if got["unknown"] != 1 {
		t.Errorf("unknown = %d, want 1 (full: %v)", got["unknown"], got)
	}
}

func TestCountsUnknownFlag(t *testing.T) {
	s := countsFixture(t, nil)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"counts", "--nope"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s))); code != 2 {
		t.Errorf("code = %d, want 2 for an unknown flag", code)
	}
}
