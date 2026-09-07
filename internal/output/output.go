// Package output is sesh-bro's machine-facing contract: the shape an action
// command prints and the exit codes it uses.
//
// It exists because sesh-bro is drivable by another coding agent, and an agent
// consuming a CLI has two needs a human does not. It must be able to branch
// without parsing prose, which is what the exit codes are for. And it must be
// able to parse output that is valid even when the command failed, which is
// what the envelope is for — a command that prints an error to stderr and
// nothing to stdout forces the caller to scrape human text to find out what
// happened.
//
// Row-producing commands (`list`, `counts`) are deliberately NOT covered here.
// They have an older `--json` contract that predates this one and that
// docs/BEHAVIOUR.md §9 S8 pins byte for byte; breaking it to gain uniformity
// would trade a real compatibility guarantee for a cosmetic one.
package output

import (
	"encoding/json"
	"fmt"
	"io"
)

// Exit codes. An agent branches on these rather than reading messages.
const (
	// ExitOK means the command did what was asked.
	ExitOK = 0
	// ExitFailure is a runtime failure: the daemon is unreachable, herdr
	// rejected the call, the target does not exist.
	ExitFailure = 1
	// ExitUsage is the caller's mistake: an unknown flag, a missing argument,
	// a status name that is not a status. Distinct from ExitFailure because
	// retrying an ExitUsage unchanged is always pointless.
	ExitUsage = 2
	// ExitEmpty means the command ran correctly and the answer is "nothing":
	// no agent needs attention, the wait timed out, the query matched none.
	//
	// This is the code that makes the CLI composable, and the one most often
	// left out. Folding "nothing matched" into ExitFailure tells a caller that
	// something went wrong when nothing did, so the only way to distinguish a
	// quiet queue from a broken daemon is to parse the message — exactly the
	// coupling these codes exist to remove.
	ExitEmpty = 3
)

// Envelope is what an action command prints under --json: exactly one object,
// on one line, always valid — including when ok is false.
type Envelope struct {
	OK      bool   `json:"ok"`
	Command string `json:"command"`
	// Results is never null in the emitted JSON, only ever an array, so a
	// consumer can iterate it without a nil check. Emit is what guarantees it.
	Results []any `json:"results"`
	// Error is null on success and a human-readable string otherwise. It
	// explains; the exit code decides.
	Error *string `json:"error"`
}

// Emit writes an envelope as one line of JSON.
//
// Compact rather than indented, and newline-terminated, so a caller can read
// it with a line-oriented reader and hand each line straight to a JSON parser.
// The row commands' pretty concatenated objects are the opposite choice for
// the opposite reason — they are read by humans as often as by machines.
func Emit(w io.Writer, env Envelope) error {
	if env.Results == nil {
		env.Results = []any{}
	}
	b, err := json.Marshal(env)
	if err != nil {
		// Unreachable for the shapes this package emits, and a caller cannot
		// do anything useful with it, but returning it beats writing a broken
		// line that a consumer would then fail to parse.
		return fmt.Errorf("output: marshal envelope: %w", err)
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}

// Success is the envelope for a command that did what was asked.
func Success(command string, results ...any) Envelope {
	return Envelope{OK: true, Command: command, Results: results}
}

// Failure is the envelope for a command that did not.
func Failure(command string, err error, results ...any) Envelope {
	msg := "unknown error"
	if err != nil {
		msg = err.Error()
	}
	return Envelope{OK: false, Command: command, Results: results, Error: &msg}
}

// Empty is the envelope for a correct run whose answer is "nothing". It is
// ok:true with no results, because nothing happening is not a failure — the
// distinction lives in ExitEmpty, not in the ok flag.
func Empty(command string) Envelope {
	return Envelope{OK: true, Command: command, Results: []any{}}
}
