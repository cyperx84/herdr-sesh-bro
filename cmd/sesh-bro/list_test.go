package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	herdr "github.com/cyperx84/herdr-api"

	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx"
)

// TestParseListFlags_StatusClearsEarlierSourceFlags is S3 (BEHAVIOUR.md §9):
// a status flag clears want_ws/want_dir at the MOMENT it is parsed, so
// argument ORDER changes the result — not just presence.
func TestParseListFlags_StatusClearsEarlierSourceFlags(t *testing.T) {
	// `list --dirs --blocked` → agents only: --blocked clears want_dir,
	// which was set a moment earlier by --dirs.
	f, err := parseListFlags([]string{"--dirs", "--blocked"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.wantWS || f.wantDir || !f.wantAgent {
		t.Fatalf("--dirs --blocked: got wantWS=%v wantDir=%v wantAgent=%v, want false/false/true", f.wantWS, f.wantDir, f.wantAgent)
	}

	// `list --blocked --dirs` → blocked agents AND dirs: --dirs runs AFTER
	// --blocked and re-enables want_dir.
	f, err = parseListFlags([]string{"--blocked", "--dirs"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.wantWS || !f.wantDir || !f.wantAgent {
		t.Fatalf("--blocked --dirs: got wantWS=%v wantDir=%v wantAgent=%v, want false/true/true", f.wantWS, f.wantDir, f.wantAgent)
	}
}

// TestParseListFlags_NoSourceFlagEnablesAll reproduces sesh-bro:144: with no
// source flag at all, every source is enabled by default.
func TestParseListFlags_NoSourceFlagEnablesAll(t *testing.T) {
	f, err := parseListFlags([]string{"--hide-current", "--json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !f.wantWS || !f.wantAgent || !f.wantDir {
		t.Fatalf("got wantWS=%v wantAgent=%v wantDir=%v, want all true", f.wantWS, f.wantAgent, f.wantDir)
	}
	if !f.hideCurrent || !f.asJSON {
		t.Fatalf("got hideCurrent=%v asJSON=%v, want both true", f.hideCurrent, f.asJSON)
	}
}

// TestParseListFlags_MultipleStatusesAccumulate reproduces sesh-bro:295:
// `--blocked --working` matches either status, not just the last one seen.
func TestParseListFlags_MultipleStatusesAccumulate(t *testing.T) {
	f, err := parseListFlags([]string{"--blocked", "--working"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []herdr.AgentStatus{herdr.StatusBlocked, herdr.StatusWorking}
	if len(f.statuses) != len(want) || f.statuses[0] != want[0] || f.statuses[1] != want[1] {
		t.Fatalf("statuses = %v, want %v", f.statuses, want)
	}
}

// TestParseListFlags_UnknownFlag reproduces sesh-bro:140: an unrecognised
// flag is an error (mapped by cmdList to exit 2), and — matching bash's
// left-to-right scan aborting immediately — no flag after it is processed.
func TestParseListFlags_UnknownFlag(t *testing.T) {
	_, err := parseListFlags([]string{"--workspaces", "--nope"})
	if err == nil {
		t.Fatal("want an error for an unknown flag")
	}
	if !strings.Contains(err.Error(), "--nope") {
		t.Fatalf("error %q does not name the bad flag", err.Error())
	}
}

func mkRow(kind herdrx.RowType, target string) herdrx.Row {
	return herdrx.Row{Type: kind, Target: target, Label: target}
}

// TestAssembleBlocks_DefaultOrder reproduces sesh-bro:226-228: with no
// SESH_BRO_SORT_ORDER, the fixed order is workspaces, agents, dirs.
func TestAssembleBlocks_DefaultOrder(t *testing.T) {
	ws := []herdrx.Row{mkRow(herdrx.RowWorkspace, "w1")}
	ag := []herdrx.Row{mkRow(herdrx.RowAgent, "a1")}
	dir := []herdrx.Row{mkRow(herdrx.RowDir, "/tmp")}
	got := assembleBlocks("", ws, ag, dir, nil)
	wantTargets := []string{"w1", "a1", "/tmp"}
	assertTargets(t, got, wantTargets)
}

// TestAssembleBlocks_CustomOrderReorders reproduces sesh-bro:216-224 with a
// custom, fully-specified order.
func TestAssembleBlocks_CustomOrderReorders(t *testing.T) {
	ws := []herdrx.Row{mkRow(herdrx.RowWorkspace, "w1")}
	ag := []herdrx.Row{mkRow(herdrx.RowAgent, "a1")}
	dir := []herdrx.Row{mkRow(herdrx.RowDir, "/tmp")}
	got := assembleBlocks("agents,dirs,workspaces", ws, ag, dir, nil)
	assertTargets(t, got, []string{"a1", "/tmp", "w1"})
}

// TestAssembleBlocks_DropsUnlistedBlocks is S5 (BEHAVIOUR.md §9): a
// non-empty SORT_ORDER that OMITS a token drops that block entirely — it is
// not merely deprioritised, and it does not fall back to "include everything
// not mentioned".
func TestAssembleBlocks_DropsUnlistedBlocks(t *testing.T) {
	ws := []herdrx.Row{mkRow(herdrx.RowWorkspace, "w1")}
	ag := []herdrx.Row{mkRow(herdrx.RowAgent, "a1")}
	dir := []herdrx.Row{mkRow(herdrx.RowDir, "/tmp")}
	got := assembleBlocks("agents", ws, ag, dir, nil)
	assertTargets(t, got, []string{"a1"})
}

// TestAssembleBlocks_RepeatedTokenDuplicatesBlock is S5's other half: a
// token named twice duplicates that block's rows.
func TestAssembleBlocks_RepeatedTokenDuplicatesBlock(t *testing.T) {
	ag := []herdrx.Row{mkRow(herdrx.RowAgent, "a1")}
	got := assembleBlocks("agents,agents", nil, ag, nil, nil)
	assertTargets(t, got, []string{"a1", "a1"})
}

// TestAssembleBlocks_UnrecognisedTokenIgnored reproduces the case
// statement's silent no-op for a token that isn't workspaces/agents/dirs.
func TestAssembleBlocks_UnrecognisedTokenIgnored(t *testing.T) {
	ag := []herdrx.Row{mkRow(herdrx.RowAgent, "a1")}
	got := assembleBlocks("bogus,agents", nil, ag, nil, nil)
	assertTargets(t, got, []string{"a1"})
}

func assertTargets(t *testing.T, rows []herdrx.Row, want []string) {
	t.Helper()
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d: %v", len(rows), len(want), rows)
	}
	for i, r := range rows {
		if r.Target != want[i] {
			t.Errorf("row %d target = %q, want %q", i, r.Target, want[i])
		}
	}
}

// TestBashSplit_TrailingDelimiterAbsorbed pins the same "IFS read -a"
// splitting contract internal/picker's bashSplitColon documents, applied
// here to SESH_BRO_SORT_ORDER's comma separator instead of the alias
// mechanism's colon.
func TestBashSplit_TrailingDelimiterAbsorbed(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"agents,", []string{"agents"}},
		{"agents,,dirs", []string{"agents", "", "dirs"}},
		{",agents", []string{"", "agents"}},
		{"agents,dirs,workspaces", []string{"agents", "dirs", "workspaces"}},
	}
	for _, c := range cases {
		got := bashSplit(c.in, ',')
		if len(got) != len(c.want) {
			t.Errorf("bashSplit(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("bashSplit(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

// TestWriteJSONRows_ConcatenatedNotArray is S8 (BEHAVIOUR.md §9): each row
// is a separate pretty-printed object, two-space indented, with NO
// enclosing array and no separator between objects — verified against the
// verbatim sample bytes in BEHAVIOUR.md §2.2.9.
func TestWriteJSONRows_ConcatenatedNotArray(t *testing.T) {
	rows := []herdrx.Row{
		{Type: herdrx.RowWorkspace, Target: "w49", Status: "working", Label: "ORG", Detail: "2p/1t · current"},
		{Type: herdrx.RowWorkspace, Target: "w3R", Status: "unknown", Label: "DOTFILES", Detail: "3p/1t"},
	}
	var buf bytes.Buffer
	writeJSONRows(&buf, rows)
	want := `{
  "type": "workspace",
  "target": "w49",
  "status": "working",
  "label": "ORG",
  "detail": "2p/1t · current"
}
{
  "type": "workspace",
  "target": "w3R",
  "status": "unknown",
  "label": "DOTFILES",
  "detail": "3p/1t"
}
`
	if buf.String() != want {
		t.Fatalf("writeJSONRows() =\n%s\nwant\n%s", buf.String(), want)
	}
}

// TestWriteJSONRows_EmptyProducesNoOutput reproduces §2.2.9: zero rows means
// zero bytes written, not "[]" or "null".
func TestWriteJSONRows_EmptyProducesNoOutput(t *testing.T) {
	var buf bytes.Buffer
	writeJSONRows(&buf, nil)
	if buf.Len() != 0 {
		t.Fatalf("buf = %q, want empty", buf.String())
	}
}

// TestWriteJSONRows_HTMLNotEscaped: jq's plain (non -c) output never
// HTML-escapes &, <, > — encoding/json's default DOES, so SetEscapeHTML(false)
// is load-bearing, not cosmetic.
func TestWriteJSONRows_HTMLNotEscaped(t *testing.T) {
	rows := []herdrx.Row{{Type: herdrx.RowWorkspace, Target: "w1", Status: "idle", Label: "A&B<C>", Detail: "x"}}
	var buf bytes.Buffer
	writeJSONRows(&buf, rows)
	want := "A&B<C>"
	if !strings.Contains(buf.String(), want) {
		t.Fatalf("writeJSONRows HTML-escaped the label, want the literal bytes %q preserved in: %s", want, buf.String())
	}
}

// TestPanesToExternal proves the []herdr.Pane -> []external.Pane narrowing
// copies WorkspaceID/CWD field for field — the two types are NOT
// structurally interchangeable in Go despite sharing field names.
func TestPanesToExternal(t *testing.T) {
	in := []herdr.Pane{{WorkspaceID: "w1", CWD: "/a"}, {WorkspaceID: "w2", CWD: "/b"}}
	out := panesToExternal(in)
	if len(out) != 2 || out[0].WorkspaceID != "w1" || out[0].CWD != "/a" || out[1].WorkspaceID != "w2" || out[1].CWD != "/b" {
		t.Fatalf("panesToExternal() = %+v", out)
	}
}

// TestExpandViewDir_SubstitutesTheMarkersView proves the star bind's reload
// renders the view the user is looking at, not the default one.
func TestExpandViewDir_SubstitutesTheMarkersView(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "view"), []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := expandViewDir([]string{"--header", "--view-dir", dir, "--hide-current"})
	want := []string{"--header", "--blocked", "--hide-current"}
	assertArgs(t, got, want)

	got = expandViewDir([]string{"--header", "--view-dir=" + dir})
	assertArgs(t, got, []string{"--header", "--blocked"})
}

// TestExpandViewDir_MissingMarkerIsTheFullList: an absent or unrecognised
// marker degrades to "all", which viewFlags renders as no source flags at all
// — the full list. Degrading to an EMPTY list instead would make a star look
// like it deleted every row.
func TestExpandViewDir_MissingMarkerIsTheFullList(t *testing.T) {
	got := expandViewDir([]string{"--header", "--view-dir", t.TempDir()})
	assertArgs(t, got, []string{"--header"})
}

// TestExpandViewDir_LeavesOtherArgsAlone: no --view-dir means the args reach
// parseListFlags byte for byte, so the one-shot `list` path is untouched.
func TestExpandViewDir_LeavesOtherArgsAlone(t *testing.T) {
	in := []string{"--agents", "--json", "--hide-current"}
	assertArgs(t, expandViewDir(in), in)
}

// TestExpandViewDir_SubstitutesInPlace pins the ordering contract:
// parseListFlags clears the source flags parsed BEFORE a status flag (§9 S3),
// so where the expansion lands changes the result. `--dirs --view-dir <blocked>`
// must mean "blocked agents only", the same as `--dirs --blocked`.
func TestExpandViewDir_SubstitutesInPlace(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "view"), []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := parseListFlags(expandViewDir([]string{"--dirs", "--view-dir", dir}))
	if err != nil {
		t.Fatal(err)
	}
	if f.wantDir || f.wantWS || !f.wantAgent {
		t.Fatalf("got wantWS=%v wantDir=%v wantAgent=%v, want false/false/true", f.wantWS, f.wantDir, f.wantAgent)
	}
}

// TestExpandViewDir_ValuelessFlagStaysUnknown: `--view-dir` with nothing after
// it falls through untouched so parseListFlags rejects it (exit 2), rather
// than being silently swallowed as a no-op.
func TestExpandViewDir_ValuelessFlagStaysUnknown(t *testing.T) {
	if _, err := parseListFlags(expandViewDir([]string{"--view-dir"})); err == nil {
		t.Fatal("want an error for a valueless --view-dir")
	}
}

func assertArgs(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
