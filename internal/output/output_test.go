package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// The envelope must be exactly one line of valid JSON. A consumer reads
// line-by-line and parses each line; a pretty-printed object would break that
// on the first newline.
func TestEmitIsOneParseableLine(t *testing.T) {
	var buf bytes.Buffer
	if err := Emit(&buf, Success("prompt", map[string]any{"target": "w1:p2"})); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	out := buf.String()
	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Errorf("output is not exactly one newline-terminated line: %q", out)
	}
	var got Envelope
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("emitted line is not valid JSON: %v (%q)", err, out)
	}
	if !got.OK || got.Command != "prompt" || len(got.Results) != 1 {
		t.Errorf("round-tripped %+v", got)
	}
}

// A failed command must still print a parseable envelope. If it printed prose
// to stderr and nothing to stdout, the caller would have to scrape human text
// to learn what happened — which is the coupling this package removes.
func TestFailureIsStillValidJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := Emit(&buf, Failure("wait", errors.New("daemon is not responding"))); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var got Envelope
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("failure envelope is not valid JSON: %v", err)
	}
	if got.OK {
		t.Error("ok = true on a failure envelope")
	}
	if got.Error == nil || !strings.Contains(*got.Error, "daemon") {
		t.Errorf("error field = %v, want the message", got.Error)
	}
}

// results is always an array, never null, so a consumer can iterate without a
// nil check. This is the difference between `for r in x["results"]` working
// and throwing.
func TestResultsIsNeverNull(t *testing.T) {
	for name, env := range map[string]Envelope{
		"empty":       Empty("next"),
		"failure":     Failure("next", errors.New("boom")),
		"nil results": {OK: true, Command: "next"},
	} {
		var buf bytes.Buffer
		if err := Emit(&buf, env); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(raw["results"]) != "[]" {
			t.Errorf("%s: results = %s, want []", name, raw["results"])
		}
	}
}

// "Nothing to report" is a success with a distinct exit code, not a failure.
// Reporting ok:false for a quiet queue would tell a caller something went
// wrong when nothing did.
func TestEmptyIsNotAFailure(t *testing.T) {
	env := Empty("next")
	if !env.OK {
		t.Error("Empty() is ok:false; nothing happening is not a failure")
	}
	if env.Error != nil {
		t.Errorf("Empty() carries an error: %v", *env.Error)
	}
}

// The codes are the branching contract. Pinning them stops a later edit from
// renumbering one and silently changing what every caller's `if` means.
func TestExitCodesAreStable(t *testing.T) {
	for name, got := range map[string]struct{ code, want int }{
		"ok":      {ExitOK, 0},
		"failure": {ExitFailure, 1},
		"usage":   {ExitUsage, 2},
		"empty":   {ExitEmpty, 3},
	} {
		if got.code != got.want {
			t.Errorf("%s exit code = %d, want %d", name, got.code, got.want)
		}
	}
}
