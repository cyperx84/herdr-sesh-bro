package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	herdr "github.com/cyperx84/herdr-api"

	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx/herdrtest"
)

// See wait_test.go for the rationale: these commands are not yet wired into
// commands.go, so test-only registration lets run() dispatch to them.
func init() {
	if _, ok := lookupCommand("wait"); !ok {
		commands = append(commands, commandSpec{Name: "wait", Run: cmdWait})
	}
	if _, ok := lookupCommand("read"); !ok {
		commands = append(commands, commandSpec{Name: "read", Run: cmdRead})
	}
}

// readFixture stands up the two calls `read` makes: the liveness probe and
// the agent.read RPC.
func readFixture(t *testing.T, text string) *herdrtest.Server {
	t.Helper()
	s := herdrtest.Start(t)
	s.Handle("workspace.list", func(json.RawMessage) (any, error) {
		return map[string]any{"workspaces": []map[string]any{}}, nil
	})
	s.Handle("agent.read", func(params json.RawMessage) (any, error) {
		var p herdr.AgentReadParams
		_ = json.Unmarshal(params, &p)
		return struct {
			Read herdr.PaneReadResult `json:"read"`
		}{
			Read: herdr.PaneReadResult{
				PaneID: "w1:p2",
				Text:   text,
				Source: p.Source,
				Format: p.Format,
			},
		}, nil
	})
	return s
}

// The plain path must emit the pane text unchanged, ANSI escapes and all,
// because a human piping this to a pager or a log file expects colour.
func TestReadPlainPreservesANSI(t *testing.T) {
	text := "\x1b[31mhello\x1b[0m\n"
	s := readFixture(t, text)
	var stdout, stderr bytes.Buffer
	code := run([]string{"read", "stuck"},
		strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	if stdout.String() != text {
		t.Errorf("stdout = %q, want %q", stdout.String(), text)
	}
}

// The JSON envelope must contain the text so a driving agent can read it
// without shell-parsing raw stdout.
func TestReadJSONEnvelope(t *testing.T) {
	s := readFixture(t, "visible text\n")
	var stdout, stderr bytes.Buffer
	code := run([]string{"read", "stuck", "--json"},
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
	if got["command"] != "read" {
		t.Errorf("command = %v, want read", got["command"])
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
	if r["text"] != "visible text\n" {
		t.Errorf("text = %v, want %q", r["text"], "visible text\n")
	}
}

// recent-unwrapped is the valuable source for scrollback; the flag name
// must map to herdr's recent_unwrapped wire value and the read must keep
// ANSI intact (strip_ansi=false).
func TestReadMapsSourceAndKeepsANSI(t *testing.T) {
	s := readFixture(t, "scrollback")
	var stdout, stderr bytes.Buffer
	code := run([]string{"read", "stuck", "--source", "recent-unwrapped"},
		strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	calls := s.Calls("agent.read")
	if len(calls) != 1 {
		t.Fatalf("agent.read calls = %d, want 1", len(calls))
	}
	var params herdr.AgentReadParams
	if err := json.Unmarshal(calls[0].Params, &params); err != nil {
		t.Fatalf("agent.read params unmarshal: %v", err)
	}
	if params.Source != herdr.ReadSourceRecentUnwrapped {
		t.Errorf("source = %q, want recent_unwrapped", params.Source)
	}
	if params.Format != herdr.ReadFormatANSI {
		t.Errorf("format = %q, want ansi", params.Format)
	}
	if params.StripANSI == nil || *params.StripANSI != false {
		t.Errorf("strip_ansi = %v, want explicit false", params.StripANSI)
	}
}

// --lines must be forwarded as a uint32 cap so herdr truncates scrollback
// rather than returning the whole buffer.
func TestReadForwardsLines(t *testing.T) {
	s := readFixture(t, "x")
	var stdout, stderr bytes.Buffer
	code := run([]string{"read", "stuck", "--lines", "42"},
		strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	calls := s.Calls("agent.read")
	var params herdr.AgentReadParams
	if err := json.Unmarshal(calls[0].Params, &params); err != nil {
		t.Fatalf("agent.read params unmarshal: %v", err)
	}
	if params.Lines == nil || *params.Lines != 42 {
		t.Errorf("lines = %v, want 42", params.Lines)
	}
}

// read requires exactly one positional target; anything else is a usage
// error because there is no sensible default agent to inspect.
func TestReadRequiresTarget(t *testing.T) {
	s := readFixture(t, "")
	var stdout, stderr bytes.Buffer
	code := run([]string{"read"},
		strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	if code != 2 {
		t.Errorf("code = %d, want 2 for missing target", code)
	}
}

// An unknown --source would send a bad value to herdr and produce a
// confusing server error, so we reject it locally with exit 2.
func TestReadRejectsUnknownSource(t *testing.T) {
	s := readFixture(t, "")
	var stdout, stderr bytes.Buffer
	code := run([]string{"read", "stuck", "--source", "invisible"},
		strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	if code != 2 {
		t.Errorf("code = %d, want 2 for an unknown source", code)
	}
}

// Unknown flags must be caught before any herdr call is made.
func TestReadRejectsUnknownFlag(t *testing.T) {
	s := readFixture(t, "")
	var stdout, stderr bytes.Buffer
	code := run([]string{"read", "stuck", "--nope"},
		strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	if code != 2 {
		t.Errorf("code = %d, want 2 for an unknown flag", code)
	}
}
