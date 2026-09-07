package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx/herdrtest"
	"github.com/cyperx84/herdr-sesh-bro/internal/output"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCmdClose_MissingArgs mirrors cmd_connect's own TYPE/TARGET contract
// (connect.go, BEHAVIOUR.md §9 S9): fewer than two arguments is our own
// message and exit 1, never a panic on args[0]/args[1].
func TestCmdClose_MissingArgs(t *testing.T) {
	var stderr bytes.Buffer
	env := &appEnv{getenv: fakeEnv(nil), stdout: &bytes.Buffer{}, stderr: &stderr}
	code := cmdClose(context.Background(), env, []string{"workspace"})
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	want := "sesh-bro: close: missing TYPE/TARGET arguments\n"
	if stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

// TestCloseRow_AgentIsNoOp is the task's own guard, stated directly: "Only
// workspace rows are closable... closing must be a no-op with a clear
// message for those, never an error that kills the picker." client is nil
// here — closeRow must never dereference it for an agent row, so a nil
// client proves the agent branch never reaches the herdr call.
func TestCloseRow_AgentIsNoOp(t *testing.T) {
	var stderr bytes.Buffer
	env := &appEnv{getenv: fakeEnv(nil), stdout: &bytes.Buffer{}, stderr: &stderr}
	err := closeRow(context.Background(), env, nil, nil, "agent", "claude")
	if err != nil {
		t.Fatalf("closeRow(agent) returned an error, want nil (no-op): %v", err)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("cannot close a agent row")) {
		t.Fatalf("stderr = %q, want a message naming the row kind", stderr.String())
	}
}

// TestCloseRow_DirIsNoOp is the agent case's twin for zoxide directory rows.
func TestCloseRow_DirIsNoOp(t *testing.T) {
	var stderr bytes.Buffer
	env := &appEnv{getenv: fakeEnv(nil), stdout: &bytes.Buffer{}, stderr: &stderr}
	err := closeRow(context.Background(), env, nil, nil, "dir", "/Users/cyperx/github/herdr-sesh-bro")
	if err != nil {
		t.Fatalf("closeRow(dir) returned an error, want nil (no-op): %v", err)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("cannot close a dir row")) {
		t.Fatalf("stderr = %q, want a message naming the row kind", stderr.String())
	}
}

// TestCloseWorkspaceRow_RefusesCurrentWorkspace is the task's named guard:
// "never close the workspace the picker itself is running in. Doing so
// kills the picker mid-action." current is resolved from HERDR_WORKSPACE_ID
// exactly like the picker's own "current" marker (herdrx.CurrentWorkspaceID
// — see list.go, last.go), so setting it equal to target must refuse the
// close as a no-op, BEFORE ever touching client — client is nil here to
// prove that.
func TestCloseWorkspaceRow_RefusesCurrentWorkspace(t *testing.T) {
	var stderr bytes.Buffer
	env := &appEnv{
		getenv: fakeEnv(map[string]string{"HERDR_WORKSPACE_ID": "w49"}),
		stdout: &bytes.Buffer{},
		stderr: &stderr,
	}
	err := closeWorkspaceRow(context.Background(), env, nil, nil, "w49")
	if err != nil {
		t.Fatalf("closeWorkspaceRow(current) returned an error, want nil (refused as a no-op): %v", err)
	}
	want := "sesh-bro: refusing to close the current workspace w49\n"
	if stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

// TestCloseWorkspaceRow_DifferentWorkspaceNotGuarded proves the guard is
// scoped to the CURRENT workspace only — closing any other id must fall
// through to the real herdr call (here surfaced as openErr, since there is
// no live socket in this test), not be silently swallowed as if it were
// also "current".
func TestCloseWorkspaceRow_DifferentWorkspaceNotGuarded(t *testing.T) {
	var stderr bytes.Buffer
	env := &appEnv{
		getenv: fakeEnv(map[string]string{"HERDR_WORKSPACE_ID": "w49"}),
		stdout: &bytes.Buffer{},
		stderr: &stderr,
	}
	wantErr := errors.New("dial failed")
	err := closeWorkspaceRow(context.Background(), env, nil, wantErr, "w3R")
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v (guard must not fire for a different workspace)", err, wantErr)
	}
	want := "sesh-bro: failed to close workspace w3R\n"
	if stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

// TestCloseWorkspaceRow_OpenErrPassesThrough covers closeWorkspaceRow's
// dial-failure short-circuit, the same pattern worktree.go's
// TestCreateGitWorktree_OpenErrPassesThrough already establishes for this
// codebase: when opening the client failed, return that exact error and
// never dereference the (nil) client. HERDR_WORKSPACE_ID is unset here, so
// current == "" and the guard above is a no-op regardless of target
// (BEHAVIOUR.md §1.7: an empty current disables it entirely).
func TestCloseWorkspaceRow_OpenErrPassesThrough(t *testing.T) {
	var stderr bytes.Buffer
	env := &appEnv{getenv: fakeEnv(nil), stdout: &bytes.Buffer{}, stderr: &stderr}
	wantErr := errors.New("dial failed")
	err := closeWorkspaceRow(context.Background(), env, nil, wantErr, "w49")
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	want := "sesh-bro: failed to close workspace w49\n"
	if stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

// writeRowsFile writes a TSV rows file of the shape fzf's {+f} produces.
func writeRowsFile(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rows.tsv")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// An irreversible action with nobody to ask is exactly when to stop. Without
// a terminal to confirm on, close must refuse rather than assume yes — an
// agent or a script that meant it says --yes.
func TestCloseFromFileRefusesWithoutATerminal(t *testing.T) {
	s := herdrtest.Start(t)
	s.Handle("workspace.list", func(json.RawMessage) (any, error) {
		return map[string]any{"workspaces": []map[string]any{}}, nil
	})
	s.Handle("workspace.close", func(json.RawMessage) (any, error) { return map[string]any{}, nil })

	rows := writeRowsFile(t, "workspace\tw1\t◆ alpha")
	var stdout, stderr bytes.Buffer
	code := run([]string{"close", "--from-file", rows}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s)))

	if len(s.Calls("workspace.close")) != 0 {
		t.Error("closed a workspace with no confirmation and no --yes")
	}
	if code != 0 {
		t.Errorf("code = %d; declining to close is not a failure", code)
	}
	if !strings.Contains(stderr.String(), "--yes") {
		t.Errorf("stderr does not tell the caller how to proceed: %q", stderr.String())
	}
}

// --yes is how a caller states the intent explicitly. Bulk close then works.
func TestCloseFromFileWithYesClosesEvery(t *testing.T) {
	s := herdrtest.Start(t)
	s.Handle("workspace.list", func(json.RawMessage) (any, error) {
		return map[string]any{"workspaces": []map[string]any{}}, nil
	})
	s.Handle("workspace.close", func(json.RawMessage) (any, error) { return map[string]any{}, nil })

	rows := writeRowsFile(t, "workspace\tw1\t◆ alpha", "workspace\tw2\t◆ beta")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"close", "--from-file", rows, "--yes"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s))); code != 0 {
		t.Fatalf("code = %d (stderr %q)", code, stderr.String())
	}
	if n := len(s.Calls("workspace.close")); n != 2 {
		t.Errorf("closed %d workspaces, want 2", n)
	}
}

// A user who selected an agent alongside a workspace and saw only "closed 1"
// could reasonably conclude the agent was closed too. Say what was skipped.
func TestCloseFromFileReportsSkippedRows(t *testing.T) {
	s := herdrtest.Start(t)
	s.Handle("workspace.list", func(json.RawMessage) (any, error) {
		return map[string]any{"workspaces": []map[string]any{}}, nil
	})
	s.Handle("workspace.close", func(json.RawMessage) (any, error) { return map[string]any{}, nil })

	rows := writeRowsFile(t,
		"header\t-\t● 1 blocked",
		"agent\tbuilder\t● builder",
		"dir\t/tmp/x\t▸ x",
		"workspace\tw1\t◆ alpha")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"close", "--from-file", rows, "--yes"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s))); code != 0 {
		t.Fatalf("code = %d (stderr %q)", code, stderr.String())
	}
	if n := len(s.Calls("workspace.close")); n != 1 {
		t.Errorf("closed %d, want only the one workspace row", n)
	}
	for _, want := range []string{"agent builder", "dir /tmp/x"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr does not report skipping %q: %q", want, stderr.String())
		}
	}
	// The pinned counts row is chrome and unselectable; mentioning it would be
	// noise about something the user cannot have chosen.
	if strings.Contains(stderr.String(), "header") {
		t.Errorf("reported the header row as skipped: %q", stderr.String())
	}
}

// Selecting only non-workspace rows is a real answer, not a fault: nothing
// closeable was chosen.
func TestCloseFromFileWithNoWorkspaceRows(t *testing.T) {
	s := herdrtest.Start(t)
	rows := writeRowsFile(t, "agent\tbuilder\t● builder")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"close", "--from-file", rows, "--yes"}, strings.NewReader(""), &stdout, &stderr, fakeEnv(fakeHerdrEnv(s))); code != output.ExitEmpty {
		t.Errorf("code = %d, want %d (ExitEmpty)", code, output.ExitEmpty)
	}
}
