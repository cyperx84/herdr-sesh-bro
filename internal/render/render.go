// Package render turns already-resolved row and preview data into the exact
// byte sequences bash's sesh-bro prints. It owns no I/O — no herdr calls, no
// exec, no filesystem — callers fetch data via herdr-api or exec (git, eza,
// bat, pane reads...) and hand this package plain strings and bools.
//
// The wire format reproduced here is specified in docs/BEHAVIOUR.md §§3-4
// (list rows) and §2.8 (preview headers). Every function below cites the
// exact sesh-bro source line it mirrors; where the bash does something that
// looks like a bug, the doc comment says so and reproduces it anyway — see
// docs/BEHAVIOUR.md §9 for the catalogue.
package render

import (
	"fmt"
	"strings"
)

// Reset and Dim are bash's $C_RESET and $C_DIM (sesh-bro lines 49-50).
// Colours are emitted unconditionally throughout this package — sesh-bro
// has no TTY check and no NO_COLOR handling, because fzf reads list output
// through a pipe with --ansi and needs the escapes literally in the bytes.
const (
	Reset = "\x1b[0m"
	Dim   = "\x1b[2m"
)

// StatusUnknown is the literal status bash passes to status_color in both
// preview "not found" branches (sesh-bro lines 447, 463). It is not one of
// the five recognised statuses, so it resolves to the same bright black as
// any other unrecognised value — exported so callers don't need to
// hardcode the string themselves.
const StatusUnknown = "unknown"

// StatusColor maps an agent/workspace status to the ANSI foreground escape
// bash's status_color() prints (sesh-bro lines 51-61, BEHAVIOUR.md §1.6).
// Anything outside the five recognised statuses — including "unknown",
// the literal "-" dir rows carry, and empty — falls to bright black.
func StatusColor(status string) string {
	switch status {
	case "blocked":
		return "\x1b[31m"
	case "working":
		return "\x1b[33m"
	case "idle":
		return "\x1b[32m"
	case "done":
		return "\x1b[34m"
	case "dir":
		return "\x1b[36m"
	case "worktree":
		// Magenta, distinct from dir's cyan: the two look alike in a row
		// (both are just paths) and the difference — one is a checkout you
		// have not opened, the other is any directory you have visited — is
		// exactly what the colour has to carry.
		return "\x1b[35m"
	default:
		return "\x1b[90m"
	}
}

// Icons holds the three glyphs `list` rows use. They are the only part of a
// row's rendering that user config touches (SESH_BRO_ICON_WORKSPACE/AGENT/
// DIR, BEHAVIOUR.md §5) — preview headers ignore this struct entirely and
// hardcode ◆/●/▸ regardless of config (BEHAVIOUR.md §S12), which is why the
// Preview* functions below take no Icons argument.
type Icons struct {
	Workspace string
	Agent     string
	Dir       string
	Worktree  string
}

// DefaultIcons returns bash's built-in glyphs (sesh-bro lines 43-45), used
// whenever the corresponding SESH_BRO_ICON_* variable is unset or empty.
func DefaultIcons() Icons {
	return Icons{Workspace: "◆", Agent: "●", Dir: "▸"}
}

// Kind is a list row's type: the first of its three TSV fields, the value
// fzf's {1} placeholder exposes, and what cmd_connect/cmd_preview dispatch
// on (BEHAVIOUR.md §4.1).
type Kind string

const (
	KindWorkspace Kind = "workspace"
	KindAgent     Kind = "agent"
	KindDir       Kind = "dir"
	// KindWorktree is a git worktree on disk with no workspace open on it
	// (BEHAVIOUR.md §10.10). It renders like a directory because that is what
	// picking one does — open a workspace there — and it carries the same "-"
	// status placeholder, having no agent of its own.
	KindWorktree Kind = "worktree"
)

// FormatRow renders one `list` row exactly as bash's row-render loop does
// (sesh-bro lines 273-283, BEHAVIOUR.md §4): three TAB-separated fields —
// type, target, display — terminated by one newline. No field is quoted or
// escaped; target may contain spaces (dir paths) but must never contain a
// TAB or newline itself, an invariant this function assumes the caller
// upholds rather than checks. There is deliberately no column alignment or
// padding — rows are ragged in bash and must stay ragged here.
//
// status drives the icon colour for workspace and agent rows via
// StatusColor. dir rows ignore status entirely and always colour their
// icon with StatusColor("dir") — a dir row's actual status field is the
// literal "-", and bash hardcodes the "dir" lookup (sesh-bro line 280)
// rather than passing status through.
//
// detail is wrapped verbatim in Dim...Reset. A workspace row's git-branch
// suffix is not this function's concern: bash splices it into detail one
// loop earlier (sesh-bro line 258), so callers that want it must build
// detail as `plainDetail + GitSuffix(branch, dirty)` before calling
// FormatRow. That ordering is why a dirty workspace row's rendered bytes
// end in a doubled ESC[0m (BEHAVIOUR.md §4.3) — GitSuffix's own inner
// reset survives inside this function's outer Dim wrapper. Reproduce it,
// don't collapse it.
func FormatRow(kind Kind, target, status, label, detail string, icons Icons) (string, error) {
	var color, glyph string
	switch kind {
	case KindWorkspace:
		color, glyph = StatusColor(status), icons.Workspace
	case KindAgent:
		color, glyph = StatusColor(status), icons.Agent
	case KindDir:
		color, glyph = StatusColor("dir"), icons.Dir
	case KindWorktree:
		color, glyph = StatusColor("worktree"), icons.Worktree
	default:
		// Unreachable from bash's own list builder: only these three kinds
		// are ever produced (BEHAVIOUR.md §4.1). Bash's equivalent `case`
		// has no default branch, so an unrecognised type would silently
		// reuse whatever $icon held from the previous loop iteration — a
		// stateful accident of a shell loop variable with no equivalent in
		// a stateless per-row function. Rather than fabricate that
		// behaviour, this rejects it; the condition cannot occur via any
		// path documented in BEHAVIOUR.md §4.1.
		return "", fmt.Errorf("render: unknown row kind %q", kind)
	}
	icon := color + glyph + Reset
	// A row is one TSV line, so any tab or newline inside label or detail
	// fabricates a second, fully-formed row that fzf cannot distinguish from a
	// real one.
	//
	// This existed in the bash too, where the worst it bought you was focusing
	// the wrong workspace. The close action makes it destructive: a label
	// carrying "\nworkspace\tw-PROD\t..." renders an innocuous-looking extra
	// row whose close target is a workspace the user never saw. And labels are
	// not all self-authored — the worktree path builds one from a GitHub issue
	// title, which is text from a public repo.
	//
	// Stripped rather than rejected: a weird label should render oddly, not
	// make the picker fail to list anything.
	label = stripRowSeparators(label)
	detail = stripRowSeparators(detail)
	return fmt.Sprintf("%s\t%s\t%s %s %s\n", kind, target, icon, label, Dim+detail+Reset), nil
}

// stripRowSeparators replaces the characters that would end a row or a field.
func stripRowSeparators(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\t', '\n', '\r':
			return ' '
		}
		return r
	}, s)
}

// GitSuffix formats the branch annotation bash's git-enrichment loop
// appends to a workspace row's detail field, before the render loop wraps
// the whole field in Dim...Reset (sesh-bro line 258, BEHAVIOUR.md §2.2.8,
// §4.2). Append it to a plain detail string yourself — `detail +
// GitSuffix(branch, dirty)` — and only when a branch was actually found;
// bash never calls this for a workspace whose cwd is not a git repo.
//
// The reset immediately after "[" deliberately un-dims the branch name for
// display; FormatRow's own outer Dim re-dims everything around it, giving
// the nested dim/reset/dim/reset/reset sequence documented in
// BEHAVIOUR.md §4.3. That is the real on-screen appearance, not a defect
// to fix here.
func GitSuffix(branch string, dirty bool) string {
	marker := ""
	if dirty {
		marker = "*"
	}
	return " " + Dim + "[" + Reset + branch + marker + Dim + "]" + Reset
}

// PreviewWorkspaceNotFound renders what `preview workspace` prints when
// `herdr workspace get` returns nothing or an error (sesh-bro line 447,
// BEHAVIOUR.md §2.8). The ◆ glyph is hardcoded — SESH_BRO_ICON_WORKSPACE is
// never consulted by any preview header, unlike list rows.
func PreviewWorkspaceNotFound(target string) string {
	return StatusColor(StatusUnknown) + "◆ " + target + Reset + "  " + Dim + "(not found)" + Reset + "\n"
}

// PreviewWorkspaceHeader renders the header printed before a found
// workspace's pane text (sesh-bro lines 450-453): status-coloured glyph,
// label, then the dimmed workspace id in parentheses, then a blank line.
// The caller appends the focused (or first) pane's raw ANSI screen text
// directly after this, with no separator and no trailing newline added —
// `herdr pane read` output is printed verbatim (BEHAVIOUR.md §6).
func PreviewWorkspaceHeader(status, label, target string) string {
	return StatusColor(status) + "◆ " + label + Reset + "  " + Dim + "(" + target + ")" + Reset + "\n\n"
}

// PreviewAgentNotFound mirrors PreviewWorkspaceNotFound for `preview agent`
// (sesh-bro line 463). The ● glyph is likewise hardcoded.
func PreviewAgentNotFound(target string) string {
	return StatusColor(StatusUnknown) + "● " + target + Reset + "  " + Dim + "(not found)" + Reset + "\n"
}

// PreviewAgentHeader renders the header for a found agent (sesh-bro lines
// 466-471). Bash reads agent_status from the same JSON blob twice — once
// to pick status's colour, once as text in the detail line — so callers
// should resolve it once and pass that single value for both purposes;
// this function only takes it once.
func PreviewAgentHeader(status, name, cwd string) string {
	return StatusColor(status) + "● " + name + Reset + "  " + Dim + status + " · " + cwd + Reset + "\n\n"
}

// PreviewAgentReason renders herdr's own account of WHY the agent is in this
// state, on its own dim line under the header.
//
// The status word alone cannot distinguish a bash command waiting for approval
// from an MCP elicitation from a question inside a dynamic workflow. All three
// render as `blocked`, and they are not equally urgent. herdr decides between
// them by running a prioritised rule set against the pane, so naming the rule
// that won turns one word into an answer.
//
// Empty reason means empty output, not a line saying nothing: this is
// best-effort enrichment from a method a 0.8.2 daemon does not have, and a
// preview must look deliberate on a daemon that cannot answer.
func PreviewAgentReason(reason string) string {
	if reason == "" {
		return ""
	}
	return Dim + "why: " + reason + Reset + "\n\n"
}

// PreviewDirHeader renders the `preview dir` header (sesh-bro line 475).
// Bash spells the colour as a literal ESC[36m rather than calling
// status_color, but it is the identical escape StatusColor("dir") returns.
// Note there is no space before the trailing reset — the %s for C_RESET
// sits directly after the path with nothing between them.
func PreviewDirHeader(path string) string {
	return StatusColor("dir") + "▸ " + path + Reset + "\n\n"
}

// PreviewDirNotFound renders the line shown when the preview target is not
// an existing directory (sesh-bro line 477).
func PreviewDirNotFound() string {
	return Dim + "(directory not found)" + Reset + "\n"
}

// PreviewDirReadmeSeparator renders the divider printed between a
// directory listing and its README's contents (sesh-bro line 488).
// basename is the README's filename alone, not a path.
func PreviewDirReadmeSeparator(basename string) string {
	return "\n" + Dim + "── " + basename + " ──" + Reset + "\n\n"
}

// PreviewUnknown is what `preview` prints for a row type it does not
// recognise (sesh-bro line 496). Unreachable through the picker, which
// only ever emits workspace/agent/dir targets, but reachable via a direct
// `sesh-bro preview <other> <target>` invocation.
func PreviewUnknown() string {
	return "no preview\n"
}

// CountsStyle selects how CountsLine renders. The three exist because the
// line has three consumers with incompatible constraints: a herdr tab-bar
// entry takes plain text and no escapes, a terminal wants colour, and a
// status-bar script wants to do its own formatting.
type CountsStyle int

const (
	// CountsPlain is glyph-and-number with no escapes: "🔴2 🟡5 🔵1 ⚪12".
	// herdr's tab_bar_right takes the last line of a command's stdout as
	// literal text, so colour has to be carried by the glyph itself.
	CountsPlain CountsStyle = iota
	// CountsANSI colours a dot per status and names it: "● 2 blocked · …",
	// using the same palette as list rows so the two never disagree.
	CountsANSI
)

// countsLabel is the word CountsANSI prints after each number.
var countsLabel = map[string]string{
	"blocked": "blocked",
	"working": "working",
	"done":    "done",
	"idle":    "idle",
	"unknown": "unknown",
}

// countsGlyph is CountsPlain's colour channel. Emoji rather than a coloured
// dot because the destination — a terminal tab title or a status bar — is
// plain text that cannot carry an escape sequence.
var countsGlyph = map[string]string{
	"blocked": "🔴",
	"working": "🟡",
	"done":    "🔵",
	"idle":    "⚪",
	"unknown": "⚫",
}

// CountsLine renders one line summarising how many agents are in each state.
//
// order fixes the sequence (callers pass herdrx.StatusOrder). A status with a
// zero count is omitted unless includeZero, because the line's whole job is to
// be readable at a glance in a tab bar: "🔴2 🟡5" says what matters, while
// "🔴2 🟡5 🔵0 ⚪0 ⚫0" makes the reader parse noise to find the one number
// that changed. With nothing at all to report the line is "no agents", never
// empty — an empty line in a tab bar reads as a broken command.
//
// The result never contains a newline: herdr takes the LAST line of stdout,
// so a multi-line result would silently show only its tail.
func CountsLine(counts map[string]int, order []string, style CountsStyle, includeZero bool) string {
	parts := make([]string, 0, len(order))
	total := 0
	for _, status := range order {
		n := counts[status]
		total += n
		if n == 0 && !includeZero {
			continue
		}
		switch style {
		case CountsANSI:
			parts = append(parts, fmt.Sprintf("%s●%s %d %s", StatusColor(status), Reset, n, countsLabel[status]))
		default:
			parts = append(parts, fmt.Sprintf("%s%d", countsGlyph[status], n))
		}
	}
	if len(parts) == 0 || total == 0 && !includeZero {
		return "no agents"
	}
	sep := " "
	if style == CountsANSI {
		sep = " · "
	}
	return strings.Join(parts, sep)
}

// HeaderRowKind is the type field of the pinned header row. It is not a
// picker candidate and can never be selected — fzf's --header-lines removes
// the line from the match list entirely — but the row still has to carry three
// tab-separated fields, because --with-nth=3.. applies to every input line and
// a short line would display the wrong part of itself.
const HeaderRowKind = "header"

// HeaderRow wraps an already-rendered summary line as `list`'s first output
// row, for fzf's --header-lines=1 to pin above the results.
//
// Putting the counts INSIDE the row stream rather than in fzf's --header flag
// is what makes them live: --header is fixed for the process's lifetime, while
// a header line is part of the input and is therefore replaced by every
// reload. So the counts update with the list, for free, using the mechanism
// that was already updating the list.
func HeaderRow(line string) string {
	return HeaderRowKind + "\t-\t" + line + "\n"
}
