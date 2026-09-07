package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cyperx84/herdr-sesh-bro/docs"
)

// Documentation an agent reads has a failure mode documentation a human reads
// does not: a human who cannot find a command asks, while an agent simply
// never discovers it. So every command marked Headless must appear in
// docs/AGENTS.md, checked mechanically rather than by remembering.
//
// This is the mechanism that stops the drift docs/FEATURE-DEMAND.md suffered,
// where a claim stayed in the repo long after the code refuted it.
func TestSkillDocumentsEveryHeadlessCommand(t *testing.T) {
	doc := docs.AgentsMD
	for _, name := range headlessCommands() {
		// `sesh-bro <name>` is how the doc shows every command, so that is
		// what is searched for: a bare name would match prose accidentally
		// ("read" and "list" are ordinary words) and prove nothing.
		if !strings.Contains(doc, "sesh-bro "+name) {
			t.Errorf("command %q is headless but never appears in docs/AGENTS.md as `sesh-bro %s`;\n"+
				"an agent reading the docs has no way to discover it", name, name)
		}
	}
}

// The exit codes ARE the contract — an agent branches on them without parsing
// text — so a code that exists in the binary and not in the docs is a promise
// nobody was told about.
func TestSkillDocumentsTheExitCodes(t *testing.T) {
	doc := docs.AgentsMD
	for _, want := range []string{"| 0 |", "| 1 |", "| 2 |", "| 3 |"} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/AGENTS.md does not document exit code row %q", want)
		}
	}
	// Exit 3 is the one most often omitted and the one that makes the CLI
	// composable, so it gets its own assertion rather than riding along.
	if !strings.Contains(doc, "timed out") {
		t.Error("docs/AGENTS.md never explains that exit 3 covers a timeout")
	}
}

// --jsonl exists because --json cannot be parsed in one call, and an agent
// that does not know this will write code that half-works on one row and
// breaks on two. If the doc stops saying so, the trap comes back.
func TestSkillWarnsAboutTheJSONTrap(t *testing.T) {
	doc := docs.AgentsMD
	if !strings.Contains(doc, "--jsonl") {
		t.Fatal("docs/AGENTS.md never mentions --jsonl")
	}
	if !strings.Contains(doc, "json.loads") {
		t.Error("docs/AGENTS.md does not warn that --json is not parseable in one call")
	}
}

// --skill must print the file, not a summary of it: the point of embedding is
// that there is one artefact and no second copy to go stale.
func TestSkillPrintsTheDocument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--skill"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(nil))
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr %q)", code, stderr.String())
	}
	if stdout.String() != docs.AgentsMD {
		t.Error("--skill output differs from docs/AGENTS.md; it must serve the file verbatim")
	}
	if stderr.Len() != 0 {
		t.Errorf("--skill wrote to stderr: %q", stderr.String())
	}
}
