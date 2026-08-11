package herdrx

import "testing"

func TestCurrentWorkspaceID_EnvVarWins(t *testing.T) {
	got := CurrentWorkspaceID("w1", `{"workspace_id":"w2"}`)
	if got != "w1" {
		t.Fatalf("got %q, want %q — $HERDR_WORKSPACE_ID must win outright", got, "w1")
	}
}

func TestCurrentWorkspaceID_TopLevel(t *testing.T) {
	got := CurrentWorkspaceID("", `{"workspace_id":"w49"}`)
	if got != "w49" {
		t.Fatalf("got %q, want %q", got, "w49")
	}
}

func TestCurrentWorkspaceID_NestedAnyDepth(t *testing.T) {
	got := CurrentWorkspaceID("", `{"a":{"b":{"workspace_id":"w7"}}}`)
	if got != "w7" {
		t.Fatalf("got %q, want %q — recursive descent must find it at any depth", got, "w7")
	}
}

func TestCurrentWorkspaceID_FirstMatchInDocumentOrder(t *testing.T) {
	// Preorder DFS: the shallower/earlier match wins, matching jq's `..`
	// traversal order (parent before children, fields in source order).
	got := CurrentWorkspaceID("", `{"workspace_id":"first","nested":{"workspace_id":"second"}}`)
	if got != "first" {
		t.Fatalf("got %q, want %q", got, "first")
	}
}

func TestCurrentWorkspaceID_SecondFieldOrderRespected(t *testing.T) {
	// The object's OWN key wins over descending into an earlier sibling
	// field that also happens to contain a workspace_id — preorder visits
	// the node itself before any of its children.
	got := CurrentWorkspaceID("", `{"decoy":{"workspace_id":"deep"},"workspace_id":"shallow"}`)
	if got != "shallow" {
		t.Fatalf("got %q, want %q — the object's own key beats a nested one", got, "shallow")
	}
}

func TestCurrentWorkspaceID_NullValueSkippedScanContinues(t *testing.T) {
	// jq's `// empty` treats null as falsy: this node doesn't match, but
	// the search must continue rather than stopping here.
	got := CurrentWorkspaceID("", `{"workspace_id":null,"nested":{"workspace_id":"w9"}}`)
	if got != "w9" {
		t.Fatalf("got %q, want %q — null must be skipped, not treated as a match or a stop", got, "w9")
	}
}

func TestCurrentWorkspaceID_FalseValueSkipped(t *testing.T) {
	got := CurrentWorkspaceID("", `{"workspace_id":false,"nested":{"workspace_id":"w9"}}`)
	if got != "w9" {
		t.Fatalf("got %q, want %q", got, "w9")
	}
}

func TestCurrentWorkspaceID_ArrayDescent(t *testing.T) {
	got := CurrentWorkspaceID("", `{"items":[{"x":1},{"workspace_id":"wA"}]}`)
	if got != "wA" {
		t.Fatalf("got %q, want %q", got, "wA")
	}
}

func TestCurrentWorkspaceID_MalformedJSONYieldsEmpty(t *testing.T) {
	got := CurrentWorkspaceID("", `{not valid json`)
	if got != "" {
		t.Fatalf("got %q, want \"\" for malformed JSON", got)
	}
}

func TestCurrentWorkspaceID_NoMatchYieldsEmpty(t *testing.T) {
	got := CurrentWorkspaceID("", `{"other":"value"}`)
	if got != "" {
		t.Fatalf("got %q, want \"\"", got)
	}
}

func TestCurrentWorkspaceID_EmptyEnvAndContext(t *testing.T) {
	if got := CurrentWorkspaceID("", ""); got != "" {
		t.Fatalf("got %q, want \"\"", got)
	}
}

func TestCurrentWorkspaceID_EmptyStringEnvVarFallsThrough(t *testing.T) {
	// ${VAR:-default} semantics carried into this function's contract: an
	// EMPTY workspaceIDEnv is not "set", so it must not win over context.
	got := CurrentWorkspaceID("", `{"workspace_id":"w2"}`)
	if got != "w2" {
		t.Fatalf("got %q, want %q", got, "w2")
	}
}
