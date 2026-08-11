package picker

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// TestBuildArgs_Default pins the exact argv for the common case: preview
// on, default width, no hide-current, no alias — every element in bash's
// exact order (sesh-bro:536-564, BEHAVIOUR.md §3.1).
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
		"--header=enter connect · ^w workspaces · ^e agents · ^b blocked · ^x dirs · ^o all · ^/ create",
		"--preview='/opt/sesh-bro/sesh-bro' preview {1} {2}",
		"--preview-window=right,60%,border-left",
		"--bind=ctrl-w:reload('/opt/sesh-bro/sesh-bro' list --workspaces )",
		"--bind=ctrl-e:reload('/opt/sesh-bro/sesh-bro' list --agents )",
		"--bind=ctrl-b:reload('/opt/sesh-bro/sesh-bro' list --blocked )",
		"--bind=ctrl-x:reload('/opt/sesh-bro/sesh-bro' list --dirs )",
		"--bind=ctrl-o:reload('/opt/sesh-bro/sesh-bro' list )",
		"--bind=ctrl-/:execute-silent('/opt/sesh-bro/sesh-bro' create)+reload('/opt/sesh-bro/sesh-bro' list )",
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
		"--bind=ctrl-w:reload('/bin/sesh-bro' list --workspaces --hide-current)",
		"--bind=ctrl-e:reload('/bin/sesh-bro' list --agents --hide-current)",
		"--bind=ctrl-b:reload('/bin/sesh-bro' list --blocked --hide-current)",
		"--bind=ctrl-x:reload('/bin/sesh-bro' list --dirs --hide-current)",
		"--bind=ctrl-o:reload('/bin/sesh-bro' list --hide-current)",
		"--bind=ctrl-/:execute-silent('/bin/sesh-bro' create)+reload('/bin/sesh-bro' list --hide-current)",
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
