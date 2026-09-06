package herdrx

import (
	"testing"

	herdr "github.com/cyperx84/herdr-api"
)

func strp(s string) *string { return &s }

// wrapAgents lifts herdr-api agents into this package's extension type for
// the many fixtures that predate it and care about none of its extra fields.
// Tests that DO care (state_change_seq ordering) build []Agent directly.
func wrapAgents(in []herdr.Agent) []Agent {
	out := make([]Agent, len(in))
	for i, a := range in {
		out[i] = Agent{Agent: a}
	}
	return out
}

func TestWorkspaceRows_CurrentFirstThenNumber(t *testing.T) {
	// Input order deliberately scrambled; expected output is priority
	// (current==0, else 1) then number ascending — BEHAVIOUR.md §2.2.4.
	in := []herdr.Workspace{
		{ID: "w3", Number: 3},
		{ID: "w1", Number: 1},
		{ID: "w2", Number: 2}, // current
	}
	rows := WorkspaceRows(in, "w2", "")
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	got := []string{rows[0].Target, rows[1].Target, rows[2].Target}
	want := []string{"w2", "w1", "w3"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row order = %v, want %v", got, want)
		}
	}
}

func TestWorkspaceRows_StableTiebreak(t *testing.T) {
	// Equal priority and equal number: input order must be preserved,
	// matching jq's verified-stable sort_by (§2.2.4).
	in := []herdr.Workspace{
		{ID: "wa", Number: 5},
		{ID: "wb", Number: 5},
		{ID: "wc", Number: 5},
	}
	rows := WorkspaceRows(in, "", "")
	got := []string{rows[0].Target, rows[1].Target, rows[2].Target}
	want := []string{"wa", "wb", "wc"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stable order = %v, want %v", got, want)
		}
	}
}

func TestWorkspaceRows_EmptyCurrentDisablesPriority(t *testing.T) {
	// current == "" must not treat any workspace as priority 0, even one
	// that happens to be Focused — priority comes only from matching
	// `current`, and sort falls through to plain number order (§2.2.3).
	in := []herdr.Workspace{
		{ID: "w2", Number: 2, Focused: true},
		{ID: "w1", Number: 1},
	}
	rows := WorkspaceRows(in, "", "")
	if rows[0].Target != "w1" || rows[1].Target != "w2" {
		t.Fatalf("got order %v, want [w1 w2] (number order, no current preference)",
			[]string{rows[0].Target, rows[1].Target})
	}
}

func TestWorkspaceRows_HideCurrent(t *testing.T) {
	in := []herdr.Workspace{
		{ID: "w1", Number: 1},
		{ID: "w2", Number: 2},
	}
	rows := WorkspaceRows(in, "w1", "w1")
	if len(rows) != 1 || rows[0].Target != "w2" {
		t.Fatalf("got %+v, want only w2", rows)
	}
}

func TestWorkspaceRows_HideEmptyIsNoOp(t *testing.T) {
	// hide == "" drops nothing, even if current is also "" — this is how
	// --hide-current with no resolvable current becomes a no-op (§2.2.3).
	in := []herdr.Workspace{{ID: "w1", Number: 1}, {ID: "w2", Number: 2}}
	rows := WorkspaceRows(in, "", "")
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (hide=\"\" must not drop anything)", len(rows))
	}
}

func TestWorkspaceRows_LabelFallsBackToID(t *testing.T) {
	rows := WorkspaceRows([]herdr.Workspace{{ID: "w1", Label: ""}}, "", "")
	if rows[0].Label != "w1" {
		t.Fatalf("label = %q, want fallback to id %q", rows[0].Label, "w1")
	}
}

func TestWorkspaceRows_DetailBytes(t *testing.T) {
	// Exact byte reproduction including U+00B7 MIDDLE DOT, BEHAVIOUR.md §4.3.
	rows := WorkspaceRows([]herdr.Workspace{
		{ID: "w49", PaneCount: 2, TabCount: 1, Focused: true},
	}, "", "")
	want := "2p/1t · current"
	if rows[0].Detail != want {
		t.Fatalf("detail = %q, want %q", rows[0].Detail, want)
	}
}

func TestWorkspaceRows_DetailNotFocused(t *testing.T) {
	rows := WorkspaceRows([]herdr.Workspace{{ID: "w1", PaneCount: 3, TabCount: 1}}, "", "")
	if rows[0].Detail != "3p/1t" {
		t.Fatalf("detail = %q, want %q", rows[0].Detail, "3p/1t")
	}
}

func TestWorkspaceRows_StatusNormalizedToUnknown(t *testing.T) {
	rows := WorkspaceRows([]herdr.Workspace{{ID: "w1", Status: ""}}, "", "")
	if rows[0].Status != "unknown" {
		t.Fatalf("status = %q, want %q", rows[0].Status, "unknown")
	}
}

func TestAgentRows_UnnamedAgent_TargetIsPaneIDLabelIsKind(t *testing.T) {
	// §9 S18: target never falls back to the agent kind; label does.
	in := []herdr.Agent{{PaneID: "w49:p1", Agent: strp("claude")}}
	rows := AgentRows(wrapAgents(in), "", "", nil)
	if rows[0].Target != "w49:p1" {
		t.Fatalf("target = %q, want pane id %q", rows[0].Target, "w49:p1")
	}
	if rows[0].Label != "claude" {
		t.Fatalf("label = %q, want agent kind %q", rows[0].Label, "claude")
	}
}

func TestAgentRows_NamedAgent(t *testing.T) {
	in := []herdr.Agent{{PaneID: "w49:p1", Name: "pagefx", Agent: strp("codex")}}
	rows := AgentRows(wrapAgents(in), "", "", nil)
	if rows[0].Target != "pagefx" || rows[0].Label != "pagefx" {
		t.Fatalf("got target=%q label=%q, want both %q", rows[0].Target, rows[0].Label, "pagefx")
	}
}

func TestAgentRows_LaunchPendingKindIsQuestionMark(t *testing.T) {
	// Agent.Agent == nil for a launch-pending seat: detail's kind and
	// label's fallback both become "?" per the bash's `.agent // "?"`.
	in := []herdr.Agent{{PaneID: "w1:p2", Agent: nil}}
	rows := AgentRows(wrapAgents(in), "", "", nil)
	if rows[0].Label != "w1:p2" {
		t.Fatalf("label = %q, want pane id fallback %q", rows[0].Label, "w1:p2")
	}
	if rows[0].Detail[:2] != "? " {
		t.Fatalf("detail = %q, want to start with \"? \"", rows[0].Detail)
	}
}

func TestAgentRows_TrailingSpaceWhenTitleAndCWDEmpty(t *testing.T) {
	// §9 S20: exact string, including the trailing space before the dim
	// reset a sibling rendering package will wrap this in.
	in := []herdr.Agent{{PaneID: "p1", Agent: strp("claude"), TerminalTitleStripped: "", CWD: ""}}
	rows := AgentRows(wrapAgents(in), "", "", nil)
	want := "claude · "
	if rows[0].Detail != want {
		t.Fatalf("detail = %q, want %q", rows[0].Detail, want)
	}
}

func TestAgentRows_TitleFallsBackToCWD(t *testing.T) {
	in := []herdr.Agent{{PaneID: "p1", Agent: strp("claude"), CWD: "/tmp/x"}}
	rows := AgentRows(wrapAgents(in), "", "", nil)
	want := "claude · /tmp/x"
	if rows[0].Detail != want {
		t.Fatalf("detail = %q, want %q", rows[0].Detail, want)
	}
}

func TestAgentRows_StatusFilterAppliesAfterNormalization(t *testing.T) {
	// An agent with agent_status == "" normalizes to "unknown" BEFORE the
	// filter runs; filtering for StatusUnknown must therefore match it.
	in := []herdr.Agent{
		{PaneID: "p1", Status: ""},
		{PaneID: "p2", Status: herdr.StatusIdle},
	}
	rows := AgentRows(wrapAgents(in), "", "", []herdr.AgentStatus{herdr.StatusUnknown})
	if len(rows) != 1 || rows[0].Target != "p1" {
		t.Fatalf("got %+v, want only p1 (normalized unknown)", rows)
	}
}

func TestAgentRows_EmptyFilterMatchesEverything(t *testing.T) {
	in := []herdr.Agent{
		{PaneID: "p1", Status: herdr.StatusBlocked},
		{PaneID: "p2", Status: herdr.StatusIdle},
	}
	rows := AgentRows(wrapAgents(in), "", "", nil)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
}

func TestAgentRows_SortByPriorityThenRankThenName(t *testing.T) {
	// Name deliberately left unset on every agent here: setting it would
	// make Target fall back to Name instead of PaneID (agentTarget's own
	// rule, tested separately), which would silently defeat this test's
	// PaneID-based assertions.
	in := []herdr.Agent{
		{PaneID: "p-idle", WorkspaceID: "w2", Status: herdr.StatusIdle},
		{PaneID: "p-blocked", WorkspaceID: "w2", Status: herdr.StatusBlocked},
		{PaneID: "p-current", WorkspaceID: "w1", Status: herdr.StatusIdle},
	}
	rows := AgentRows(wrapAgents(in), "w1", "", nil)
	// current workspace's agent (w1) sorts first regardless of rank; within
	// w2, blocked (rank 0) sorts before idle (rank 3).
	got := []string{rows[0].Target, rows[1].Target, rows[2].Target}
	want := []string{"p-current", "p-blocked", "p-idle"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestAgentRows_UnnamedSortsBeforeNamedWithinRank(t *testing.T) {
	in := []herdr.Agent{
		{PaneID: "p1", Status: herdr.StatusIdle, Name: "zzz"},
		{PaneID: "p2", Status: herdr.StatusIdle, Name: ""},
	}
	rows := AgentRows(wrapAgents(in), "", "", nil)
	if rows[0].Target != "p2" {
		t.Fatalf("got %v first, want unnamed agent (p2) first", rows[0].Target)
	}
}

func TestAgentRows_HideCurrent(t *testing.T) {
	in := []herdr.Agent{
		{PaneID: "p1", WorkspaceID: "w1"},
		{PaneID: "p2", WorkspaceID: "w2"},
	}
	rows := AgentRows(wrapAgents(in), "w1", "w1", nil)
	if len(rows) != 1 || rows[0].Target != "p2" {
		t.Fatalf("got %+v, want only p2", rows)
	}
}

func TestKnownCWDs(t *testing.T) {
	panes := []herdr.Pane{{CWD: "/a"}, {CWD: "/b"}, {CWD: ""}}
	known := KnownCWDs(panes)
	if !known["/a"] || !known["/b"] {
		t.Fatalf("known = %+v, want /a and /b present", known)
	}
	if known[""] {
		t.Fatalf("known[\"\"] should not be set")
	}
}

func TestDirRows_DedupIsByteExact(t *testing.T) {
	// No symlink resolution, no trailing-slash normalization —
	// BEHAVIOUR.md §2.2.6 point 3: "/a/b/" and "/a/b" are different paths.
	known := map[string]bool{"/a/b": true}
	rows := DirRows([]string{"/a/b", "/a/b/"}, known, "")
	if len(rows) != 1 || rows[0].Target != "/a/b/" {
		t.Fatalf("got %+v, want only /a/b/ to survive (byte-exact dedup)", rows)
	}
}

func TestDirRows_SkipsEmptyLines(t *testing.T) {
	rows := DirRows([]string{"", "/a"}, nil, "")
	if len(rows) != 1 || rows[0].Target != "/a" {
		t.Fatalf("got %+v, want only /a", rows)
	}
}

func TestDirRows_PreservesInputOrder(t *testing.T) {
	rows := DirRows([]string{"/z", "/a", "/m"}, nil, "")
	got := []string{rows[0].Target, rows[1].Target, rows[2].Target}
	want := []string{"/z", "/a", "/m"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v (zoxide frecency order preserved, not sorted)", got, want)
		}
	}
}

func TestDirRows_StatusAndDetailFields(t *testing.T) {
	rows := DirRows([]string{"/Users/cyperx/proj"}, nil, "")
	r := rows[0]
	if r.Type != RowDir || r.Status != dirStatus || r.Label != "proj" || r.Detail != "/Users/cyperx/proj" {
		t.Fatalf("got %+v", r)
	}
}

func TestBasename(t *testing.T) {
	cases := []struct{ path, want string }{
		{"/Users/cyperx/proj", "proj"},
		{"/", "/"}, // §8 edge case #13
		{"/Users/cyperx/proj/", "/Users/cyperx/proj/"}, // trailing slash: computed base is "", falls back
		{"noslash", "noslash"},
	}
	for _, c := range cases {
		if got := Basename(c.path); got != c.want {
			t.Errorf("Basename(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestBlacklistMatches_SplitsOnColonOnly(t *testing.T) {
	// A blacklist entry containing a space is ONE glob token here (unlike
	// bash, which further word-splits it — §9 S6, divergence #1).
	if !blacklistMatches("/a b/*", "/a b/c") {
		t.Fatalf("expected the space-containing token to match as a single glob")
	}
}

func TestBlacklistMatches_GlobDoesNotCrossSlash(t *testing.T) {
	// Divergence #2 (documented on blacklistMatches): filepath.Match's `*`
	// stops at '/', unlike bash's `[[ == ]]` glob.
	if blacklistMatches("/tmp/*", "/tmp/a/b") {
		t.Fatalf("filepath.Match should not let * cross a path separator")
	}
	if !blacklistMatches("/tmp/*", "/tmp/a") {
		t.Fatalf("expected /tmp/* to match /tmp/a")
	}
}

func TestBlacklistMatches_Empty(t *testing.T) {
	if blacklistMatches("", "/anything") {
		t.Fatalf("empty blacklist must match nothing")
	}
}

// Within one status rank the newest state change leads. This is the 0.4.0
// tiebreak (BEHAVIOUR.md §10): of several blocked agents, the one that just
// started asking is the one worth landing on, and state_change_seq is the
// only recency signal herdr exposes.
func TestAgentRowsOrdersByStateChangeSeqWithinRank(t *testing.T) {
	in := []Agent{
		{Agent: herdr.Agent{Name: "aaa", PaneID: "p1", Status: herdr.StatusBlocked}, StateChangeSeq: 10},
		{Agent: herdr.Agent{Name: "zzz", PaneID: "p2", Status: herdr.StatusBlocked}, StateChangeSeq: 40},
		{Agent: herdr.Agent{Name: "mmm", PaneID: "p3", Status: herdr.StatusBlocked}, StateChangeSeq: 25},
	}
	rows := AgentRows(in, "", "", nil)
	got := []string{rows[0].Target, rows[1].Target, rows[2].Target}
	want := []string{"zzz", "mmm", "aaa"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v (newest state change first)", got, want)
		}
	}
}

// Rank still beats recency: a blocked agent that changed long ago outranks a
// working agent that changed a moment ago.
func TestAgentRowsRankBeatsStateChangeSeq(t *testing.T) {
	in := []Agent{
		{Agent: herdr.Agent{Name: "busy", PaneID: "p1", Status: herdr.StatusWorking}, StateChangeSeq: 99},
		{Agent: herdr.Agent{Name: "stuck", PaneID: "p2", Status: herdr.StatusBlocked}, StateChangeSeq: 1},
	}
	rows := AgentRows(in, "", "", nil)
	if rows[0].Target != "stuck" {
		t.Fatalf("first = %q, want %q — blocked must outrank a newer working agent", rows[0].Target, "stuck")
	}
}

// Name remains the final tiebreak, so equal rank and equal seq is still a
// total, deterministic order.
func TestAgentRowsNameBreaksEqualSeq(t *testing.T) {
	in := []Agent{
		{Agent: herdr.Agent{Name: "beta", PaneID: "p1", Status: herdr.StatusIdle}, StateChangeSeq: 7},
		{Agent: herdr.Agent{Name: "alpha", PaneID: "p2", Status: herdr.StatusIdle}, StateChangeSeq: 7},
	}
	rows := AgentRows(in, "", "", nil)
	if rows[0].Target != "alpha" {
		t.Fatalf("first = %q, want %q", rows[0].Target, "alpha")
	}
}

// SplitAttention pulls out blocked and done — herdr's two "has something for
// you that you haven't seen" states — and leaves every other row untouched,
// each group keeping its incoming order.
func TestSplitAttention(t *testing.T) {
	rows := []Row{
		{Type: RowWorkspace, Target: "w1", Status: "idle"},
		{Type: RowAgent, Target: "a-working", Status: "working"},
		{Type: RowAgent, Target: "a-blocked", Status: "blocked"},
		{Type: RowAgent, Target: "a-idle", Status: "idle"},
		{Type: RowAgent, Target: "a-done", Status: "done"},
		{Type: RowDir, Target: "/tmp", Status: "-"},
	}
	attention, rest := SplitAttention(rows)

	wantAttention := []string{"a-blocked", "a-done"}
	if len(attention) != len(wantAttention) {
		t.Fatalf("attention = %d rows, want %d", len(attention), len(wantAttention))
	}
	for i, w := range wantAttention {
		if attention[i].Target != w {
			t.Errorf("attention[%d] = %q, want %q", i, attention[i].Target, w)
		}
	}

	wantRest := []string{"w1", "a-working", "a-idle", "/tmp"}
	if len(rest) != len(wantRest) {
		t.Fatalf("rest = %d rows, want %d", len(rest), len(wantRest))
	}
	for i, w := range wantRest {
		if rest[i].Target != w {
			t.Errorf("rest[%d] = %q, want %q", i, rest[i].Target, w)
		}
	}
}

// A workspace row whose status happens to be blocked is NOT hoisted: the
// hoist is about agents that want you, and a workspace inherits its status
// from the panes inside it, so hoisting it would duplicate the signal.
func TestSplitAttentionIgnoresNonAgentRows(t *testing.T) {
	rows := []Row{{Type: RowWorkspace, Target: "w1", Status: "blocked"}}
	attention, rest := SplitAttention(rows)
	if len(attention) != 0 {
		t.Errorf("attention = %v, want none", attention)
	}
	if len(rest) != 1 {
		t.Errorf("rest = %v, want the workspace row", rest)
	}
}

// Badges land only on the rows where the number changes what you do. An agent
// blocked for nine minutes is a different situation from one blocked for nine
// seconds; a working agent's age is noise competing with those.
func TestWithAgesOnlyBadgesAttentionRows(t *testing.T) {
	rows := []Row{
		{Type: RowAgent, Target: "b", Status: "blocked", Detail: "claude · x", PaneID: "p1"},
		{Type: RowAgent, Target: "d", Status: "done", Detail: "claude · y", PaneID: "p2"},
		{Type: RowAgent, Target: "w", Status: "working", Detail: "claude · z", PaneID: "p3"},
		{Type: RowAgent, Target: "i", Status: "idle", Detail: "claude · q", PaneID: "p4"},
		{Type: RowWorkspace, Target: "w1", Status: "blocked", Detail: "1p/1t", PaneID: ""},
	}
	got := WithAges(rows, map[string]string{"p1": "9m", "p2": "3h", "p3": "1s", "p4": "2d"})

	if got[0].Detail != "claude · x · 9m" {
		t.Errorf("blocked detail = %q, want a badge", got[0].Detail)
	}
	if got[1].Detail != "claude · y · 3h" {
		t.Errorf("done detail = %q, want a badge", got[1].Detail)
	}
	if got[2].Detail != "claude · z" {
		t.Errorf("working row was badged: %q", got[2].Detail)
	}
	if got[3].Detail != "claude · q" {
		t.Errorf("idle row was badged: %q", got[3].Detail)
	}
	if got[4].Detail != "1p/1t" {
		t.Errorf("workspace row was badged: %q", got[4].Detail)
	}
}

// A pane with no recorded age is left exactly as it was: a missed transition
// costs a badge, never a wrong one.
func TestWithAgesLeavesUnknownPanesAlone(t *testing.T) {
	rows := []Row{{Type: RowAgent, Target: "b", Status: "blocked", Detail: "claude · x", PaneID: "p1"}}
	got := WithAges(rows, map[string]string{"other": "9m"})
	if got[0].Detail != "claude · x" {
		t.Errorf("detail = %q, want it untouched", got[0].Detail)
	}
	if same := WithAges(rows, nil); same[0].Detail != "claude · x" {
		t.Errorf("nil ages changed the detail: %q", same[0].Detail)
	}
}
