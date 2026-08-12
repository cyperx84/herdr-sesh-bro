package main

import (
	"bytes"
	"context"
	"errors"
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
