package config

import (
	"errors"
	"testing"

	"github.com/cyperx84/herdr-sesh-bro/internal/picker"
)

// envMap builds a getenv func over a fixed map, for tests that don't want
// to touch process-wide environment state.
func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// TestLoad_Defaults asserts every one of the twelve SESH_BRO_* variables'
// hardcoded defaults (sesh-bro:35-46), with a completely empty environment
// — the "unset" case.
func TestLoad_Defaults(t *testing.T) {
	c := Load(envMap(nil))
	want := Config{
		PreviewWidth:  "60%",
		Aliases:       "",
		Blacklist:     "",
		DefaultFilter: "all",
		SortOrder:     "",
		IconWorkspace: "◆",
		IconAgent:     "●",
		IconDir:       "▸",
		KeyWorkspaces: "ctrl-w",
		KeyAgents:     "ctrl-e",
		KeyBlocked:    "ctrl-b",
		KeyDirs:       "ctrl-x",
		KeyAll:        "ctrl-o",
		KeyCreate:     "ctrl-/",
		KeyClose:      "alt-x",
	}
	got := Config{
		PreviewWidth:  c.PreviewWidth,
		Aliases:       c.Aliases,
		Blacklist:     c.Blacklist,
		DefaultFilter: c.DefaultFilter,
		SortOrder:     c.SortOrder,
		IconWorkspace: c.IconWorkspace,
		IconAgent:     c.IconAgent,
		IconDir:       c.IconDir,
		KeyWorkspaces: c.KeyWorkspaces,
		KeyAgents:     c.KeyAgents,
		KeyBlocked:    c.KeyBlocked,
		KeyDirs:       c.KeyDirs,
		KeyAll:        c.KeyAll,
		KeyCreate:     c.KeyCreate,
		KeyClose:      c.KeyClose,
	}
	if got != want {
		t.Fatalf("Load(empty env) = %+v, want %+v", got, want)
	}

	if pe, err := c.PreviewEnabled(); err != nil || !pe {
		t.Fatalf("PreviewEnabled default: got (%v, %v), want (true, nil)", pe, err)
	}
	if hc, err := c.HideCurrent(); err != nil || hc {
		t.Fatalf("HideCurrent default: got (%v, %v), want (false, nil)", hc, err)
	}
	if ds, err := c.DirSources(); err != nil || !ds {
		t.Fatalf("DirSources default: got (%v, %v), want (true, nil)", ds, err)
	}
}

// TestLoad_EmptyValueFallsBackToDefault asserts bash's `${VAR:-default}`
// nullity test: a variable present in the environment but set to the exact
// empty string is indistinguishable from unset (BEHAVIOUR.md §1.5).
func TestLoad_EmptyValueFallsBackToDefault(t *testing.T) {
	c := Load(envMap(map[string]string{
		"SESH_BRO_PREVIEW_WIDTH": "",
		"SESH_BRO_CACHE_TTL":     "",
	}))
	if c.PreviewWidth != "60%" {
		t.Errorf("PreviewWidth = %q, want default 60%%", c.PreviewWidth)
	}
}

// TestLoad_WhitespaceOnlyIsNotReplaced asserts the other half of bash's
// `:-`: a whitespace-only value is non-empty, so it is used VERBATIM, not
// treated as blank (BEHAVIOUR.md §1.5 — "an empty value falls back to the
// default, a whitespace-only value does not").
func TestLoad_WhitespaceOnlyIsNotReplaced(t *testing.T) {
	c := Load(envMap(map[string]string{
		"SESH_BRO_PREVIEW_WIDTH":  "   ",
		"SESH_BRO_DEFAULT_FILTER": " ",
	}))
	if c.PreviewWidth != "   " {
		t.Errorf("PreviewWidth = %q, want the literal whitespace value preserved", c.PreviewWidth)
	}
	if c.DefaultFilter != " " {
		t.Errorf("DefaultFilter = %q, want the literal whitespace value preserved", c.DefaultFilter)
	}
}

// TestLoad_NonEmptyOverridesDefault is the ordinary path: a real value
// wins over the default for every field.
func TestLoad_NonEmptyOverridesDefault(t *testing.T) {
	env := map[string]string{
		"SESH_BRO_PREVIEW_WIDTH":   "40%",
		"SESH_BRO_ALIASES":         "dev=Development",
		"SESH_BRO_BLACKLIST":       "/tmp/*:/var/*",
		"SESH_BRO_CACHE_TTL":       "10",
		"SESH_BRO_DEFAULT_FILTER":  "agents",
		"SESH_BRO_SORT_ORDER":      "agents,dirs",
		"SESH_BRO_ICON_WORKSPACE":  "W",
		"SESH_BRO_ICON_AGENT":      "A",
		"SESH_BRO_ICON_DIR":        "D",
		"SESH_BRO_PREVIEW_ENABLED": "0",
		"SESH_BRO_HIDE_CURRENT":    "1",
		"SESH_BRO_DIR_SOURCES":     "0",
		"SESH_BRO_KEY_WORKSPACES":  "alt-w",
		"SESH_BRO_KEY_AGENTS":      "alt-e",
		"SESH_BRO_KEY_BLOCKED":     "alt-b",
		"SESH_BRO_KEY_DIRS":        "alt-x",
		"SESH_BRO_KEY_ALL":         "alt-o",
		"SESH_BRO_KEY_CREATE":      "alt-c",
		"SESH_BRO_KEY_CLOSE":       "alt-q",
	}
	c := Load(envMap(env))
	if c.PreviewWidth != "40%" || c.Aliases != "dev=Development" || c.Blacklist != "/tmp/*:/var/*" ||
		c.DefaultFilter != "agents" || c.SortOrder != "agents,dirs" ||
		c.IconWorkspace != "W" || c.IconAgent != "A" || c.IconDir != "D" {
		t.Fatalf("Load did not thread every overridden value through: %+v", c)
	}
	if c.KeyWorkspaces != "alt-w" || c.KeyAgents != "alt-e" || c.KeyBlocked != "alt-b" ||
		c.KeyDirs != "alt-x" || c.KeyAll != "alt-o" || c.KeyCreate != "alt-c" || c.KeyClose != "alt-q" {
		t.Fatalf("Load did not thread every overridden SESH_BRO_KEY_* value through: %+v", c)
	}
	if pe, err := c.PreviewEnabled(); err != nil || pe {
		t.Errorf("PreviewEnabled = (%v, %v), want (false, nil)", pe, err)
	}
	if hc, err := c.HideCurrent(); err != nil || !hc {
		t.Errorf("HideCurrent = (%v, %v), want (true, nil)", hc, err)
	}
	if ds, err := c.DirSources(); err != nil || ds {
		t.Errorf("DirSources = (%v, %v), want (false, nil)", ds, err)
	}
}

// TestLoadEnv_ReadsProcessEnvironment asserts LoadEnv is actually wired to
// os.Getenv, not a copy of Load that never gets called with it.
func TestLoadEnv_ReadsProcessEnvironment(t *testing.T) {
	t.Setenv("SESH_BRO_ICON_DIR", "→")
	c := LoadEnv()
	if c.IconDir != "→" {
		t.Fatalf("LoadEnv().IconDir = %q, want %q (LoadEnv must read the real process environment)", c.IconDir, "→")
	}
}

// TestParseBoolFlag_ValidIntegers covers bash's `[[ $val -eq 1 ]]` for
// every value that IS valid shell arithmetic in this port's accepted
// subset (base-10 integers): exactly 1 is true, every other integer
// (including negative and zero) is false, no error.
func TestParseBoolFlag_ValidIntegers(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"1", true},
		{"0", false},
		{"2", false},
		{"-1", false},
		// "01" parses as decimal 1 via strconv.ParseInt base 10 (no octal
		// reinterpretation at an explicit base), so it IS -eq 1 in this
		// port's accepted subset.
		{"01", true},
	}
	for _, tc := range cases {
		got, err := ParseBoolFlag("SESH_BRO_TEST", tc.val)
		if err != nil {
			t.Errorf("ParseBoolFlag(%q) unexpected error: %v", tc.val, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseBoolFlag(%q) = %v, want %v", tc.val, got, tc.want)
		}
	}
}

// TestParseBoolFlag_MalformedDegradesToError is S1: bash's `-eq 1` is
// arithmetic and dies under `set -u`/an arithmetic syntax error on
// anything that isn't a base-10 integer. This port's contract (documented
// on ParseBoolFlag) is that the same class of input degrades to a non-nil
// *BoolVarError — never a panic, never a silent default.
func TestParseBoolFlag_MalformedDegradesToError(t *testing.T) {
	for _, val := range []string{"true", "false", "yes", "no", "0x1", "1.0", "", " ", "one"} {
		got, err := ParseBoolFlag("SESH_BRO_TEST", val)
		if err == nil {
			t.Errorf("ParseBoolFlag(%q) = (%v, nil), want a non-nil error", val, got)
			continue
		}
		if got != false {
			t.Errorf("ParseBoolFlag(%q) returned bool=%v alongside an error, want false", val, got)
		}
		if !errors.Is(err, ErrMalformedBool) {
			t.Errorf("ParseBoolFlag(%q) error %v does not wrap ErrMalformedBool", val, err)
		}
		var bve *BoolVarError
		if !errors.As(err, &bve) {
			t.Errorf("ParseBoolFlag(%q) error is not a *BoolVarError: %v", val, err)
			continue
		}
		if bve.Var != "SESH_BRO_TEST" || bve.Value != val {
			t.Errorf("BoolVarError = %+v, want Var=SESH_BRO_TEST Value=%q", bve, val)
		}
	}
}

// TestPreviewEnabled_HideCurrent_DirSources_PropagateMalformedValues checks
// the three call-site methods surface ParseBoolFlag's error under the
// right variable name, and that a malformed PREVIEW_ENABLED does not
// somehow also break HIDE_CURRENT or DIR_SOURCES — each of bash's three
// `-eq 1` tests is independent and can fail on its own (BEHAVIOUR.md §9
// S1: "a `sesh-bro create` run never evaluates CFG_PREVIEW_ENABLED at
// all").
func TestPreviewEnabled_HideCurrent_DirSources_PropagateMalformedValues(t *testing.T) {
	c := Load(envMap(map[string]string{
		"SESH_BRO_PREVIEW_ENABLED": "nope",
		"SESH_BRO_HIDE_CURRENT":    "0",
		"SESH_BRO_DIR_SOURCES":     "1",
	}))
	if _, err := c.PreviewEnabled(); err == nil {
		t.Error("PreviewEnabled() with a malformed value returned nil error")
	} else {
		var bve *BoolVarError
		if errors.As(err, &bve) && bve.Var != "SESH_BRO_PREVIEW_ENABLED" {
			t.Errorf("PreviewEnabled error names variable %q, want SESH_BRO_PREVIEW_ENABLED", bve.Var)
		}
	}
	if hc, err := c.HideCurrent(); err != nil || hc {
		t.Errorf("HideCurrent() = (%v, %v), want (false, nil) — independent of the malformed PREVIEW_ENABLED", hc, err)
	}
	if ds, err := c.DirSources(); err != nil || !ds {
		t.Errorf("DirSources() = (%v, %v), want (true, nil) — independent of the malformed PREVIEW_ENABLED", ds, err)
	}
}

// TestDefaultFilterFlag covers cmd_picker's default-filter case statement
// (sesh-bro:510-517): the four recognised values map to their flags,
// "all" is the explicit no-op, and anything else — including a value
// that's meaningful ELSEWHERE, like an agent status — is silently ignored
// (ok == false), never an error.
func TestDefaultFilterFlag(t *testing.T) {
	cases := []struct {
		filter   string
		wantFlag string
		wantOK   bool
	}{
		{"all", "", false},
		{"workspaces", "--workspaces", true},
		{"agents", "--agents", true},
		{"dirs", "--dirs", true},
		{"blocked", "--blocked", true},
		{"working", "", false}, // a valid agent status, but not one of the four filter arms
		{"", "", false},
		{"bogus", "", false},
	}
	for _, tc := range cases {
		c := Config{DefaultFilter: tc.filter}
		flag, ok := c.DefaultFilterFlag()
		if flag != tc.wantFlag || ok != tc.wantOK {
			t.Errorf("DefaultFilterFlag() for %q = (%q, %v), want (%q, %v)", tc.filter, flag, ok, tc.wantFlag, tc.wantOK)
		}
	}
}

// TestIcons asserts the render.Icons convenience is a plain field copy —
// no transformation, no default substitution (Load already applied
// defaults by the time Icons is called).
func TestIcons(t *testing.T) {
	c := Config{IconWorkspace: "W", IconAgent: "A", IconDir: "D"}
	icons := c.Icons()
	if icons.Workspace != "W" || icons.Agent != "A" || icons.Dir != "D" {
		t.Errorf("Icons() = %+v, want {W A D}", icons)
	}
}

// TestLoad_KeyEmptyValueFallsBackToDefault is SESH_BRO_KEY_*'s share of
// TestLoad_EmptyValueFallsBackToDefault: a variable present but set to the
// exact empty string is indistinguishable from unset, same as every other
// SESH_BRO_* variable (BEHAVIOUR.md §1.5's `${VAR:-default}` rule, which
// this port's own SESH_BRO_KEY_* variables follow by design even though
// bash never had them).
func TestLoad_KeyEmptyValueFallsBackToDefault(t *testing.T) {
	c := Load(envMap(map[string]string{
		"SESH_BRO_KEY_CLOSE":  "",
		"SESH_BRO_KEY_CREATE": "",
	}))
	if c.KeyClose != "alt-x" {
		t.Errorf("KeyClose = %q, want default alt-x", c.KeyClose)
	}
	if c.KeyCreate != "ctrl-/" {
		t.Errorf("KeyCreate = %q, want default ctrl-/", c.KeyCreate)
	}
}

// TestLoad_KeyCloseDefaultAgreesWithPicker pins this package's close-key
// default to internal/picker's. The two drifted apart exactly once — config
// said ctrl-q, picker said alt-x — and shipped the bug this fix closes: the
// config value wins at runtime (ctrl-q passes picker's validKey), and since
// ctrl-q is one of fzf's four default abort keys, the very keystroke fzf
// trains users to press for "get me out of here" silently closed a
// workspace (see picker.DefaultKeyBindings's own comment for the original
// reasoning). Two defaults for the same key with no test relating them is
// how that shipped; this is the test that makes the next drift a failure,
// not a release note.
func TestLoad_KeyCloseDefaultAgreesWithPicker(t *testing.T) {
	c := Load(envMap(map[string]string{}))
	if c.KeyClose != picker.DefaultKeyBindings.Close {
		t.Errorf("config KeyClose default = %q, but picker.DefaultKeyBindings.Close = %q — the two packages' close-key defaults have drifted; they must be changed together (or better, only here — the picker keeps its own copy because BuildArgs must work without config)", c.KeyClose, picker.DefaultKeyBindings.Close)
	}
}

// TestKeys asserts the picker.KeyBindings convenience is a plain field
// copy — Keys' exact counterpart to TestIcons above. No guard here: a
// malformed value survives this call unchanged, because sanitizeKey lives
// in internal/picker, not here (see the package doc comment and
// TestLoad_MalformedKeyDegradesOnlyInPicker below for where that guard
// actually fires).
func TestKeys(t *testing.T) {
	c := Config{
		KeyWorkspaces: "alt-w",
		KeyAgents:     "alt-e",
		KeyBlocked:    "alt-b",
		KeyDirs:       "alt-x",
		KeyAll:        "alt-o",
		KeyCreate:     "alt-c",
		KeyClose:      "bad:key", // deliberately malformed — passed through untouched here
	}
	want := picker.KeyBindings{
		Workspaces: "alt-w",
		Agents:     "alt-e",
		Blocked:    "alt-b",
		Dirs:       "alt-x",
		All:        "alt-o",
		Create:     "alt-c",
		Close:      "bad:key",
	}
	if got := c.Keys(); got != want {
		t.Errorf("Keys() = %+v, want %+v", got, want)
	}
}

// TestLoad_MalformedKeyDegradesOnlyInPicker is the end-to-end contract the
// task asks for: a malformed SESH_BRO_KEY_* value must never reach fzf as a
// broken --bind flag. Config.Load and Config.Keys both pass it through
// unvalidated (TestKeys above) — the guard fires one layer later, in
// picker.BuildArgs (via KeyBindings.resolved/sanitizeKey), by which point
// the argv is well-formed regardless of what SESH_BRO_KEY_CLOSE held.
func TestLoad_MalformedKeyDegradesOnlyInPicker(t *testing.T) {
	c := Load(envMap(map[string]string{
		"SESH_BRO_KEY_CLOSE": "ctrl-q:evil(rm -rf ~)", // colon + parens: would corrupt the --bind argv
	}))
	// Survives Load/Keys unvalidated, exactly like every other SESH_BRO_*
	// raw field this package exposes (PreviewWidth, Aliases, Blacklist).
	if c.KeyClose != "ctrl-q:evil(rm -rf ~)" {
		t.Fatalf("KeyClose = %q, want the raw malformed value preserved by Load", c.KeyClose)
	}
	args := picker.BuildArgs(picker.Options{SelfPath: "/bin/sesh-bro", Keys: c.Keys()})
	wantCloseBind := "--bind=alt-x:execute('/bin/sesh-bro' close --from-file {+f})+reload('/bin/sesh-bro' list --header )"
	found := false
	for _, a := range args {
		if a == wantCloseBind {
			found = true
		}
		if a == "--bind=ctrl-q:evil(rm -rf ~):execute('/bin/sesh-bro' close --from-file {+f})+reload('/bin/sesh-bro' list --header )" {
			t.Fatalf("malformed SESH_BRO_KEY_CLOSE reached the argv uncorrected: %q", a)
		}
	}
	if !found {
		t.Errorf("close bind fell back to the default alt-x, but %q was not found in argv %v", wantCloseBind, args)
	}
}

// AttentionFirst defaults on — the 0.4.0 picker's whole premise is that
// "does anything need me" is answered before you type.
func TestAttentionFirstDefaultsOn(t *testing.T) {
	c := Load(func(string) string { return "" })
	on, err := c.AttentionFirst()
	if err != nil {
		t.Fatalf("AttentionFirst() error = %v", err)
	}
	if !on {
		t.Error("AttentionFirst() = false, want true by default")
	}
}

func TestAttentionFirstOff(t *testing.T) {
	c := Load(func(k string) string {
		if k == "SESH_BRO_ATTENTION_FIRST" {
			return "0"
		}
		return ""
	})
	on, err := c.AttentionFirst()
	if err != nil {
		t.Fatalf("AttentionFirst() error = %v", err)
	}
	if on {
		t.Error("AttentionFirst() = true, want false")
	}
}

// A malformed value errors at the point of use, like the other three lazily
// parsed booleans (BEHAVIOUR.md §9 S1) — it must not break `create`.
func TestAttentionFirstMalformed(t *testing.T) {
	c := Load(func(k string) string {
		if k == "SESH_BRO_ATTENTION_FIRST" {
			return "yes"
		}
		return ""
	})
	if _, err := c.AttentionFirst(); err == nil {
		t.Error("AttentionFirst() error = nil, want a parse error for \"yes\"")
	}
}
