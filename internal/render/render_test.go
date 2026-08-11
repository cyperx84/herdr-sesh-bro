package render

import "testing"

// TestStatusColor pins the exact escape for every recognised status plus
// the catch-all, per BEHAVIOUR.md §1.6 / sesh-bro lines 51-61.
func TestStatusColor(t *testing.T) {
	cases := []struct {
		status string
		want   string
	}{
		{"blocked", "\x1b[31m"},
		{"working", "\x1b[33m"},
		{"idle", "\x1b[32m"},
		{"done", "\x1b[34m"},
		{"dir", "\x1b[36m"},
		{"unknown", "\x1b[90m"},
		{"-", "\x1b[90m"},
		{"", "\x1b[90m"},
		{"garbage", "\x1b[90m"},
	}
	for _, c := range cases {
		if got := StatusColor(c.status); got != c.want {
			t.Errorf("StatusColor(%q) = %q, want %q", c.status, got, c.want)
		}
	}
}

// TestFormatRow_ReferenceBytes reproduces every row in BEHAVIOUR.md §4.3
// byte-for-byte, including the ANSI escapes, unstripped. The dir git
// suffixes are pre-spliced into detail via GitSuffix exactly as the caller
// is documented to do, so this also exercises the FormatRow/GitSuffix
// interaction that produces the doubled trailing ESC[0m.
func TestFormatRow_ReferenceBytes(t *testing.T) {
	icons := DefaultIcons()

	cases := []struct {
		name   string
		kind   Kind
		target string
		status string
		label  string
		detail string
		want   string
	}{
		{
			name:   "workspace current no git suffix",
			kind:   KindWorkspace,
			target: "w49",
			status: "working",
			label:  "ORG",
			detail: "2p/1t · current",
			want:   "workspace\tw49\t\x1b[33m◆\x1b[0m ORG \x1b[2m2p/1t · current\x1b[0m\n",
		},
		{
			name:   "workspace unknown status dirty branch",
			kind:   KindWorkspace,
			target: "w3R",
			status: "unknown",
			label:  "DOTFILES",
			detail: "3p/1t" + GitSuffix("main", true),
			want:   "workspace\tw3R\t\x1b[90m◆\x1b[0m DOTFILES \x1b[2m3p/1t \x1b[2m[\x1b[0mmain*\x1b[2m]\x1b[0m\x1b[0m\n",
		},
		{
			name:   "workspace done status clean branch",
			kind:   KindWorkspace,
			target: "w48",
			status: "done",
			label:  "hero-phases",
			detail: "5p/2t" + GitSuffix("feat/stoke-meld", false),
			want:   "workspace\tw48\t\x1b[34m◆\x1b[0m hero-phases \x1b[2m5p/2t \x1b[2m[\x1b[0mfeat/stoke-meld\x1b[2m]\x1b[0m\x1b[0m\n",
		},
		{
			name:   "unnamed agent target is pane id, label is agent kind",
			kind:   KindAgent,
			target: "w49:p1",
			status: "working",
			label:  "claude",
			detail: "claude · Explore Herdr skills flag and documentation",
			want:   "agent\tw49:p1\t\x1b[33m●\x1b[0m claude \x1b[2mclaude · Explore Herdr skills flag and documentation\x1b[0m\n",
		},
		{
			name:   "named agent",
			kind:   KindAgent,
			target: "pagefx",
			status: "working",
			label:  "pagefx",
			detail: "codex · hero-pages",
			want:   "agent\tpagefx\t\x1b[33m●\x1b[0m pagefx \x1b[2mcodex · hero-pages\x1b[0m\n",
		},
		{
			name:   "dir row with spaces in target, status field ignored",
			kind:   KindDir,
			target: "/Users/cyperx/Library/Mobile Documents/iCloud~md~obsidian/Documents/cyperx",
			status: "-",
			label:  "cyperx",
			detail: "/Users/cyperx/Library/Mobile Documents/iCloud~md~obsidian/Documents/cyperx",
			want:   "dir\t/Users/cyperx/Library/Mobile Documents/iCloud~md~obsidian/Documents/cyperx\t\x1b[36m▸\x1b[0m cyperx \x1b[2m/Users/cyperx/Library/Mobile Documents/iCloud~md~obsidian/Documents/cyperx\x1b[0m\n",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := FormatRow(c.kind, c.target, c.status, c.label, c.detail, icons)
			if err != nil {
				t.Fatalf("FormatRow returned error: %v", err)
			}
			if got != c.want {
				t.Errorf("FormatRow() =\n%q\nwant\n%q", got, c.want)
			}
		})
	}
}

// TestFormatRow_DirIgnoresStatus proves a dir row's icon colour never
// depends on its status field — bash hardcodes status_color("dir")
// (sesh-bro line 280) regardless of what's in that column.
func TestFormatRow_DirIgnoresStatus(t *testing.T) {
	icons := DefaultIcons()
	statuses := []string{"-", "blocked", "working", "idle", "done", "unknown", ""}
	var first string
	for i, s := range statuses {
		got, err := FormatRow(KindDir, "/tmp/x", s, "x", "/tmp/x", icons)
		if err != nil {
			t.Fatalf("FormatRow returned error: %v", err)
		}
		if i == 0 {
			first = got
			continue
		}
		if got != first {
			t.Errorf("dir row rendering differs by status: status=%q got %q, want %q (same as status=%q)", s, got, first, statuses[0])
		}
	}
}

// TestFormatRow_CustomIcons checks a configured (non-default) glyph set is
// honoured for list rows — SESH_BRO_ICON_* is the one piece of config that
// reaches row rendering (BEHAVIOUR.md §5).
func TestFormatRow_CustomIcons(t *testing.T) {
	icons := Icons{Workspace: "W", Agent: "A", Dir: "D"}

	got, err := FormatRow(KindWorkspace, "w1", "idle", "l", "d", icons)
	if err != nil {
		t.Fatalf("FormatRow returned error: %v", err)
	}
	want := "workspace\tw1\t\x1b[32mW\x1b[0m l \x1b[2md\x1b[0m\n"
	if got != want {
		t.Errorf("workspace icon: got %q, want %q", got, want)
	}

	got, err = FormatRow(KindAgent, "a1", "blocked", "l", "d", icons)
	if err != nil {
		t.Fatalf("FormatRow returned error: %v", err)
	}
	want = "agent\ta1\t\x1b[31mA\x1b[0m l \x1b[2md\x1b[0m\n"
	if got != want {
		t.Errorf("agent icon: got %q, want %q", got, want)
	}

	got, err = FormatRow(KindDir, "/d", "-", "l", "d", icons)
	if err != nil {
		t.Fatalf("FormatRow returned error: %v", err)
	}
	want = "dir\t/d\t\x1b[36mD\x1b[0m l \x1b[2md\x1b[0m\n"
	if got != want {
		t.Errorf("dir icon: got %q, want %q", got, want)
	}
}

// TestFormatRow_UnknownKind rejects a row type outside the three bash ever
// emits, rather than silently reproducing the stateful stale-icon quirk of
// bash's loop variable (see FormatRow's doc comment).
func TestFormatRow_UnknownKind(t *testing.T) {
	if _, err := FormatRow(Kind("bogus"), "t", "s", "l", "d", DefaultIcons()); err == nil {
		t.Error("FormatRow with an unknown kind should return an error, got nil")
	}
}

// TestGitSuffix_Bytes pins the exact bytes of the git-branch annotation,
// separately from FormatRow, so a regression here is diagnosable without
// wading through a whole row's escapes (sesh-bro line 258, BEHAVIOUR.md
// §2.2.8/§4.2).
func TestGitSuffix_Bytes(t *testing.T) {
	cases := []struct {
		branch string
		dirty  bool
		want   string
	}{
		{"main", true, " \x1b[2m[\x1b[0mmain*\x1b[2m]\x1b[0m"},
		{"main", false, " \x1b[2m[\x1b[0mmain\x1b[2m]\x1b[0m"},
		{"feat/stoke-meld", false, " \x1b[2m[\x1b[0mfeat/stoke-meld\x1b[2m]\x1b[0m"},
		{"HEAD (no branch)", false, " \x1b[2m[\x1b[0mHEAD (no branch)\x1b[2m]\x1b[0m"},
	}
	for _, c := range cases {
		if got := GitSuffix(c.branch, c.dirty); got != c.want {
			t.Errorf("GitSuffix(%q, %v) = %q, want %q", c.branch, c.dirty, got, c.want)
		}
	}
}

// TestPreviewWorkspace pins §2.8's "not found" and header formats
// (sesh-bro lines 447, 450-453).
func TestPreviewWorkspace(t *testing.T) {
	got := PreviewWorkspaceNotFound("w49")
	want := "\x1b[90m◆ w49\x1b[0m  \x1b[2m(not found)\x1b[0m\n"
	if got != want {
		t.Errorf("PreviewWorkspaceNotFound() = %q, want %q", got, want)
	}

	got = PreviewWorkspaceHeader("working", "ORG", "w49")
	want = "\x1b[33m◆ ORG\x1b[0m  \x1b[2m(w49)\x1b[0m\n\n"
	if got != want {
		t.Errorf("PreviewWorkspaceHeader() = %q, want %q", got, want)
	}
}

// TestPreviewAgent pins §2.8's agent formats (sesh-bro lines 463, 466-471).
func TestPreviewAgent(t *testing.T) {
	got := PreviewAgentNotFound("pagefx")
	want := "\x1b[90m● pagefx\x1b[0m  \x1b[2m(not found)\x1b[0m\n"
	if got != want {
		t.Errorf("PreviewAgentNotFound() = %q, want %q", got, want)
	}

	got = PreviewAgentHeader("working", "claude", "/Users/cyperx/proj")
	want = "\x1b[33m● claude\x1b[0m  \x1b[2mworking · /Users/cyperx/proj\x1b[0m\n\n"
	if got != want {
		t.Errorf("PreviewAgentHeader() = %q, want %q", got, want)
	}
}

// TestPreviewDir pins §2.8's dir preview formats (sesh-bro lines 475, 477,
// 488), including the no-space-before-reset quirk in the header.
func TestPreviewDir(t *testing.T) {
	got := PreviewDirHeader("/Users/cyperx/proj")
	want := "\x1b[36m▸ /Users/cyperx/proj\x1b[0m\n\n"
	if got != want {
		t.Errorf("PreviewDirHeader() = %q, want %q", got, want)
	}

	got = PreviewDirNotFound()
	want = "\x1b[2m(directory not found)\x1b[0m\n"
	if got != want {
		t.Errorf("PreviewDirNotFound() = %q, want %q", got, want)
	}

	got = PreviewDirReadmeSeparator("README.md")
	want = "\n\x1b[2m── README.md ──\x1b[0m\n\n"
	if got != want {
		t.Errorf("PreviewDirReadmeSeparator() = %q, want %q", got, want)
	}
}

// TestPreviewUnknown pins the fallback line for an unrecognised preview
// type (sesh-bro line 496).
func TestPreviewUnknown(t *testing.T) {
	if got, want := PreviewUnknown(), "no preview\n"; got != want {
		t.Errorf("PreviewUnknown() = %q, want %q", got, want)
	}
}
