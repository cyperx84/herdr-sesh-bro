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

	args := []string{
		"--ansi",
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
		// sesh-bro:555.
		"--header=enter connect · ^w workspaces · ^e agents · ^b blocked · ^x dirs · ^o all · ^/ create",
	}
	if opts.PreviewEnabled {
		// Bare {1} {2} — fzf single-quotes each {n} placeholder itself
		// before handing the command to the shell (BEHAVIOUR.md §3.2), so
		// adding quotes here would double-escape a target containing
		// spaces and break it. sesh-bro:540 passes them bare for exactly
		// this reason.
		args = append(args, fmt.Sprintf("--preview=%s preview {1} {2}", selfQ))
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
		fmt.Sprintf("--bind=ctrl-w:reload(%s list --workspaces %s)", selfQ, hide),
		fmt.Sprintf("--bind=ctrl-e:reload(%s list --agents %s)", selfQ, hide),
		fmt.Sprintf("--bind=ctrl-b:reload(%s list --blocked %s)", selfQ, hide),
		fmt.Sprintf("--bind=ctrl-x:reload(%s list --dirs %s)", selfQ, hide),
		fmt.Sprintf("--bind=ctrl-o:reload(%s list %s)", selfQ, hide),
		fmt.Sprintf("--bind=ctrl-/:execute-silent(%s create)+reload(%s list %s)", selfQ, selfQ, hide),
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
	raw = strings.TrimRight(raw, "\n")
	if raw == "" {
		return Selection{}, false
	}
	return Selection{Kind: cutField(raw, 1), Target: cutField(raw, 2)}, true
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
