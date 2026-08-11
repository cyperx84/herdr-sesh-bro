package main

import "testing"

// TestExpandTilde_HomeSlash reproduces sesh-bro:339-340: "~/..." (including
// bare "~/") becomes $HOME + the rest.
func TestExpandTilde_HomeSlash(t *testing.T) {
	getenv := fakeEnv(map[string]string{"HOME": "/Users/cyperx"})
	cases := map[string]string{
		"~/":         "/Users/cyperx/",
		"~/projects": "/Users/cyperx/projects",
	}
	for in, want := range cases {
		if got := expandTilde(in, getenv); got != want {
			t.Errorf("expandTilde(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestExpandTilde_NoLeadingTildeUnchanged covers the common case: a path
// that isn't tilde-prefixed at all passes through untouched.
func TestExpandTilde_NoLeadingTildeUnchanged(t *testing.T) {
	if got := expandTilde("/absolute/path", fakeEnv(nil)); got != "/absolute/path" {
		t.Errorf("expandTilde() = %q, want unchanged", got)
	}
}

// TestExpandTilde_BareUserNoRealAccountIsNoop reproduces S10 (BEHAVIOUR.md
// §9): on macOS, `getent` does not exist at all, so "~user" forms are left
// COMPLETELY untouched. getentHome shells out via exec.LookPath, which
// reads the real process PATH (not the injected getenv closure — there is
// no way to fake that out without actually altering the test binary's own
// environment, which this package's tests deliberately never do), so this
// test is platform-observed rather than platform-forced: on macOS `getent`
// is genuinely absent (S10's own finding), and even where `getent` exists
// (Linux CI), "sesh-bro-test-nonexistent-user" is not a real account, so
// getentHome's own `ok` return is false either way — both paths converge on
// the same observable result this function documents.
func TestExpandTilde_BareUserNoRealAccountIsNoop(t *testing.T) {
	getenv := fakeEnv(nil)
	for _, in := range []string{"~", "~sesh-bro-test-nonexistent-user", "~sesh-bro-test-nonexistent-user/proj"} {
		if got := expandTilde(in, getenv); got != in {
			t.Errorf("expandTilde(%q) = %q, want unchanged (no resolvable account)", in, got)
		}
	}
}

// TestJqTruthy reproduces jq's `//` falsy set (null and `false` are falsy)
// for the values create.go's contextCWD tests against, PLUS this function's
// own documented simplification: a truthy NON-string (here, `true`) is
// treated as not-found, since only a string is ever a usable path — see
// jqTruthy's own doc comment for why that's a deliberate, unreachable-in-
// practice divergence from strict jq semantics rather than an oversight.
func TestJqTruthy(t *testing.T) {
	cases := []struct {
		in     any
		wantOK bool
	}{
		{nil, false},
		{false, false},
		{true, false},
		{"", true},
		{"/some/path", true},
	}
	for _, c := range cases {
		_, ok := jqTruthy(c.in)
		if ok != c.wantOK {
			t.Errorf("jqTruthy(%#v) ok = %v, want %v", c.in, ok, c.wantOK)
		}
	}
}

// TestContextCWD_TopLevelOnly reproduces BEHAVIOUR.md §2.4 point 2: `create`
// reads focused_pane_cwd/workspace_cwd from the TOP LEVEL of the context
// JSON only — unlike CurrentWorkspaceID's recursive `..` descent elsewhere.
// A workspace_id buried in a nested object must NOT be found by this
// function (it isn't even the key this function looks for, but the point
// stands: no recursion happens here at all).
func TestContextCWD_TopLevelOnly(t *testing.T) {
	got, ok := contextCWD(`{"nested":{"focused_pane_cwd":"/should/not/be/found"}}`)
	if ok {
		t.Fatalf("contextCWD found a nested key: %q", got)
	}
}

// TestContextCWD_FocusedPaneWinsOverWorkspaceCWD reproduces jq's `//`
// left-to-right precedence: focused_pane_cwd first, workspace_cwd only as
// fallback.
func TestContextCWD_FocusedPaneWinsOverWorkspaceCWD(t *testing.T) {
	got, ok := contextCWD(`{"focused_pane_cwd":"/pane","workspace_cwd":"/workspace"}`)
	if !ok || got != "/pane" {
		t.Fatalf("contextCWD() = (%q, %v), want (/pane, true)", got, ok)
	}
}

// TestContextCWD_FalsyFocusedFallsThrough covers a null focused_pane_cwd
// falling through to workspace_cwd (jq's `//` on a null LHS).
func TestContextCWD_FalsyFocusedFallsThrough(t *testing.T) {
	got, ok := contextCWD(`{"focused_pane_cwd":null,"workspace_cwd":"/workspace"}`)
	if !ok || got != "/workspace" {
		t.Fatalf("contextCWD() = (%q, %v), want (/workspace, true)", got, ok)
	}
}

// TestContextCWD_EmptyStringIsKept reproduces the same subtlety
// BEHAVIOUR.md §2.2.5 flags for terminal_title_stripped: jq's `//` keeps an
// EXPLICIT empty string rather than treating it as falsy.
func TestContextCWD_EmptyStringIsKept(t *testing.T) {
	got, ok := contextCWD(`{"focused_pane_cwd":"","workspace_cwd":"/workspace"}`)
	if !ok || got != "" {
		t.Fatalf("contextCWD() = (%q, %v), want (\"\", true) — explicit \"\" must be kept, not skipped", got, ok)
	}
}

// TestContextCWD_EmptyOrInvalidJSON covers the "" env var and unparseable
// JSON cases, both of which must degrade to (ok=false), not a panic.
func TestContextCWD_EmptyOrInvalidJSON(t *testing.T) {
	if _, ok := contextCWD(""); ok {
		t.Error(`contextCWD("") ok = true, want false`)
	}
	if _, ok := contextCWD("not json"); ok {
		t.Error(`contextCWD("not json") ok = true, want false`)
	}
}
