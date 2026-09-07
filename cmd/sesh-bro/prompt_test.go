package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	herdr "github.com/cyperx84/herdr-api"

	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx/herdrtest"
)

func init() {
	if _, ok := lookupCommand("prompt"); !ok {
		commands = append(commands, commandSpec{Name: "prompt", Run: cmdPrompt})
	}
}

func promptFixture(t *testing.T, agents []herdrtest.SnapshotFixtureAgent, promptFn func(target, text string) (herdr.Agent, error)) *herdrtest.Server {
	t.Helper()
	s := herdrtest.Start(t)
	s.Handle("workspace.list", func(json.RawMessage) (any, error) {
		return map[string]any{"workspaces": []map[string]any{}}, nil
	})
	s.Handle("session.snapshot", func(json.RawMessage) (any, error) {
		return json.RawMessage(herdrtest.SnapshotFixture{Agents: agents}.SnapshotJSON()), nil
	})
	s.Handle("agent.prompt", func(params json.RawMessage) (any, error) {
		var p struct {
			Target string `json:"target"`
			Text   string `json:"text"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		a, err := promptFn(p.Target, p.Text)
		if err != nil {
			return nil, err
		}
		return map[string]any{"agent": a}, nil
	})
	return s
}

func promptFixtureAgent(name, pane string, status herdr.AgentStatus, seq uint64) herdrtest.SnapshotFixtureAgent {
	kind := "claude"
	return herdrtest.SnapshotFixtureAgent{
		Agent: herdr.Agent{
			Name: name, PaneID: pane, WorkspaceID: "w1",
			Status: status, Agent: &kind, CWD: "/tmp/" + name,
		},
		StateChangeSeq: seq,
	}
}

// --all-blocked prompts every blocked agent and no others. A user would see
// this fail if a blocked agent was left waiting for an answer it never
// received, or if an idle agent was spuriously interrupted and lost context.
func TestPromptAllBlockedPromptsBlockedOnly(t *testing.T) {
	s := promptFixture(t, []herdrtest.SnapshotFixtureAgent{
		promptFixtureAgent("calm", "w1:p1", herdr.StatusIdle, 90),
		promptFixtureAgent("stuck", "w1:p2", herdr.StatusBlocked, 10),
		promptFixtureAgent("asked", "w1:p3", herdr.StatusBlocked, 20),
	}, func(target, text string) (herdr.Agent, error) {
		return herdr.Agent{PaneID: target + "-pane", Status: herdr.StatusWorking}, nil
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"prompt", "--all-blocked", "--text", "continue"},
		strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	calls := s.Calls("agent.prompt")
	if len(calls) != 2 {
		t.Fatalf("agent.prompt calls = %d, want 2", len(calls))
	}
	var got []string
	for _, c := range calls {
		var p struct{ Target string }
		if err := json.Unmarshal(c.Params, &p); err != nil {
			t.Fatalf("unmarshal params: %v", err)
		}
		got = append(got, p.Target)
		if p.Target == "calm" {
			t.Errorf("prompted idle agent %s", p.Target)
		}
	}
	// AttentionSet orders blocked agents newest state change first.
	want := []string{"asked", "stuck"}
	if !sliceEqual(got, want) {
		t.Errorf("prompted %v, want %v", got, want)
	}
	for _, c := range calls {
		var p struct{ Text string }
		if err := json.Unmarshal(c.Params, &p); err != nil {
			t.Fatalf("unmarshal params: %v", err)
		}
		if p.Text != "continue" {
			t.Errorf("text = %q, want %q", p.Text, "continue")
		}
	}
}

// A partial failure must still prompt every remaining target and report each
// result. If one unreachable agent aborted the loop, the other agents would
// sit blocked forever while the caller would only know about the first
// failure.
func TestPromptPartialFailureContinues(t *testing.T) {
	s := promptFixture(t, []herdrtest.SnapshotFixtureAgent{
		promptFixtureAgent("a", "w1:p1", herdr.StatusBlocked, 10),
		promptFixtureAgent("b", "w1:p2", herdr.StatusBlocked, 20),
	}, func(target, text string) (herdr.Agent, error) {
		if target == "a" {
			return herdr.Agent{}, &herdr.APIError{Code: "not_found", Message: "no such agent"}
		}
		return herdr.Agent{PaneID: "w1:p2", Status: herdr.StatusWorking}, nil
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"prompt", "--all-blocked", "--text", "go", "--json"},
		strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	if code != 1 {
		t.Fatalf("code = %d, want 1 (stderr: %q)", code, stderr.String())
	}
	calls := s.Calls("agent.prompt")
	if len(calls) != 2 {
		t.Fatalf("agent.prompt calls = %d, want 2", len(calls))
	}
	var got []string
	for _, c := range calls {
		var p struct{ Target string }
		json.Unmarshal(c.Params, &p)
		got = append(got, p.Target)
	}
	if !sliceEqual(got, []string{"b", "a"}) {
		t.Errorf("prompted %v, want both b and a", got)
	}

	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	if envelope["ok"] != false {
		t.Errorf("ok = %v, want false", envelope["ok"])
	}
	results, _ := envelope["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("results = %v, want 2", results)
	}
	okCount := 0
	for _, r := range results {
		m, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("result is not an object: %v", r)
		}
		if m["ok"] == true {
			okCount++
		}
		if _, ok := m["target"]; !ok {
			t.Errorf("result missing target: %v", m)
		}
		if _, ok := m["pane_id"]; !ok {
			t.Errorf("result missing pane_id: %v", m)
		}
	}
	if okCount != 1 {
		t.Errorf("ok results = %d, want 1", okCount)
	}
}

// Empty text is almost certainly a caller mistake. Sending it would wake an
// idle agent or confuse a blocked agent with a blank prompt, so we refuse
// before touching the daemon.
func TestPromptEmptyText(t *testing.T) {
	s := promptFixture(t, nil, func(_, _ string) (herdr.Agent, error) {
		return herdr.Agent{}, nil
	})
	var stdout, stderr bytes.Buffer
	code := run([]string{"prompt", "a", "--text", ""},
		strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	if code != 2 {
		t.Fatalf("code = %d, want 2 (stderr: %q)", code, stderr.String())
	}
	if len(s.Calls("agent.prompt")) != 0 {
		t.Error("prompted an agent with empty text")
	}
}

// Nothing blocked is a correct answer to "who needs a prompt now", not a
// failure. Returning exit 3 lets a driving script distinguish "nobody" from
// "the daemon is down".
func TestPromptAllBlockedEmpty(t *testing.T) {
	s := promptFixture(t, []herdrtest.SnapshotFixtureAgent{
		promptFixtureAgent("calm", "w1:p1", herdr.StatusIdle, 10),
	}, func(_, _ string) (herdr.Agent, error) {
		return herdr.Agent{}, nil
	})
	var stdout, stderr bytes.Buffer
	code := run([]string{"prompt", "--all-blocked", "--text", "go", "--json"},
		strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	if code != 3 {
		t.Fatalf("code = %d, want 3 (stderr: %q)", code, stderr.String())
	}
	if len(s.Calls("agent.prompt")) != 0 {
		t.Error("prompted with nothing blocked")
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	if envelope["ok"] != true {
		t.Errorf("ok = %v, want true", envelope["ok"])
	}
	results, _ := envelope["results"].([]any)
	if len(results) != 0 {
		t.Errorf("results = %v, want empty", results)
	}
}

// --from-file lets a multi-selection from the picker turn into one prompt.
// Non-agent rows must be reported so the caller does not think they were
// prompted too.
func TestPromptFromFileTakesAgentRows(t *testing.T) {
	s := promptFixture(t, nil, func(target, text string) (herdr.Agent, error) {
		return herdr.Agent{PaneID: target + "-pane", Status: herdr.StatusWorking}, nil
	})

	rows := writeRowsFile(t,
		"agent\tbuilder\t● builder",
		"workspace\tw1\t◆ alpha",
		"dir\t/tmp/x\t▸ x",
	)

	var stdout, stderr bytes.Buffer
	code := run([]string{"prompt", "--from-file", rows, "--text", "go"},
		strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	calls := s.Calls("agent.prompt")
	if len(calls) != 1 {
		t.Fatalf("agent.prompt calls = %d, want 1", len(calls))
	}
	var p struct{ Target string }
	if err := json.Unmarshal(calls[0].Params, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Target != "builder" {
		t.Errorf("prompted %s, want builder", p.Target)
	}
	if !strings.Contains(stderr.String(), "workspace w1") {
		t.Errorf("stderr does not report skipped workspace: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "dir /tmp/x") {
		t.Errorf("stderr does not report skipped dir: %q", stderr.String())
	}
	// The header row is chrome and not a user selection; it must not be
	// reported as skipped.
	if strings.Contains(stderr.String(), "header") {
		t.Errorf("reported the header row as skipped: %q", stderr.String())
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
