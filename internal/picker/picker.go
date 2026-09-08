// Package picker is sesh-bro's fzf integration: it builds the exact fzf
// argv cmd_picker constructs, runs fzf with a caller-supplied row stream on
// its stdin, parses the winning row back into a type/target pair, and
// drives the post-selection connect + failure-prompt dance.
//
// fzf itself is never reimplemented — Go has no equivalent fuzzy matcher
// with fzf's exact ranking and key-binding behaviour, and the whole point
// of shelling out is that the user's fingers already know this tool. This
// package's only job is producing byte-identical argv and reproducing the
// surrounding shell plumbing (single-quoting, trailing spaces, swallowed
// exit codes) that bash gets "for free" and Go must build by hand.
//
// It owns none of: row generation (`list`, herdr-api calls — see
// internal/render and whatever package drives herdr-api), SESH_BRO_ARGS
// word-splitting or CFG_DEFAULT_FILTER substitution (cmd_picker lines
// 502-517, sesh-bro:501-576), require_herdr, or cmd_connect's actual
// workspace/agent/dir logic. Callers resolve all of that and hand this
// package the already-decided Options plus a Connector closure — see
// Options and Connector below.
//
// Every function cites the exact sesh-bro source line(s) and BEHAVIOUR.md
// section it reproduces. Where bash does something odd, the comment says
// so and this package does it anyway — that is the rewrite's prime
// directive (docs/BEHAVIOUR.md's own preamble): a port that "fixes" the
// bash silently is a different tool.
package picker

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Connector performs the connect action for a picker selection — this is
// cmd_connect (sesh-bro:287-320), owned by another package because it
// makes herdr-api calls this package has no business knowing about. To
// preserve bash's exact two-message failure sequence (BEHAVIOUR.md §3.4),
// a Connector implementation must behave like cmd_connect: on failure it
// writes its OWN diagnostic to stderr first (e.g. "sesh-bro: failed to
// focus workspace <id>", "sesh-bro: not a directory: <target>" — Appendix
// B) and then returns a non-nil error. Run prints a second, generic line
// on top of that — see Run's doc comment.
type Connector func(kind, target string) error

// Selection is the picker's parsed choice: fields 1 and 2 of the winning
// TSV row (sesh-bro:568-570, BEHAVIOUR.md §3.4, §4.1). Field 3 (the
// display text) is never returned — fzf hands back exactly what it read
// off its own stdin, and --with-nth=3.. only changes what is *shown* and
// matched against, never what a selected line's raw bytes are.
type Selection struct {
	// Kind is "workspace", "agent", or "dir" through the picker in
	// practice, but this package does not validate it — see cutField.
	Kind string
	// Target is the connect/preview handle: a workspace id, an agent name
	// or pane id, or an absolute directory path.
	Target string
}

// Options configures one fzf invocation. Every field maps directly onto a
// piece of cmd_picker or the fzf command line it builds (sesh-bro:501-564,
// BEHAVIOUR.md §2.10, §3).
type Options struct {
	// SelfPath is bash's $SELF (sesh-bro:12-19) — the sesh-bro binary's own
	// resolved, UNQUOTED path. Run single-quotes it itself (quoteSingle,
	// mirroring $SELF_Q at sesh-bro:30) before splicing it into the
	// --preview and --bind strings; callers must not pre-quote it.
	SelfPath string

	// Rows is the TSV byte stream `list` produced — bash's
	// `"$SELF" list ... | fzf`, fed here directly to fzf's stdin instead of
	// through a re-exec of `list`. picker.go never generates or parses row
	// content; that is internal/render plus whatever package drives the
	// herdr-api calls that back `list`. May be nil (treated as empty).
	Rows io.Reader

	// HideCurrent is bash's `$hide_flag != ""` (sesh-bro:504-508): true
	// when --hide-current appeared anywhere in the combined SESH_BRO_ARGS +
	// argv list cmd_picker assembled. It affects ONLY the reload binds
	// (BEHAVIOUR.md §3.3 — "Reload binds carry only --hide-current, never
	// the status filters"); the initial Rows stream already reflects
	// whatever full flag set the caller used to build it.
	HideCurrent bool

	// PreviewEnabled is CFG_PREVIEW_ENABLED == 1 (sesh-bro:540). false
	// omits --preview entirely. --preview-window is still emitted either
	// way — bash passes it unconditionally (BEHAVIOUR.md §3.1: "harmless").
	PreviewEnabled bool

	// PreviewWidth is CFG_PREVIEW_WIDTH, unsanitised. BuildArgs applies the
	// same regex guard bash does (sesh-bro:543-544) and falls back to
	// "60%" for anything not matching ^[0-9]+%?$ — including "".
	PreviewWidth string

	// Aliases is raw CFG_ALIASES ("a=b:c=d", sesh-bro:46). BuildArgs
	// derives the fzf --query prefill from it (sesh-bro:522-534) — see
	// aliasQuery.
	Aliases string

	// Keys is every fzf --bind key, resolved from SESH_BRO_KEY_<ACTION>
	// (docs/COMPETITIVE-DEMAND.md #2) or the zero value when a caller
	// doesn't set it. BuildArgs runs this through KeyBindings.resolved,
	// which falls back to DefaultKeyBindings field-by-field for anything
	// empty or malformed — so leaving Keys unset entirely (every existing
	// caller, before this field existed) reproduces bash's six hardcoded
	// binds exactly, plus Feature A's new Close bind at its own default.
	Keys KeyBindings

	// Fzf is what the installed fzf actually supports (see Detect). Every
	// feature it reports absent degrades to the pre-0.4.0 behaviour rather
	// than failing, so the zero value builds exactly the argv 0.3.0 built.
	Fzf Features

	// HeaderLines pins the first input row as a header instead of matching
	// against it — `list --header` emits a live counts line there. It is a
	// separate flag from Fzf because --header-lines predates every feature
	// Detect probes; the caller decides whether it asked `list` for the row.
	HeaderLines bool

	// ListenSocket is a unix socket path fzf serves actions on (--listen,
	// fzf 0.66). Empty, or an fzf without Listen, means nothing can push into
	// this picker and it behaves as a snapshot. fzf requires the path to end
	// in ".sock".
	ListenSocket string

	// RowsDir holds pre-rendered per-view row files (all.tsv, agents.tsv, …)
	// plus a "view" marker file naming the one currently displayed.
	//
	// When set, the filter keys reload by `cat`-ing a file instead of
	// re-executing this binary, which is the difference between a keypress
	// costing a process start plus a daemon round trip and it costing a read
	// of a few kilobytes. The marker file is how the process pushing updates
	// knows which view to re-send. Empty keeps the 0.3.0 behaviour of
	// re-execing `list` per keypress.
	RowsDir string

	// Stderr receives both this package's own diagnostics ("sesh-bro: fzf
	// is required", "sesh-bro: failed to connect to <kind> <target>") and
	// fzf's own inherited stderr (its TUI paints via /dev/tty, not stdout
	// or stderr, but fzf does write some diagnostics to stderr). Bash
	// never redirects the fzf invocation's stderr, so it flows straight to
	// the terminal; defaulting this to os.Stderr reproduces that. Inject a
	// buffer in tests.
	Stderr io.Writer

	// PressAnyKey blocks until the user acknowledges a connect failure,
	// standing in for bash's
	// `read -r -p "..." -n 1 _ </dev/tty 2>/dev/null || sleep 1`
	// (sesh-bro:573, BEHAVIOUR.md §3.4, S13). Defaults to waitForKeypress.
	// Inject a no-op in tests — see Run's doc comment for why the "Press
	// enter..." text itself is never reproduced anywhere, on purpose.
	PressAnyKey func()
}

// ErrFzfNotFound is returned when fzf is not on PATH — bash's
// `command -v fzf >/dev/null 2>&1 || { echo "sesh-bro: fzf is required" >&2; exit 1; }`
// guard (sesh-bro:520), which fires before the fzf command line is ever
// built and unconditionally exits 1.
var ErrFzfNotFound = errors.New("picker: fzf not found on PATH")

// ErrConnectFailed is returned when a real selection's Connector call
// fails. Mirrors cmd_picker's exit-1 path (sesh-bro:571-575, BEHAVIOUR.md
// §3.4, S13): the picker does not reopen and does not retry; the caller
// should exit 1 and do nothing further, exactly as bash does.
var ErrConnectFailed = errors.New("picker: connect failed")

// widthRe is the preview-width guard regex, copied verbatim from
// sesh-bro:544 (`[[ $pw =~ ^[0-9]+%?$ ]]`).
var widthRe = regexp.MustCompile(`^[0-9]+%?$`)

// quoteSingle reproduces bash's $SELF_Q derivation exactly
// (sesh-bro:30: `SELF_Q="'${SELF//\'/\'\\\'\'}'"`) — the standard
// POSIX-shell single-quote escape: close the quote, emit an escaped
// literal quote, reopen the quote, for every embedded `'`; wrap the whole
// result in a leading and trailing `'`. This protects $SELF when it is
// spliced into the --preview/--bind command strings fzf later hands to
// `sh -c`. It has nothing to do with fzf's own {n}-placeholder quoting
// (BEHAVIOUR.md §3.2), which is a separate mechanism fzf applies itself.
func quoteSingle(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// sanitizeWidth reproduces the preview-width guard at sesh-bro:543-544:
// anything not matching ^[0-9]+%?$ — including the empty string — falls
// back to "60%". "60" and "45%" pass through unchanged; "half", "60px",
// "-10%" and "" do not.
func sanitizeWidth(pw string) string {
	if widthRe.MatchString(pw) {
		return pw
	}
	return "60%"
}

// KeyBindings is every fzf --bind key sesh-bro assigns to an action, one
// field per action bash hardcoded (sesh-bro:558-563) plus Close, which bash
// never had. This is Feature B (docs/COMPETITIVE-DEMAND.md #2, Navigator
// issues/26 — "unmet by every competitor... the one item where we could
// lead rather than catch up"): every field here is overridable via
// SESH_BRO_KEY_<ACTION> (internal/config.Config.Keys resolves the env var
// plus default; this package resolves "present but malformed" — see
// resolved). A zero-value KeyBindings (every caller before this feature
// existed) resolves to DefaultKeyBindings in full.
type KeyBindings struct {
	Workspaces string // reload: workspaces only.
	Agents     string // reload: agents only.
	Blocked    string // reload: blocked agents only.
	Dirs       string // reload: directories only.
	Worktrees  string // reload: unopened git worktrees only.
	Issues     string // reload: open GitHub issues for this repo only.
	Star       string // toggle the highlighted agent's pin.
	Reply      string // send the first canned reply to a blocked agent.
	All        string // reload: all sources.
	Create     string // execute-silent create, then reload.
	// Close is Feature A's new bind (docs/COMPETITIVE-DEMAND.md #1: "Close /
	// remove a workspace from the picker" — four independent competitor
	// plugins converge on this, sesh-bro had none). execute-silent close on
	// the highlighted row, then reload, the same shape as Create.
	Close string
}

// DefaultKeyBindings is the key assigned to each action when Options.Keys
// leaves a field unset or KeyBindings.resolved's guard rejects it: bash's
// six literal binds (sesh-bro:558-563: ctrl-w/e/b/x/o/-slash), plus alt-x
// for Close.
//
// Close is NOT on ctrl-q, which an earlier version chose by checking this
// picker's own six binds and forgetting fzf's. fzf documents four default
// abort keys — ctrl-c, ctrl-g, ctrl-q, esc — so binding a silent,
// irreversible workspace close to one of them means the keystroke fzf itself
// trains users to press for "get me out of here" destroys a workspace and
// every agent running in it, with execute-silent swallowing any message.
//
// alt-x is deliberately awkward: this is the one action here that cannot be
// undone, and a modifier that is hard to hit by accident is the point.
var DefaultKeyBindings = KeyBindings{
	Workspaces: "ctrl-w",
	Agents:     "ctrl-e",
	Blocked:    "ctrl-b",
	Dirs:       "ctrl-x",
	Worktrees:  "ctrl-t",
	// alt-i, not ctrl-i: a terminal sends ctrl-i as Tab, so binding it would
	// steal the multi-select key and be impossible to press on its own.
	Issues: "alt-i",
	Star:   "ctrl-s",
	Reply:  "ctrl-y",
	All:    "ctrl-o",
	Create: "ctrl-/",
	Close:  "alt-x",
}

// validKey reports whether a SESH_BRO_KEY_* override is a key fzf will
// actually accept.
//
// An earlier version allowed anything shaped like [A-Za-z0-9][A-Za-z0-9_/-]*,
// which is a guard against splicing (":" would append a second ACTION, ","
// another key spec) but says nothing about fzf's key grammar. Measured against
// fzf 0.74.2, "shift-a", "zzz", "f25", "ctrl-ww" and "alt-" all pass that
// shape and all make fzf exit 2 with "unsupported key" — and because Run
// deliberately swallows exec failures, the picker then flashes and vanishes
// with exit 0 and no diagnostic anywhere the user can see. One typo in one env
// var makes the picker unopenable and unexplainable, which is exactly the
// failure the fallback-to-default rule exists to prevent.
//
// So match fzf's real grammar instead of a character class.
func validKey(k string) bool {
	if k == "" {
		return false
	}
	switch {
	case namedKeys[k]:
		return true
	case strings.HasPrefix(k, "ctrl-"):
		return isSingleKeyChar(k[len("ctrl-"):])
	case strings.HasPrefix(k, "alt-"):
		return isSingleKeyChar(k[len("alt-"):])
	case fnKeyRe.MatchString(k):
		return true
	}
	// A single literal character is a valid fzf key.
	return len([]rune(k)) == 1 && !strings.ContainsAny(k, ":,()")
}

// isSingleKeyChar reports whether s is one character a modifier may apply to.
func isSingleKeyChar(s string) bool {
	r := []rune(s)
	return len(r) == 1 && !strings.ContainsAny(s, ":,()")
}

// fnKeyRe matches f1 through f12. fzf accepts up to f24 on some terminals, but
// nothing above f12 is reliably deliverable, and a key that silently never
// fires is its own bug.
var fnKeyRe = regexp.MustCompile(`^f([1-9]|1[0-2])$`)

// namedKeys is fzf's set of spelled-out keys, which no pattern captures.
var namedKeys = map[string]bool{
	"tab": true, "shift-tab": true, "enter": true, "return": true,
	"space": true, "bspace": true, "bs": true, "del": true, "delete": true,
	"home": true, "end": true, "pgup": true, "page-up": true,
	"pgdn": true, "page-down": true, "insert": true,
	"up": true, "down": true, "left": true, "right": true,
	"shift-up": true, "shift-down": true, "shift-left": true, "shift-right": true,
	"double-click": true, "left-click": true, "right-click": true,
}

// sanitizeKey guards one SESH_BRO_KEY_* override: def is returned verbatim
// unless val is non-empty AND validKey accepts it. The val == "" branch mirrors
// sanitizeWidth's own "" -> default case, but in practice rarely fires here
// — internal/config's Load already turns an unset/empty env var into the
// same default before this ever sees it (two-layer default, exactly like
// PreviewWidth); it only matters for a caller that builds Options directly
// without going through config.Load, e.g. this package's own tests.
func sanitizeKey(val, def string) string {
	if val != "" && validKey(val) {
		return val
	}
	return def
}

// resolved applies sanitizeKey to every field against DefaultKeyBindings,
// once, so BuildArgs does not repeat the same seven-way fallback inline.
func (k KeyBindings) resolved() KeyBindings {
	return KeyBindings{
		Workspaces: sanitizeKey(k.Workspaces, DefaultKeyBindings.Workspaces),
		Agents:     sanitizeKey(k.Agents, DefaultKeyBindings.Agents),
		Blocked:    sanitizeKey(k.Blocked, DefaultKeyBindings.Blocked),
		Dirs:       sanitizeKey(k.Dirs, DefaultKeyBindings.Dirs),
		Worktrees:  sanitizeKey(k.Worktrees, DefaultKeyBindings.Worktrees),
		Issues:     sanitizeKey(k.Issues, DefaultKeyBindings.Issues),
		Star:       sanitizeKey(k.Star, DefaultKeyBindings.Star),
		Reply:      sanitizeKey(k.Reply, DefaultKeyBindings.Reply),
		All:        sanitizeKey(k.All, DefaultKeyBindings.All),
		Create:     sanitizeKey(k.Create, DefaultKeyBindings.Create),
		Close:      sanitizeKey(k.Close, DefaultKeyBindings.Close),
	}
}

// keyLabel renders a resolved bind key for the --header hint text: a
// "ctrl-X" key where X is exactly one character shortens to bash's own
// caret notation (sesh-bro:555: "^w workspaces", "^/ create") since that IS
// every DefaultKeyBindings value's natural display form; anything else (an
// alt-* bind, a function key, a bare letter with no ctrl- prefix, ...) is
// shown verbatim, since there is no established shorthand for those.
//
// Making the header track the ACTUAL resolved key, rather than staying
// hardcoded to bash's six literals the way preview headers ignore
// SESH_BRO_ICON_* (BEHAVIOUR.md §9 S12), is a deliberate divergence from
// that precedent: an icon override is cosmetic, but a header hint naming a
// key that no longer does anything — because SESH_BRO_KEY_CLOSE moved it
// elsewhere — would actively mislead the user standing in front of the
// picker, which is a worse failure than the one S12 accepts.
func keyLabel(key string) string {
	if rest, ok := strings.CutPrefix(key, "ctrl-"); ok && len(rest) == 1 {
		return "^" + rest
	}
	return key
}

// bashSplitColon reproduces `IFS=':' read -r -a pairs <<< "$s"` field
// splitting (sesh-bro:529). This is NOT strings.Split: measured directly
// against bash 5.x —
//
//	IFS=":" read -r -a p <<< ""      → 0 fields
//	IFS=":" read -r -a p <<< "a=b:"  → 1 field  ["a=b"]
//	IFS=":" read -r -a p <<< "a=b::" → 2 fields ["a=b", ""]
//	IFS=":" read -r -a p <<< ":a=b"  → 2 fields ["", "a=b"]
//
// i.e. an empty input yields zero fields, interior consecutive delimiters
// yield empty fields, but exactly one trailing delimiter is absorbed
// rather than producing a trailing empty field — the same way word
// splitting drops trailing IFS whitespace. strings.Split alone gets the
// empty-input and trailing-delimiter cases wrong, so this trims at most
// one trailing empty element after splitting.
func bashSplitColon(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ":")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

// aliasQuery reproduces sesh-bro:522-534: with SESH_BRO_ALIASES set to
// exactly one "alias=label" pair, the part before the first "=" prefills
// fzf's --query so that alias's row floats to the top (a pair with no "="
// at all, e.g. "dev", prefills the whole token — `${pair%%=*}` on "dev" is
// "dev"). With zero pairs (Aliases empty) or two-or-more pairs, there is
// no prefill: the bash comment at sesh-bro:524-526 explains that a
// multi-term query would OR-match unrelated rows, so the user just types
// the alias and fzf's own ranking puts the exact label match first.
func aliasQuery(aliases string) string {
	pairs := bashSplitColon(aliases)
	if len(pairs) != 1 {
		return ""
	}
	alias, _, _ := strings.Cut(pairs[0], "=")
	return alias
}

// BuildArgs builds the exact fzf argv — every element bash produces at
// sesh-bro:536-564 (BEHAVIOUR.md §3), in bash's exact order, as one Go
// string per argv slot. The "fzf" program name itself is not included;
// callers pass this directly to exec.Command("fzf", BuildArgs(opts)...).
//
// Because exec.Command never goes through a shell, each returned element
// is delivered to fzf as one opaque argv word with no further splitting or
// re-quoting — the same guarantee bash's own double-quoted
// `--bind="ctrl-w:reload($SELF_Q list --workspaces $hide_flag)"` gives,
// just achieved by a different mechanism (no shell at all, vs. bash
// quoting suppressing word-splitting).
func BuildArgs(opts Options) []string {
	selfQ := quoteSingle(opts.SelfPath)
	hide := ""
	if opts.HideCurrent {
		hide = "--hide-current"
	}
	pw := sanitizeWidth(opts.PreviewWidth)
	query := aliasQuery(opts.Aliases)
	keys := opts.Keys.resolved()

	args := []string{
		"--ansi",
		// Multi-select. Enter still connects to one destination (the first
		// selected row); what --multi adds is the ability to hand several rows
		// to close and prompt at once, each of which shows what it resolved
		// before acting.
		"--multi",
		// Two literal characters, backslash then 't' — fzf parses this as
		// a regex matching one TAB byte (BEHAVIOUR.md §3.1 note); it is
		// NOT a real tab character embedded in the argv.
		`--delimiter=\t`,
		"--with-nth=3..",
		"--layout=reverse",
		"--tiebreak=index",
		"--query=" + query,
		// Trailing space in the prompt is part of it (sesh-bro:554).
		"--prompt=sesh> ",
		// U+00B7 MIDDLE DOT between clauses, copied verbatim from
		// sesh-bro:555, extended with Feature A's close action and built
		// from the RESOLVED keys rather than bash's hardcoded literals —
		// see keyLabel's doc comment for why this header must stay truthful
		// under SESH_BRO_KEY_* overrides where the preview headers don't.
	}

	// The key hints move to --footer when fzf can render one (0.72), which
	// frees the header for the live counts row. Below that version they stay
	// exactly where they have always been.
	hints := fmt.Sprintf(
		"enter connect · %s workspaces · %s agents · %s blocked · %s dirs · %s worktrees · %s issues · %s all · %s star · %s reply · %s close · %s create",
		keyLabel(keys.Workspaces), keyLabel(keys.Agents), keyLabel(keys.Blocked),
		keyLabel(keys.Dirs), keyLabel(keys.Worktrees), keyLabel(keys.Issues), keyLabel(keys.All),
		keyLabel(keys.Star), keyLabel(keys.Reply), keyLabel(keys.Close), keyLabel(keys.Create),
	)
	if opts.Fzf.Footer {
		args = append(args, "--footer="+hints)
	} else {
		args = append(args, "--header="+hints)
	}
	if opts.HeaderLines {
		args = append(args, "--header-lines=1")
	}
	if opts.PreviewEnabled {
		// Bare {1} {2} — fzf single-quotes each {n} placeholder itself
		// before handing the command to the shell (BEHAVIOUR.md §3.2), so
		// adding quotes here would double-escape a target containing
		// spaces and break it. sesh-bro:540 passes them bare for exactly
		// this reason.
		args = append(args, fmt.Sprintf("--preview=%s preview {1} {2}", selfQ))
	}
	if opts.Fzf.Listen && opts.ListenSocket != "" {
		// fzf only treats the value as a socket path when it ends in .sock;
		// anything else is parsed as a port, which would open a TCP listener
		// instead. runtimeDir guarantees the suffix, and this is the reason.
		args = append(args, "--listen="+opts.ListenSocket)
	}
	if opts.Fzf.TrackID {
		// Track the cursor by the row's TARGET (field 2), not by its index.
		// The list re-sorts under the user whenever an agent changes state,
		// and index tracking would silently move the selection to whatever
		// row inherited that position. Identity tracking keeps the cursor on
		// the agent the user was looking at — which is precisely the tradeoff
		// herdr core cannot make for its own panel (discussion #2761).
		args = append(args, "--track", "--id-nth=2")
	}

	args = append(args,
		fmt.Sprintf("--preview-window=right,%s,border-left", pw),
		// Every reload/execute-silent bind below re-invokes the compiled
		// binary itself (SelfPath) as a subprocess THAT FZF SPAWNS, not
		// something this package calls directly — BuildArgs only ever
		// produces the strings; fzf owns running them.
		//
		// When hide is "", the trailing space before ")" from the format
		// string below is preserved rather than trimmed
		// (BEHAVIOUR.md §3.3: "the reload string still contains the
		// trailing space... Harmless to the shell; a Go port emitting the
		// same strings byte-for-byte is safest.").
		// Close is emitted BEFORE every navigation bind on purpose. fzf's
		// last-bind-wins means a collision — SESH_BRO_KEY_CLOSE=ctrl-w, a
		// plausible preference or a copy-paste slip — would otherwise make a
		// navigation key silently destructive while the header still
		// advertises both. Emitted first, a collision costs the user their
		// close key and leaves the navigation key doing the harmless thing.
		//
		// {1} {2} hand `close` the highlighted row's type and target, exactly
		// as the --preview bind does. No --multi: highlighted-versus-selected
		// cannot then diverge for an irreversible action.
		// {+f}, not {1} {2}, and execute, not execute-silent. Both matter.
		//
		// {+f} writes the SELECTED rows (or the highlighted one when nothing
		// is selected) to a temp file and substitutes its path, so the command
		// receives whole TSV lines and resolves types and targets itself — no
		// argv length limit, no quoting hazard, and under --header-lines the
		// header is unselectable and never appears.
		//
		// execute suspends fzf and hands the child the terminal, which is what
		// lets close print what it is about to destroy and read y/N. That
		// confirmation is what makes --multi safe here: the old design avoided
		// --multi entirely so that "highlighted" and "selected" could not
		// diverge for an irreversible action. That reasoning was right, and it
		// is replaced rather than ignored — the ambiguity is now resolved by
		// showing the user the resolved list before anything happens.
		fmt.Sprintf("--bind=%s:execute(%s close --from-file {+f})+%s", keys.Close, selfQ, reloadCurrent(opts, selfQ, hide)),
		viewBind(opts, keys.Workspaces, "workspaces", selfQ, hide),
		viewBind(opts, keys.Agents, "agents", selfQ, hide),
		viewBind(opts, keys.Blocked, "blocked", selfQ, hide),
		viewBind(opts, keys.Dirs, "dirs", selfQ, hide),
		viewBind(opts, keys.Worktrees, "worktrees", selfQ, hide),
		viewBind(opts, keys.Issues, "issues", selfQ, hide),
		// Pin the highlighted agent. execute-silent because there is nothing
		// to confirm and nothing to read — unlike close, a pin is trivially
		// reversible by pressing the same key again.
		fmt.Sprintf("--bind=%s:execute-silent(%s star --toggle {2})+%s", keys.Star, selfQ, reloadCurrent(opts, selfQ, hide)),
		// Reply is execute-silent and needs no confirm because `reply` refuses
		// any agent that is not blocked: pressed on the wrong row it exits 3
		// and does nothing. That is what makes a one-keypress answer safe
		// enough to sit next to the filter keys.
		fmt.Sprintf("--bind=%s:execute-silent(%s reply {2})+%s", keys.Reply, selfQ, reloadCurrent(opts, selfQ, hide)),
		viewBind(opts, keys.All, "all", selfQ, hide),
		fmt.Sprintf("--bind=%s:execute-silent(%s create)+%s", keys.Create, selfQ, reloadCurrent(opts, selfQ, hide)),
	)
	return args
}

// cutField reproduces `cut -f<n>` with the default TAB delimiter and no
// -s (--only-delimited) flag: a line containing no delimiter at all is
// passed through UNMODIFIED for every requested field — cut does not
// treat a missing delimiter as "field 1 is the line, field 2 is empty".
// Verified directly: `printf 'abc' | cut -f2` prints `abc`, not "". A line
// that does contain at least one TAB is split normally, and a request for
// a field past the end returns "". This matters for the picker
// specifically because of the degenerate `--json` case
// (BEHAVIOUR.md §2.2.9 line 538): "succeeds and feeds fzf pretty-printed
// JSON as rows" — a JSON line reaching the picker has no TAB in it, so
// bash's cmd_picker would try to connect with type == target == that
// whole JSON line, not type == "" or a JSON fragment.
func cutField(line string, n int) string {
	fields := strings.Split(line, "\t")
	if len(fields) == 1 {
		return line
	}
	if n-1 < len(fields) {
		return fields[n-1]
	}
	return ""
}

// ParseSelection parses fzf's captured stdout the way cmd_picker does at
// sesh-bro:566-570. Bash's `$(...)` command substitution strips all
// trailing newlines before the `[[ -z $selection ]]` emptiness check and
// before `cut -f1`/`cut -f2` run; TrimRight here reproduces that. Only a
// wholly-empty (post-trim) raw string reports ok == false — that is bash's
// ONE test for "nothing to connect to", and it fires identically whether
// fzf was aborted (ESC/ctrl-c), matched nothing, or `list` failed upstream
// and produced no rows at all (BEHAVIOUR.md S16). A non-empty raw string
// is always ok == true, even a single word with no TAB in it at all — see
// cutField.
func ParseSelection(raw string) (Selection, bool) {
	sels := ParseSelections(raw)
	if len(sels) == 0 {
		return Selection{}, false
	}
	// Enter on a multi-selection connects to the FIRST row. Connect has one
	// destination — you cannot focus two workspaces — so any other choice is
	// arbitrary, and the first is the one the user's cursor reached first.
	return sels[0], true
}

// ParseSelections parses every line fzf returned.
//
// Splitting on newline BEFORE splitting on tab is the whole correctness
// argument. With --multi, fzf prints one selected line per line; cutField
// splits the entire blob on tabs, so a two-line selection yields a "target"
// spanning both lines — a string that is not any row's target and that every
// downstream lookup silently fails to match. That bug is invisible in
// single-selection use, which is every use this function had before --multi.
func ParseSelections(raw string) []Selection {
	raw = strings.TrimRight(raw, "\n")
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	out := make([]Selection, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		out = append(out, Selection{Kind: cutField(line, 1), Target: cutField(line, 2)})
	}
	return out
}

// waitForKeypress is the default PressAnyKey: open /dev/tty and read one
// byte, falling back to a one-second sleep if /dev/tty cannot be opened —
// mirroring `read -r -p "..." -n 1 _ </dev/tty 2>/dev/null || sleep 1`
// (sesh-bro:573).
//
// This is a KNOWN, DELIBERATE deviation from bash, not an oversight: bash's
// `read -n 1` puts the terminal in character mode via termios and returns
// on the very first keystroke, no Enter required. Reproducing that needs
// raw-mode terminal control (golang.org/x/term or equivalent), which is
// out of scope for this package alone to add as a dependency. A plain
// os.Open("/dev/tty").Read degrades to canonical (line-buffered) mode:
// this blocks until the user presses Enter, then returns the line's first
// byte. Functionally this still "waits for the user to acknowledge before
// continuing" — the actual behaviour this bind exists for — but it is not
// bash's true "any single key, no Enter" gesture. Report this gap rather
// than silently widening the package's dependency footprint to close it.
func waitForKeypress() {
	tty, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0)
	if err != nil {
		time.Sleep(time.Second)
		return
	}
	defer tty.Close()
	buf := make([]byte, 1)
	_, _ = tty.Read(buf)
}

// Run drives one interactive fzf session end to end: build the argv
// (BuildArgs), run fzf with opts.Rows on its stdin, parse the winning row
// (ParseSelection), and — on a real selection — hand it to connect. This
// is cmd_picker's full body (sesh-bro:501-576).
//
// Exit-code mapping for callers (BEHAVIOUR.md Appendix A, "picker" row):
//   - nil error: selection connected, OR the user aborted fzf (ESC/ctrl-c),
//     OR fzf matched nothing, OR the upstream `list` process that produced
//     opts.Rows had already failed and printed its own error — bash's
//     `selection="$(... | fzf ...)" || true` swallows every fzf exit code
//     unconditionally (BEHAVIOUR.md S16), so all four of those cases are
//     indistinguishable from here and all four mean exit 0.
//   - ErrFzfNotFound: fzf is not on PATH. "sesh-bro: fzf is required" has
//     already been written to Stderr. Exit 1.
//   - ErrConnectFailed: connect returned an error for a real selection.
//     Two things happen before this returns, matching sesh-bro:571-575
//     exactly:
//     (1) connect itself has already written its own diagnostic to
//     stderr (that is Connector's contract — see the Connector doc).
//     (2) Run writes a second line, "sesh-bro: failed to connect to
//     <kind> <target>\n", then calls PressAnyKey and blocks until it
//     returns.
//     Bash's version of step (2) also passes "Press enter to return to
//     the picker..." as the `-p` argument to `read`, but `read -p`
//     writes its prompt to stderr, and that whole `read` invocation is
//     itself suffixed `2>/dev/null` (sesh-bro:573) — so the prompt text
//     is generated only to be thrown away. Verified with a pty
//     (`script`): the marker text never appears in the captured output.
//     BEHAVIOUR.md's S13 quotes that prompt as if it were visible; it is
//     not. This is a spec-doc discrepancy, not a behaviour this package
//     should introduce — Run therefore never writes that text anywhere,
//     matching what the bash actually does over what the doc quotes.
//     Exit 1. The picker does NOT reopen — this is bash's real behaviour
//     despite what its own prompt claims (S13).
func Run(opts Options, connect Connector) error {
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	cmd := exec.Command("fzf", BuildArgs(opts)...)
	cmd.Stdin = opts.Rows
	cmd.Stderr = stderr
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			fmt.Fprintln(stderr, "sesh-bro: fzf is required")
			return ErrFzfNotFound
		}
		// Every other failure — fzf aborted (130), no match, or anything
		// else — is swallowed exactly like bash's `|| true`
		// (sesh-bro:564). out.String() is simply empty or partial in
		// these cases, and ParseSelection below turns that into ok ==
		// false the same way an empty $selection does.
	}

	sel, ok := ParseSelection(out.String())
	if !ok {
		return nil
	}

	if err := connect(sel.Kind, sel.Target); err != nil {
		fmt.Fprintf(stderr, "sesh-bro: failed to connect to %s %s\n", sel.Kind, sel.Target)
		press := opts.PressAnyKey
		if press == nil {
			press = waitForKeypress
		}
		press()
		return ErrConnectFailed
	}
	return nil
}

// viewBind builds one filter key's --bind.
//
// With a RowsDir the key reads a pre-rendered file and records which view is
// now showing; without one it re-executes `list` exactly as 0.3.0 did. The
// marker file is written BEFORE the cat so that an update arriving mid-reload
// re-sends the view the user just asked for rather than the one they left.
//
// "all" is spelled as a bare `list` in the re-exec form because that is the
// string bash emitted and the golden argv test pins it (BEHAVIOUR.md §3.3,
// including its trailing space when --hide-current is absent).
func viewBind(opts Options, key, view, selfQ, hide string) string {
	if opts.RowsDir == "" {
		// --header is not optional here: BuildArgs emits --header-lines=1, so
		// a stream without a header row loses its first candidate to fzf's
		// header (BEHAVIOUR.md §10.6). See reloadCurrent.
		if view == "all" {
			return fmt.Sprintf("--bind=%s:reload(%s list --header %s)", key, selfQ, hide)
		}
		return fmt.Sprintf("--bind=%s:reload(%s list --%s --header %s)", key, selfQ, view, hide)
	}
	marker := quoteSingle(filepath.Join(opts.RowsDir, viewMarkerFile))
	rows := quoteSingle(filepath.Join(opts.RowsDir, view+".tsv"))
	return fmt.Sprintf("--bind=%s:reload(printf %%s %s > %s; cat %s)", key, view, marker, rows)
}

// viewMarkerFile names the file inside RowsDir holding the current view.
const viewMarkerFile = "view"

// reloadCurrent is the reload action for a bind that must redisplay whatever
// view is currently on screen after CHANGING something — close, create and
// star.
//
// With a RowsDir it re-executes `list --view-dir`, which reads the view marker
// and renders that view fresh. Both halves matter, and each rules out an
// obvious simpler form:
//
//   - The marker is why it is not plain `list --header`. That resets the
//     display to the default all-sources view no matter which filter the user
//     had active, and leaves the marker saying otherwise, so the next live
//     push silently switches it back (BEHAVIOUR.md §10.6).
//   - A real render is why it is not `rows`, which cats the pre-rendered file.
//     These binds have just changed what the list should say, and star is the
//     case that proves it: a pin moves no herdr state, so no event fires, so
//     nothing re-renders those files — the star would not appear until some
//     unrelated event happened to repaint. Close and create do fire events,
//     but the reload runs first and would show the closed workspace still
//     there until the push landed.
//
// `rows` remains the right answer for the FILTER keys, which change what is on
// screen without changing what is true, and run on a keypress rather than on a
// mutation.
//
// Without a RowsDir there are no files and no marker, so it falls back to the
// 0.3.0 re-exec — with --header, which the pre-0.4.1 form omitted.
func reloadCurrent(opts Options, selfQ, hide string) string {
	if opts.RowsDir == "" {
		return fmt.Sprintf("reload(%s list --header %s)", selfQ, hide)
	}
	return fmt.Sprintf("reload(%s list --header --view-dir %s %s)", selfQ, quoteSingle(opts.RowsDir), hide)
}
