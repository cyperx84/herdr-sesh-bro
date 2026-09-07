package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx/herdrtest"
)

// `rows` runs on every filter keypress, so it must read two files and do
// nothing else. If it ever opens the socket, a keypress costs a daemon round
// trip — which is exactly the cost the pre-rendered view files exist to remove.
func TestRowsTouchesNoSocket(t *testing.T) {
	s := herdrtest.Start(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "view"), []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rowsFile(dir, "blocked"), []byte("header\t-\tcounts\nagent\tp1\trow\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"rows", "--dir", dir}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	if got := len(s.CallOrder()); got != 0 {
		t.Errorf("rows made %d socket calls (%v), want none", got, s.CallOrder())
	}
}

// It emits the view the marker names, which is what keeps a filter alive across
// a close or create — those binds reload through this command.
func TestRowsEmitsTheMarkedView(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(rowsFile(dir, "all"), []byte("header\t-\tc\nagent\ta\tALL\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rowsFile(dir, "agents"), []byte("header\t-\tc\nagent\tb\tAGENTS\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "view"), []byte("agents"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"rows", "--dir", dir}, strings.NewReader(""), &stdout, &stderr, fakeEnv(nil)); code != 0 {
		t.Fatalf("code = %d (stderr %q)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "AGENTS") || strings.Contains(stdout.String(), "ALL") {
		t.Errorf("emitted the wrong view:\n%s", stdout.String())
	}
	if !strings.HasPrefix(stdout.String(), "header\t") {
		t.Errorf("output must start with the header row fzf pins:\n%s", stdout.String())
	}
}

// A view the renderer has not written yet is a race with its own first render,
// not a fault. Emitting nothing lets fzf show an empty list for a moment; the
// next push fills it. Failing would tear the picker down instead.
func TestRowsMissingFileIsNotAnError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"rows", "--dir", t.TempDir()}, strings.NewReader(""), &stdout, &stderr, fakeEnv(nil))
	if code != 0 {
		t.Errorf("code = %d, want 0 for an unwritten view (stderr %q)", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
}

func TestRowsRequiresDir(t *testing.T) {
	for _, args := range [][]string{{"rows"}, {"rows", "--dir"}, {"rows", "--nope"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, strings.NewReader(""), &stdout, &stderr, fakeEnv(nil)); code != 2 {
			t.Errorf("%v: code = %d, want 2", args, code)
		}
	}
}
