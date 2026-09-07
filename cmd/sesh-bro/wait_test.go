package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	herdr "github.com/cyperx84/herdr-api"

	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx/herdrtest"
)

// Register wait/read with the command table from tests so run() can dispatch
// to them before the real wiring lands in commands.go. lookupCommand guards
// against duplicates once the owner adds the production entries.

// waitFixture stands up the two calls `wait` makes: the liveness probe and
// the blocking agent.wait RPC.
func waitFixture(t *testing.T, final herdr.AgentStatus) *herdrtest.Server {
	t.Helper()
	s := herdrtest.Start(t)
	s.Handle("workspace.list", func(json.RawMessage) (any, error) {
		return map[string]any{"workspaces": []map[string]any{}}, nil
	})
	s.Handle("agent.wait", func(json.RawMessage) (any, error) {
		return struct {
			Agent herdr.Agent `json:"agent"`
		}{Agent: herdr.Agent{PaneID: "w1:p2", Status: final}}, nil
	})
	return s
}

// A settled agent must be reported on stdout so a script can read the final
// status and pane id without parsing JSON.
func TestWaitReportsSettledAgent(t *testing.T) {
	s := waitFixture(t, herdr.StatusBlocked)
	var stdout, stderr bytes.Buffer
	code := run([]string{"wait", "--target", "stuck", "--until", "blocked", "--timeout", "5s"},
		strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	if got := stdout.String(); got != "blocked w1:p2\n" {
		t.Errorf("stdout = %q, want %q", got, "blocked w1:p2\n")
	}
	calls := s.Calls("agent.wait")
	if len(calls) != 1 {
		t.Fatalf("agent.wait calls = %d, want 1 (wait must not poll)", len(calls))
	}
	var params struct {
		Target    string   `json:"target"`
		Until     []string `json:"until"`
		TimeoutMs uint64   `json:"timeout_ms"`
	}
	if err := json.Unmarshal(calls[0].Params, &params); err != nil {
		t.Fatalf("agent.wait params unmarshal: %v", err)
	}
	if params.Target != "stuck" {
		t.Errorf("target = %q, want stuck", params.Target)
	}
	found := false
	for _, u := range params.Until {
		if u == "blocked" {
			found = true
		}
	}
	if !found {
		t.Errorf("until = %v, want it to contain blocked", params.Until)
	}
	if params.TimeoutMs != 5000 {
		t.Errorf("timeout_ms = %d, want 5000", params.TimeoutMs)
	}
}

// The JSON envelope is what another coding agent consumes. A malformed or
// multi-line response would break every caller that expects one parseable
// object per invocation.
func TestWaitJSONEnvelope(t *testing.T) {
	s := waitFixture(t, herdr.StatusDone)
	var stdout, stderr bytes.Buffer
	code := run([]string{"wait", "--target", "stuck", "--until", "done", "--json"},
		strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	if strings.Count(stdout.String(), "\n") != 1 {
		t.Errorf("JSON output must be exactly one line, got %q", stdout.String())
	}
	var got map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v (%q)", err, stdout.String())
	}
	if got["ok"] != true {
		t.Errorf("ok = %v, want true", got["ok"])
	}
	if got["command"] != "wait" {
		t.Errorf("command = %v, want wait", got["command"])
	}
	if got["error"] != nil {
		t.Errorf("error = %v, want null", got["error"])
	}
	results, ok := got["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %v, want one-element array", got["results"])
	}
	r, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result element is not an object: %v", results[0])
	}
	if r["status"] != "done" || r["pane_id"] != "w1:p2" {
		t.Errorf("result = %v, want status done and pane_id w1:p2", r)
	}
}

// An unknown --until status must exit 2: the caller made a mistake, and
// treating that as a runtime or timeout failure would hide the real problem.
func TestWaitRejectsUnknownStatus(t *testing.T) {
	s := waitFixture(t, herdr.StatusIdle)
	var stdout, stderr bytes.Buffer
	code := run([]string{"wait", "--target", "stuck", "--until", "finished"},
		strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	if code != 2 {
		t.Errorf("code = %d, want 2 for an unknown status", code)
	}
	if !strings.Contains(stderr.String(), "unknown status") {
		t.Errorf("stderr = %q, want unknown-status message", stderr.String())
	}
}

// --target is mandatory; without it there is no agent to wait on.
func TestWaitRequiresTarget(t *testing.T) {
	s := waitFixture(t, herdr.StatusIdle)
	var stdout, stderr bytes.Buffer
	code := run([]string{"wait"},
		strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	if code != 2 {
		t.Errorf("code = %d, want 2 for missing --target", code)
	}
}

// Unknown flags must not be silently ignored, or a typo would silently change
// the command's meaning (e.g. --until mistyped becomes the default settle set).
func TestWaitRejectsUnknownFlag(t *testing.T) {
	s := waitFixture(t, herdr.StatusIdle)
	var stdout, stderr bytes.Buffer
	code := run([]string{"wait", "--target", "stuck", "--nope"},
		strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	if code != 2 {
		t.Errorf("code = %d, want 2 for an unknown flag", code)
	}
}

// Timeout is the whole reason the command exists: a script must be able to
// ask "did it settle in 60s?" and get a distinct negative answer. Exit 3
// keeps that separate from unreachable-daemons (1) and bad invocation (2).
func TestWaitTimeoutExitsThree(t *testing.T) {
	s := waitFixture(t, herdr.StatusWorking)
	var stdout, stderr bytes.Buffer
	code := run([]string{"wait", "--target", "stuck", "--timeout", "100ms"},
		strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	if code != 3 {
		t.Fatalf("code = %d, want 3 for a timed-out wait", code)
	}
	if !strings.Contains(stderr.String(), "timed out") {
		t.Errorf("stderr = %q, want a timeout message", stderr.String())
	}
}

// Even on timeout the JSON response must carry the agent's final state, so
// the caller can decide what to do next without making a second agent.get call.
func TestWaitTimeoutJSONIncludesFinalState(t *testing.T) {
	s := waitFixture(t, herdr.StatusWorking)
	var stdout, stderr bytes.Buffer
	code := run([]string{"wait", "--target", "stuck", "--timeout", "100ms", "--json"},
		strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	if code != 3 {
		t.Fatalf("code = %d, want 3", code)
	}
	var got map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v (%q)", err, stdout.String())
	}
	if got["ok"] != false {
		t.Errorf("ok = %v, want false on timeout", got["ok"])
	}
	if got["error"] != nil {
		t.Errorf("error = %v, want null on timeout", got["error"])
	}
	results := got["results"].([]any)
	r := results[0].(map[string]any)
	if r["status"] != "working" || r["pane_id"] != "w1:p2" {
		t.Errorf("result = %v, want status working and pane_id w1:p2", r)
	}
}
