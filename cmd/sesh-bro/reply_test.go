package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadReplies_MissingFileIsTheDefaults: a reply key that silently stopped
// working would be diagnosed as a broken keybind, so an absent file degrades
// to the built-in answers rather than to nothing.
func TestLoadReplies_MissingFileIsTheDefaults(t *testing.T) {
	got := loadReplies(filepath.Join(t.TempDir(), "nope.txt"))
	assertArgs(t, got, defaultReplies)
}

// TestLoadReplies_SkipsBlanksAndComments so the file can explain itself.
func TestLoadReplies_SkipsBlanksAndComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "replies.txt")
	body := "# the first one is what the key sends\n\nyes, go ahead\n  \nrun: make test\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	assertArgs(t, loadReplies(path), []string{"yes, go ahead", "run: make test"})
}

// TestLoadReplies_ColonsAndCommasSurvive is the reason this is a file and not
// an environment variable: every list variable in this project splits on one
// or the other, and a canned reply is free text that may contain both.
func TestLoadReplies_ColonsAndCommasSurvive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "replies.txt")
	if err := os.WriteFile(path, []byte("run: make check, then push\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertArgs(t, loadReplies(path), []string{"run: make check, then push"})
}

// TestLoadReplies_EmptyFileIsTheDefaults: a file containing only comments is
// not a request for no replies at all.
func TestLoadReplies_EmptyFileIsTheDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "replies.txt")
	if err := os.WriteFile(path, []byte("# nothing here\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertArgs(t, loadReplies(path), defaultReplies)
}

func TestParseReplyFlags(t *testing.T) {
	f, err := parseReplyFlags([]string{"builder"})
	if err != nil {
		t.Fatal(err)
	}
	if f.target != "builder" || f.index != 1 {
		t.Fatalf("got target=%q index=%d, want builder/1", f.target, f.index)
	}

	f, err = parseReplyFlags([]string{"builder", "3", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if f.index != 3 || !f.asJSON {
		t.Fatalf("got index=%d asJSON=%v, want 3/true", f.index, f.asJSON)
	}

	for _, args := range [][]string{
		{},                    // no target
		{"builder", "0"},      // replies are 1-based
		{"builder", "-1"},     // parsed as an unknown flag, not a number
		{"builder", "two"},    // not a number
		{"builder", "1", "x"}, // too many positionals
		{"builder", "--nope"}, // unknown flag
	} {
		if _, err := parseReplyFlags(args); err == nil {
			t.Errorf("parseReplyFlags(%v) = nil error, want one", args)
		}
	}
}

// TestParseReplyFlags_ListNeedsNoTarget: --list is a question about
// configuration, not about an agent.
func TestParseReplyFlags_ListNeedsNoTarget(t *testing.T) {
	f, err := parseReplyFlags([]string{"--list"})
	if err != nil {
		t.Fatal(err)
	}
	if !f.list {
		t.Fatal("--list did not set list")
	}
}
