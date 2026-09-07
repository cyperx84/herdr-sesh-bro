package picker

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// TestBuildArgs_Default pins the exact argv for the common case: preview
// on, default width, no hide-current, no alias, default (unset) keys —
// every element in bash's exact order for the six ported binds
// (sesh-bro:536-564, BEHAVIOUR.md §3.1), plus Feature A's new close bind
// (alt-x — deliberately not an fzf abort key; see DefaultKeyBindings) and
// the header's “· alt-x close” clause
// (docs/COMPETITIVE-DEMAND.md #1).
func TestBuildArgs_Default(t *testing.T) {
	got := BuildArgs(Options{
		SelfPath:       "/opt/sesh-bro/sesh-bro",
		PreviewEnabled: true,
		PreviewWidth:   "60%",
	})
	want := []string{
		"--ansi",
		`--delimiter=\t`,
		"--with-nth=3..",
		"--layout=reverse",
		"--tiebreak=index",
		"--query=",
		"--prompt=sesh> ",
		"--header=enter connect · ^w workspaces · ^e agents · ^b blocked · ^x dirs · ^o all · alt-x close · ^/ create",
		"--preview='/opt/sesh-bro/sesh-bro' preview {1} {2}",
		"--preview-window=right,60%,border-left",
		"--bind=alt-x:execute-silent('/opt/sesh-bro/sesh-bro' close {1} {2})+reload('/opt/sesh-bro/sesh-bro' list --header )",
		"--bind=ctrl-w:reload('/opt/sesh-bro/sesh-bro' list --workspaces --header )",
		"--bind=ctrl-e:reload('/opt/sesh-bro/sesh-bro' list --agents --header )",
		"--bind=ctrl-b:reload('/opt/sesh-bro/sesh-bro' list --blocked --header )",
		"--bind=ctrl-x:reload('/opt/sesh-bro/sesh-bro' list --dirs --header )",
		"--bind=ctrl-o:reload('/opt/sesh-bro/sesh-bro' list --header )",
		"--bind=ctrl-/:execute-silent('/opt/sesh-bro/sesh-bro' create)+reload('/opt/sesh-bro/sesh-bro' list --header )",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArgs() =\n%#v\nwant\n%#v", got, want)
	}
}

// TestBuildArgs_HideCurrent proves --hide-current reaches only the reload
// binds, and that it fills the same slot the trailing space occupied when
// empty — no other argv element changes (BEHAVIOUR.md §3.3).
func TestBuildArgs_HideCurrent(t *testing.T) {
	got := BuildArgs(Options{SelfPath: "/bin/sesh-bro", HideCurrent: true})
	wantBinds := []string{
		"--bind=ctrl-w:reload('/bin/sesh-bro' list --workspaces --header --hide-current)",
		"--bind=ctrl-e:reload('/bin/sesh-bro' list --agents --header --hide-current)",
		"--bind=ctrl-b:reload('/bin/sesh-bro' list --blocked --header --hide-current)",
		"--bind=ctrl-x:reload('/bin/sesh-bro' list --dirs --header --hide-current)",
		"--bind=ctrl-o:reload('/bin/sesh-bro' list --header --hide-current)",
		"--bind=alt-x:execute-silent('/bin/sesh-bro' close {1} {2})+reload('/bin/sesh-bro' list --header --hide-current)",
		"--bind=ctrl-/:execute-silent('/bin/sesh-bro' create)+reload('/bin/sesh-bro' list --header --hide-current)",
	}
	for _, want := range wantBinds {
		found := false
		for _, g := range got {
			if g == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing bind %q in argv %v", want, got)
		}
	}
}

// TestBuildArgs_PreviewDisabled proves --preview is entirely absent when
// PreviewEnabled is false, but --preview-window is still emitted
// unconditionally (BEHAVIOUR.md §3.1: "passed unconditionally... harmless").
func TestBuildArgs_PreviewDisabled(t *testing.T) {
	got := BuildArgs(Options{SelfPath: "/bin/sesh-bro", PreviewEnabled: false, PreviewWidth: "40%"})
	for _, g := range got {
		if strings.HasPrefix(g, "--preview=") {
			t.Errorf("--preview present despite PreviewEnabled=false: %q", g)
		}
	}
	wantWindow := "--preview-window=right,40%,border-left"
	found := false
	for _, g := range got {
		if g == wantWindow {
			found = true
		}
	}
	if !found {
		t.Errorf("missing %q in argv %v", wantWindow, got)
	}
}

// TestSanitizeWidth pins sesh-bro:543-544's regex guard: numeric, with an
// optional trailing '%', or fall back to "60%".
func TestSanitizeWidth(t *testing.T) {
	cases := map[string]string{
		"60":   "60",
		"45%":  "45%",
		"0":    "0",
		"half": "60%",
		"60px": "60%",
		"-10%": "60%",
		"":     "60%",
		"60%%": "60%",
		"60 %": "60%",
	}
	for in, want := range cases {
		if got := sanitizeWidth(in); got != want {
			t.Errorf("sanitizeWidth(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestQuoteSingle covers a path containing both a space and an embedded
// single quote — the case bash's SELF_Q substitution (sesh-bro:30) exists
// to handle safely.
func TestQuoteSingle(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/opt/sesh-bro/sesh-bro", "'/opt/sesh-bro/sesh-bro'"},
		{"", "''"},
		{"it's/a path/sesh-bro", `'it'\''s/a path/sesh-bro'`},
		{"a'b'c", `'a'\''b'\''c'`},
	}
	for _, c := range cases {
		if got := quoteSingle(c.in); got != c.want {
			t.Errorf("quoteSingle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestBuildArgs_SelfPathWithSpaceAndQuote is an end-to-end argv check for
// the same nasty path, confirming quoting survives into the actual bind
// strings.
func TestBuildArgs_SelfPathWithSpaceAndQuote(t *testing.T) {
	got := BuildArgs(Options{SelfPath: "/opt/it's a dir/sesh-bro", PreviewEnabled: true})
	wantPreview := `--preview='/opt/it'\''s a dir/sesh-bro' preview {1} {2}`
	found := false
	for _, g := range got {
		if g == wantPreview {
			found = true
		}
	}
	if !found {
		t.Errorf("missing %q in argv %v", wantPreview, got)
	}
}

// TestSanitizeKey covers Feature B's malformed-value guard
// (docs/COMPETITIVE-DEMAND.md #2): a well-formed override is used verbatim,
// and anything that would corrupt a --bind=KEY:ACTION(...) argv element —
// empty, an embedded colon (redefines ACTION), a comma (chains a second key
// spec), parens, whitespace — degrades to the supplied default instead of
// reaching BuildArgs at all.
func TestSanitizeKey(t *testing.T) {
	const def = "ctrl-w"
	cases := map[string]string{
		"ctrl-a":       "ctrl-a",
		"alt-w":        "alt-w",
		"f5":           "f5",
		"q":            "q",
		"ctrl-/":       "ctrl-/",
		"double-click": "double-click",
		"":             def,
		"ctrl-a:foo":   def, // colon would splice a second ACTION onto this bind
		"ctrl-a,q":     def, // comma chains an extra, unintended key spec
		"ctrl-a)pwn(":  def, // parens would prematurely close/reopen the action list
		"ctrl a":       def, // whitespace has no meaning in a key spec
		"-ctrl-a":      def, // must start alnum
	}
	for in, want := range cases {
		if got := sanitizeKey(in, def); got != want {
			t.Errorf("sanitizeKey(%q, %q) = %q, want %q", in, def, got, want)
		}
	}
}

// TestKeyBindings_Resolved proves every field falls back independently —
// one malformed override does not clobber its siblings — and that a
// well-formed override is threaded straight through untouched.
func TestKeyBindings_Resolved(t *testing.T) {
	got := KeyBindings{
		Workspaces: "alt-w",  // valid override
		Agents:     "bad:ag", // malformed -> default
		Close:      "ctrl-d", // valid override
		// Blocked, Dirs, All, Create left zero-value -> default
	}.resolved()
	want := KeyBindings{
		Workspaces: "alt-w",
		Agents:     DefaultKeyBindings.Agents,
		Blocked:    DefaultKeyBindings.Blocked,
		Dirs:       DefaultKeyBindings.Dirs,
		All:        DefaultKeyBindings.All,
		Create:     DefaultKeyBindings.Create,
		Close:      "ctrl-d",
	}
	if got != want {
		t.Errorf("KeyBindings.resolved() = %+v, want %+v", got, want)
	}
}

// TestKeyLabel covers the --header shorthand: single-character ctrl- binds
// shorten to bash's caret notation, everything else is shown verbatim.
func TestKeyLabel(t *testing.T) {
	cases := map[string]string{
		"ctrl-w":       "^w",
		"ctrl-/":       "^/",
		"ctrl-q":       "^q", // still a valid caret form; no longer the close default
		"alt-x":        "alt-x",
		"alt-w":        "alt-w",
		"f5":           "f5",
		"q":            "q",
		"double-click": "double-click",
		"ctrl-space":   "ctrl-space", // "space" is 5 chars, not one — shown verbatim
	}
	for in, want := range cases {
		if got := keyLabel(in); got != want {
			t.Errorf("keyLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestBuildArgs_KeyOverrides proves SESH_BRO_KEY_* overrides (via
// Options.Keys) reach both the --bind flags and the --header hint text
// together — the header must never name a key that isn't actually bound
// (keyLabel's doc comment) — while a malformed override in the mix degrades
// only that one action, both in the bind and in the header.
func TestBuildArgs_KeyOverrides(t *testing.T) {
	got := BuildArgs(Options{
		SelfPath: "/bin/sesh-bro",
		Keys: KeyBindings{
			Close:  "ctrl-d",  // valid override
			Create: "bad:key", // malformed -> falls back to ctrl-/
		},
	})
	wantHeader := "--header=enter connect · ^w workspaces · ^e agents · ^b blocked · ^x dirs · ^o all · ^d close · ^/ create"
	wantCloseBind := "--bind=ctrl-d:execute-silent('/bin/sesh-bro' close {1} {2})+reload('/bin/sesh-bro' list --header )"
	wantCreateBind := "--bind=ctrl-/:execute-silent('/bin/sesh-bro' create)+reload('/bin/sesh-bro' list --header )"
	for _, want := range []string{wantHeader, wantCloseBind, wantCreateBind} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing %q in argv %v", want, got)
		}
	}
}

// TestBashSplitColon pins the four bash-measured cases from the package
// doc comment, plus the "single delimiter" edge case — this is NOT the
// same as strings.Split, and the mismatches matter for aliasQuery's
// exactly-one-pair check.
func TestBashSplitColon(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a=b", []string{"a=b"}},
		{"a=b:", []string{"a=b"}},
		{"a=b::c=d", []string{"a=b", "", "c=d"}},
		{"a=b::", []string{"a=b", ""}},
		{":a=b", []string{"", "a=b"}},
		{":", []string{""}},
		{"a=b:c=d", []string{"a=b", "c=d"}},
	}
	for _, c := range cases {
		got := bashSplitColon(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("bashSplitColon(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}
}

// TestAliasQuery covers empty, single-pair (with and without "="),
// multi-pair, and trailing-colon aliases (BEHAVIOUR.md §2.10, §5).
func TestAliasQuery(t *testing.T) {
	cases := map[string]string{
		"":                      "",
		"dev=DOTFILES":          "dev",
		"dev":                   "dev",
		"dev=DOTFILES:web=Site": "",
		"dev=DOTFILES:":         "dev",
	}
	for in, want := range cases {
		if got := aliasQuery(in); got != want {
			t.Errorf("aliasQuery(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestBuildArgs_AliasQuery confirms the derived alias prefills --query in
// the argv.
func TestBuildArgs_AliasQuery(t *testing.T) {
	got := BuildArgs(Options{SelfPath: "/bin/sesh-bro", Aliases: "dev=DOTFILES"})
	want := "--query=dev"
	found := false
	for _, g := range got {
		if g == want {
			found = true
		}
	}
	if !found {
		t.Errorf("missing %q in argv %v", want, got)
	}
}

// TestParseSelection covers a normal row, a target containing a space, and
// the empty-string "no selection" case (BEHAVIOUR.md §3.4).
func TestParseSelection(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		want   Selection
		wantOK bool
	}{
		{
			name:   "normal row",
			raw:    "workspace\tw49\t\x1b[33m◆\x1b[0m ORG \x1b[2m2p/1t\x1b[0m",
			want:   Selection{Kind: "workspace", Target: "w49"},
			wantOK: true,
		},
		{
			name:   "dir target with spaces",
			raw:    "dir\t/Users/cyperx/Mobile Documents/foo\t\x1b[36m▸\x1b[0m foo \x1b[2m/Users/cyperx/Mobile Documents/foo\x1b[0m",
			want:   Selection{Kind: "dir", Target: "/Users/cyperx/Mobile Documents/foo"},
			wantOK: true,
		},
		{
			name:   "trailing newline stripped like $()",
			raw:    "agent\tclaude\tsome display\n\n",
			want:   Selection{Kind: "agent", Target: "claude"},
			wantOK: true,
		},
		{
			name:   "empty selection",
			raw:    "",
			want:   Selection{},
			wantOK: false,
		},
		{
			name:   "only newlines is still empty",
			raw:    "\n\n",
			want:   Selection{},
			wantOK: false,
		},
		{
			name:   "no delimiter at all — cut passes the whole line through for every field",
			raw:    `{"type":"workspace"}`,
			want:   Selection{Kind: `{"type":"workspace"}`, Target: `{"type":"workspace"}`},
			wantOK: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ParseSelection(c.raw)
			if ok != c.wantOK {
				t.Fatalf("ParseSelection(%q) ok = %v, want %v", c.raw, ok, c.wantOK)
			}
			if got != c.want {
				t.Errorf("ParseSelection(%q) = %#v, want %#v", c.raw, got, c.want)
			}
		})
	}
}

// TestCutField pins the cut(1) semantics ParseSelection relies on: a field
// index past the last present field is "", but a line with NO delimiter is
// passed through unmodified for any requested field.
func TestCutField(t *testing.T) {
	cases := []struct {
		line string
		n    int
		want string
	}{
		{"a\tb\tc", 1, "a"},
		{"a\tb\tc", 2, "b"},
		{"a\tb\tc", 3, "c"},
		{"a\tb", 3, ""},
		{"abc", 1, "abc"},
		{"abc", 2, "abc"},
		{"", 1, ""},
	}
	for _, c := range cases {
		if got := cutField(c.line, c.n); got != c.want {
			t.Errorf("cutField(%q, %d) = %q, want %q", c.line, c.n, got, c.want)
		}
	}
}

// TestRun_FzfNotFound exercises Run's dependency guard without needing fzf
// installed: PATH is emptied so exec.Command("fzf", ...) resolves to
// exec.ErrNotFound, matching bash's `command -v fzf` check
// (sesh-bro:520, BEHAVIOUR.md Appendix B).
func TestRun_FzfNotFound(t *testing.T) {
	t.Setenv("PATH", "")

	var stderr bytes.Buffer
	connectCalled := false
	err := Run(Options{
		SelfPath: "/bin/sesh-bro",
		Rows:     strings.NewReader(""),
		Stderr:   &stderr,
	}, func(kind, target string) error {
		connectCalled = true
		return nil
	})

	if !errors.Is(err, ErrFzfNotFound) {
		t.Fatalf("Run() error = %v, want ErrFzfNotFound", err)
	}
	if connectCalled {
		t.Error("connect was called despite fzf being unavailable")
	}
	if !strings.Contains(stderr.String(), "sesh-bro: fzf is required") {
		t.Errorf("stderr = %q, want it to contain the fzf-required message", stderr.String())
	}
}

// fzf documents four default abort keys — ctrl-c, ctrl-g, ctrl-q, esc. Binding
// a silent, irreversible workspace close to one of them means the keystroke fzf
// itself trains users to press for "get me out of here" destroys a workspace
// and every agent in it, with execute-silent swallowing any message.
func TestCloseIsNotBoundToAnFzfAbortKey(t *testing.T) {
	for _, abort := range []string{"ctrl-c", "ctrl-g", "ctrl-q", "esc"} {
		if DefaultKeyBindings.Close == abort {
			t.Errorf("close defaults to %q, which fzf uses to abort — pressing it to dismiss the picker would delete a workspace", abort)
		}
	}
}

// fzf applies last-bind-wins. If the close bind were emitted after the
// navigation binds, a collision — SESH_BRO_KEY_CLOSE=ctrl-w, a plausible
// preference or a copy-paste slip — would silently make a navigation key
// destructive while the header still advertised both.
func TestCloseBindIsEmittedBeforeNavigationBinds(t *testing.T) {
	args := BuildArgs(Options{SelfPath: "/bin/sesh-bro"})

	closeAt, navAt := -1, -1
	for i, a := range args {
		if strings.Contains(a, " close {1} {2}") {
			closeAt = i
		}
		if navAt == -1 && strings.Contains(a, "list --workspaces") {
			navAt = i
		}
	}
	if closeAt == -1 || navAt == -1 {
		t.Fatalf("expected both a close bind and a navigation bind: close=%d nav=%d", closeAt, navAt)
	}
	if closeAt > navAt {
		t.Errorf("close bind at %d comes after navigation bind at %d — a key collision would make navigation destructive", closeAt, navAt)
	}
}

// A well-shaped value that fzf rejects makes fzf exit 2, which Run swallows —
// the picker flashes and vanishes with exit 0 and no diagnostic. One typo in
// one env var then makes the picker unopenable and unexplainable.
func TestValidKeyMatchesFzfGrammarNotJustShape(t *testing.T) {
	valid := []string{"ctrl-w", "ctrl-/", "alt-x", "f1", "f12", "tab", "shift-tab", "enter", "double-click", "q", "?"}
	for _, k := range valid {
		if !validKey(k) {
			t.Errorf("validKey(%q) = false, but fzf accepts it", k)
		}
	}
	// Every one of these passes a naive [A-Za-z0-9][A-Za-z0-9_/-]* shape check
	// and every one makes fzf exit 2 with "unsupported key".
	invalid := []string{"shift-a", "zzz", "f25", "ctrl-ww", "alt-", "q_x", "a/b", ""}
	for _, k := range invalid {
		if validKey(k) {
			t.Errorf("validKey(%q) = true, but fzf rejects it and will not start", k)
		}
	}
}

// BuildArgs emits --header-lines=1 whenever HeaderLines is set, which tells fzf
// to treat the first line of EVERY stream it loads as chrome rather than a
// candidate. So every reload has to produce that header line — and three of
// them did not, which silently promoted the first real row into an
// unselectable header (BEHAVIOUR.md §10.6). This states the invariant as a
// test so the next bind added cannot quietly reintroduce it.
func TestEveryReloadProducesAHeaderLine(t *testing.T) {
	for _, opts := range []Options{
		{SelfPath: "/bin/sesh-bro", HeaderLines: true},
		{SelfPath: "/bin/sesh-bro", HeaderLines: true, HideCurrent: true},
		{
			SelfPath: "/bin/sesh-bro", HeaderLines: true, RowsDir: "/run/p1",
			ListenSocket: "/run/p1/fzf.sock",
			Fzf:          Features{Version: "0.74.3", Listen: true, TrackID: true, Footer: true},
		},
	} {
		for _, arg := range BuildArgs(opts) {
			if !strings.HasPrefix(arg, "--bind=") || !strings.Contains(arg, "reload(") {
				continue
			}
			// A reload is safe in exactly three forms: it cats a pre-rendered
			// view file, it goes through `rows` (which cats one), or it asks
			// `list` for a header explicitly. Every view file begins with a
			// header row because renderRows writes one into each.
			viaTSV := strings.Contains(arg, ".tsv'")
			viaRows := strings.Contains(arg, " rows --dir ")
			viaHeaderFlag := strings.Contains(arg, "--header")
			if !viaTSV && !viaRows && !viaHeaderFlag {
				t.Errorf("reload bind produces no header line: %q (RowsDir=%q)", arg, opts.RowsDir)
			}
		}
	}
}

// With a RowsDir every reload goes through `rows`, which reads the view marker.
// The previous form re-executed `list` with no source flags, so pressing close
// or create threw the user back to the all-sources view no matter which filter
// was active — and left the marker untouched, so the next live push switched
// the list back again.
func TestCloseAndCreateReloadPreserveTheView(t *testing.T) {
	got := BuildArgs(Options{
		SelfPath: "/bin/sesh-bro", RowsDir: "/run/p1",
		Fzf: Features{Version: "0.74.3", Listen: true},
	})
	var closeBind, createBind string
	for _, a := range got {
		switch {
		case strings.HasPrefix(a, "--bind=alt-x:"):
			closeBind = a
		case strings.HasPrefix(a, "--bind=ctrl-/:"):
			createBind = a
		}
	}
	for name, bind := range map[string]string{"close": closeBind, "create": createBind} {
		if bind == "" {
			t.Fatalf("%s bind missing from %v", name, got)
		}
		if !strings.Contains(bind, "rows --dir '/run/p1'") {
			t.Errorf("%s bind does not reload the current view: %q", name, bind)
		}
		if strings.Contains(bind, "reload('/bin/sesh-bro' list") {
			t.Errorf("%s bind still re-execs list, discarding the active filter: %q", name, bind)
		}
	}
}

// The live argv had no golden coverage at all, which is why the header defect
// survived. This pins every flag the live picker depends on.
func TestBuildArgs_Live(t *testing.T) {
	got := BuildArgs(Options{
		SelfPath: "/bin/sesh-bro", PreviewEnabled: true, PreviewWidth: "60%",
		HeaderLines: true, RowsDir: "/run/p1", ListenSocket: "/run/p1/fzf.sock",
		Fzf: Features{Version: "0.74.3", Listen: true, TrackID: true, Footer: true},
	})
	joined := strings.Join(got, " ")
	for _, want := range []string{
		"--header-lines=1",
		"--listen=/run/p1/fzf.sock",
		"--track",
		"--id-nth=2",
		"--footer=enter connect",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("live argv missing %q: %v", want, got)
		}
	}
	// With a footer the hints must NOT also occupy the header, which is where
	// the live counts row goes.
	for _, a := range got {
		if strings.HasPrefix(a, "--header=") {
			t.Errorf("hints stayed in the header while a footer was available: %q", a)
		}
	}
}

// An fzf too old for --listen must produce the pre-0.4.0 picker exactly: no
// listen socket, no tracking, hints back in the header. This is the
// degradation path the CHANGELOG promises, and it was broken.
func TestBuildArgs_OldFzfDegradesCleanly(t *testing.T) {
	got := BuildArgs(Options{SelfPath: "/bin/sesh-bro", HeaderLines: true, Fzf: Features{}})
	joined := strings.Join(got, " ")
	for _, unwanted := range []string{"--listen", "--track", "--id-nth", "--footer="} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("old fzf argv contains %q: %v", unwanted, got)
		}
	}
	if !strings.Contains(joined, "--header=enter connect") {
		t.Errorf("hints did not fall back to the header: %v", got)
	}
}
