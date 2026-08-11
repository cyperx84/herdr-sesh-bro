package main

import (
	"bytes"
	"context"
	"testing"
)

// TestResolveWorktreeURL_EnvOverridesArgument reproduces sesh-bro:392: the
// ENV VAR wins over the positional argument, not the other way around, even
// when the argument is ALSO a syntactically valid ref.
func TestResolveWorktreeURL_EnvOverridesArgument(t *testing.T) {
	env := &appEnv{getenv: fakeEnv(map[string]string{
		"HERDR_PLUGIN_CLICKED_URL": "https://github.com/env-owner/env-repo/issues/111",
	})}
	got := resolveWorktreeURL(env, []string{"arg-owner/arg-repo#222"})
	want := "https://github.com/env-owner/env-repo/issues/111"
	if got != want {
		t.Fatalf("resolveWorktreeURL() = %q, want %q (env must win)", got, want)
	}
}

// TestResolveWorktreeURL_ArgumentUsedWhenEnvUnset covers the fallback: with
// no HERDR_PLUGIN_CLICKED_URL, the positional argument is used.
func TestResolveWorktreeURL_ArgumentUsedWhenEnvUnset(t *testing.T) {
	env := &appEnv{getenv: fakeEnv(nil)}
	got := resolveWorktreeURL(env, []string{"owner/repo#42"})
	if got != "owner/repo#42" {
		t.Fatalf("resolveWorktreeURL() = %q, want %q", got, "owner/repo#42")
	}
}

// TestResolveWorktreeURL_BothAbsent covers neither source supplying a URL.
func TestResolveWorktreeURL_BothAbsent(t *testing.T) {
	env := &appEnv{getenv: fakeEnv(nil)}
	if got := resolveWorktreeURL(env, nil); got != "" {
		t.Fatalf("resolveWorktreeURL() = %q, want empty", got)
	}
}

// TestCmdWorktree_NoURLAtAll reproduces sesh-bro:393's usage message when
// neither the env var nor an argument supplies a URL — the ONE cmdWorktree
// exit path reachable without ever touching gh or the herdr socket, so it's
// the only one driven end-to-end here (see resolveWorktreeURL's own tests
// above for the precedence logic itself, tested in isolation to avoid a
// real `gh` network call).
func TestCmdWorktree_NoURLAtAll(t *testing.T) {
	var stderr bytes.Buffer
	env := &appEnv{getenv: fakeEnv(nil), stdout: &bytes.Buffer{}, stderr: &stderr}
	code := cmdWorktree(context.Background(), env, nil)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	want := "sesh-bro worktree: no URL (usage: sesh-bro worktree <github-url>)\n"
	if stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

// TestCmdWorktree_UnrecognizedURL reproduces the unrecognised-URL exit path
// (BEHAVIOUR.md §2.7): also reachable before any gh/herdr call, since ref
// parsing happens first.
func TestCmdWorktree_UnrecognizedURL(t *testing.T) {
	var stderr bytes.Buffer
	env := &appEnv{getenv: fakeEnv(nil), stdout: &bytes.Buffer{}, stderr: &stderr}
	code := cmdWorktree(context.Background(), env, []string{"not-a-url-at-all"})
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	want := "sesh-bro worktree: unrecognized URL not-a-url-at-all\n"
	if stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}
