package herdrx

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	herdr "github.com/cyperx84/herdr-api"

	"github.com/cyperx84/herdr-sesh-bro/internal/render"
)

// RowType is which of sesh-bro's three candidate sources a Row came from —
// wire field 1 in BEHAVIOUR.md §4.1. It is a type alias for render.Kind, not
// a second parallel type: render.FormatRow takes a render.Kind, and every
// Row this package produces is fed straight into it by whatever assembles
// `list`'s output. Two exported types spelling the same three strings would
// force a cast at every call site for no reason — see the port's own
// "exported surface must read as one library" requirement.
type RowType = render.Kind

const (
	RowWorkspace = render.KindWorkspace
	RowAgent     = render.KindAgent
	RowDir       = render.KindDir
)

// dirStatus is the literal placeholder BEHAVIOUR.md §2.2.6 puts in a dir
// row's status field. Downstream rendering ignores it — dir rows always get
// status_color("dir") regardless of this value (§4.2) — but the field
// exists on the wire, so Row carries it faithfully rather than leaving it
// empty.
const dirStatus = "-"

// Row is one picker candidate: a workspace, an agent, or a zoxide
// directory. It carries every field both the picker (`list`) and the
// preview (`preview`) need. See BEHAVIOUR.md §4 for the wire contract a
// sibling rendering package turns this into (icon/colour lookup, TSV
// framing) — none of that lives here, this package only produces the data.
type Row struct {
	Type   RowType
	Target string // connect/preview handle — field 2. Never TAB or newline.
	Status string // agent_status, "unknown", or dirStatus for RowDir.
	Label  string
	Detail string
}

// NormalizeStatus maps an absent/empty agent_status to "unknown", matching
// jq's `.agent_status // "unknown"` (BEHAVIOUR.md §2.2.4-5, §2.8). herdr.AgentStatus
// is a plain string field on both Workspace and Agent, so a genuinely absent
// key and an explicit "" are indistinguishable here — acceptable per §5's
// own note that herdr omits the key rather than ever sending "". Exported
// because `preview` needs the identical normalization bash's
// `.agent_status // "unknown"` applies (sesh-bro:451, :467, :470) — not just
// `list`'s WorkspaceRows/AgentRows, which are this function's other callers.
func NormalizeStatus(s herdr.AgentStatus) herdr.AgentStatus {
	if s == "" {
		return herdr.StatusUnknown
	}
	return s
}

// rowPriority is 0 for the current workspace, 1 otherwise — the jq
// pipeline's `.priority` field (BEHAVIOUR.md §2.2.4/§2.2.5). current == ""
// makes every row priority 1: there is no "current" to prefer, which is
// exactly how §2.2.3 disables current-first ordering when the workspace
// context is unknown.
func rowPriority(id, current string) int {
	if current != "" && id == current {
		return 0
	}
	return 1
}

// Basename reproduces bash's `${path##*/}`, falling back to the whole path
// when that strips to empty (BEHAVIOUR.md §2.2.6 point 4; §8 edge case #13:
// path "/" or a path ending in "/"). This is deliberately NOT
// filepath.Base, which treats a trailing slash and the empty string
// differently than bash's suffix-stripping pattern does. Exported: `create`
// and `connect dir` (sesh-bro:352, :313) apply this identical fallback to
// derive a workspace label from a raw path, so the command layer reuses this
// instead of re-deriving the same three lines twice more.
func Basename(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		if b := path[i+1:]; b != "" {
			return b
		}
		return path
	}
	return path
}

// WorkspaceRows converts workspace.list's result into picker rows, matching
// BEHAVIOUR.md §2.2.4's jq pipeline field for field:
//   - a workspace whose id equals hide is dropped entirely; hide == "" drops
//     nothing (that's how --hide-current with no known current becomes a
//     no-op, §2.2.3)
//   - priority 0 for the current workspace, 1 otherwise; stable sort by
//     (priority, number) — sort.SliceStable preserves workspace.list's
//     response order as the tiebreak, exactly like jq's verified-stable
//     sort_by (§2.2.4)
//   - label falls back to the workspace id when empty (same absent-vs-""
//     structural limitation as agentDetail's title fallback: Workspace.Label
//     is a plain string, so this reproduces jq's `// .workspace_id` only
//     because herdr never actually sends an empty label)
//   - detail is "<pane_count>p/<tab_count>t", plus " · current" when focused
//
// Git-branch enrichment (the trailing " [branch]" suffix, §2.2.8) is NOT
// applied here — it isn't a herdr call, it's `git status`, and belongs to
// whichever sibling package owns git.
func WorkspaceRows(workspaces []herdr.Workspace, current, hide string) []Row {
	kept := make([]herdr.Workspace, 0, len(workspaces))
	for _, w := range workspaces {
		if hide != "" && w.ID == hide {
			continue
		}
		kept = append(kept, w)
	}
	sort.SliceStable(kept, func(i, j int) bool {
		pi, pj := rowPriority(kept[i].ID, current), rowPriority(kept[j].ID, current)
		if pi != pj {
			return pi < pj
		}
		return kept[i].Number < kept[j].Number
	})

	rows := make([]Row, len(kept))
	for i, w := range kept {
		label := w.Label
		if label == "" {
			label = w.ID
		}
		detail := fmt.Sprintf("%dp/%dt", w.PaneCount, w.TabCount)
		if w.Focused {
			detail += " · current"
		}
		rows[i] = Row{
			Type:   RowWorkspace,
			Target: w.ID,
			Status: string(NormalizeStatus(w.Status)),
			Label:  label,
			Detail: detail,
		}
	}
	return rows
}

// agentRank orders agent_status the way BEHAVIOUR.md §2.2.5's jq `rank` def
// does: blocked, working, done, idle, then everything else — including
// "unknown" — tied at the bottom.
func agentRank(s herdr.AgentStatus) int {
	switch s {
	case herdr.StatusBlocked:
		return 0
	case herdr.StatusWorking:
		return 1
	case herdr.StatusDone:
		return 2
	case herdr.StatusIdle:
		return 3
	default:
		return 4
	}
}

// agentStatusMatches reports whether status passes an accumulated status
// filter, matching the bash's set-membership test over accumulated
// `--blocked --working ...` flags (§2.2.5's `test(" " + status + " ")`
// against the space-joined filter — every status is a bare word, so this is
// exactly set membership). An empty filter matches everything.
//
// AgentRows takes the filter as a slice rather than bash's space-joined
// accumulator string: the order-dependent flag accumulation that produces
// that string (§9 S3) is `list` command-line parsing, not herdr data.
// Coupling this package to bash's string representation would buy nothing.
func agentStatusMatches(filter []herdr.AgentStatus, status herdr.AgentStatus) bool {
	if len(filter) == 0 {
		return true
	}
	for _, f := range filter {
		if f == status {
			return true
		}
	}
	return false
}

// AgentRows converts agent.list's result into picker rows, matching
// BEHAVIOUR.md §2.2.5's jq pipeline field for field:
//   - agent_status is normalized ("" -> "unknown") BEFORE the hide/status
//     filters run — this is the bash's own order (`map(.agent_status = …)`
//     precedes both `select`s) and it matters: filtering on the raw status
//     would let an unset status silently fail every filter, including "no
//     filter".
//   - target: name, else pane id — never the agent kind (§9 S18: this is
//     deliberate, agent.focus accepts a name or a pane id, not a kind)
//   - label: name, else agent kind, else pane id
//   - detail: "<kind|?> · <terminal_title_stripped|cwd|"">" — see
//     agentDetail's doc comment for the trailing-space caveat (§9 S20)
//   - sort: current-first, then agentRank, then name ascending (empty name
//     sorts first within a rank, so unnamed agents lead), stable
func AgentRows(agents []herdr.Agent, current, hide string, statusFilter []herdr.AgentStatus) []Row {
	kept := make([]herdr.Agent, 0, len(agents))
	for _, a := range agents {
		a.Status = NormalizeStatus(a.Status)
		if hide != "" && a.WorkspaceID == hide {
			continue
		}
		if !agentStatusMatches(statusFilter, a.Status) {
			continue
		}
		kept = append(kept, a)
	}
	sort.SliceStable(kept, func(i, j int) bool {
		pi, pj := rowPriority(kept[i].WorkspaceID, current), rowPriority(kept[j].WorkspaceID, current)
		if pi != pj {
			return pi < pj
		}
		if ri, rj := agentRank(kept[i].Status), agentRank(kept[j].Status); ri != rj {
			return ri < rj
		}
		return kept[i].Name < kept[j].Name
	})

	rows := make([]Row, len(kept))
	for i, a := range kept {
		rows[i] = Row{
			Type:   RowAgent,
			Target: agentTarget(a),
			Status: string(a.Status),
			Label:  agentLabel(a),
			Detail: agentDetail(a),
		}
	}
	return rows
}

// agentTarget is name, else pane id — see AgentRows's doc comment (§9 S18).
func agentTarget(a herdr.Agent) string {
	if a.Name != "" {
		return a.Name
	}
	return a.PaneID
}

// agentLabel is name, else agent kind, else pane id.
func agentLabel(a herdr.Agent) string {
	if a.Name != "" {
		return a.Name
	}
	if a.Agent != nil && *a.Agent != "" {
		return *a.Agent
	}
	return a.PaneID
}

// agentDetail is "<kind|?> · <terminal_title_stripped|cwd|"">".
//
// STRUCTURAL LIMITATION, not a choice: Agent.TerminalTitleStripped is a
// plain string, so an explicit empty title and an absent one are
// indistinguishable here. The bash's jq `//` is false-or-null coalescing —
// an explicit "" is KEPT (rendering "claude · ", not falling back to cwd,
// §9 S20) — and that is what this reproduces by construction, since Go has
// no way to tell "" from "not sent" on a plain string field.
// BEHAVIOUR.md §2.2.5 flags the underlying assumption itself as
// [UNVERIFIED that herdr never sends ""]; if that assumption is ever wrong,
// this function is doing the right thing for the documented case and the
// same (wrong) thing the bash does for the unverified one.
func agentDetail(a herdr.Agent) string {
	kind := "?"
	if a.Agent != nil {
		kind = *a.Agent
	}
	title := a.TerminalTitleStripped
	if title == "" {
		title = a.CWD
	}
	return kind + " · " + title
}

// KnownCWDs returns the set of pane working directories, for exact-match
// dedup against zoxide entries (BEHAVIOUR.md §2.2.6 point 3: comparison is
// byte-exact — no symlink resolution, no trailing-slash normalization).
//
// This is a set rather than bash's `sort -u` + `grep -Fxq` pipeline: same
// observable result (membership), no need to reproduce the intermediate
// sort.
func KnownCWDs(panes []herdr.Pane) map[string]bool {
	known := make(map[string]bool, len(panes))
	for _, p := range panes {
		if p.CWD != "" {
			known[p.CWD] = true
		}
	}
	return known
}

// blacklistMatches reports whether path matches any colon-separated glob in
// blacklist (BEHAVIOUR.md §9 S6).
//
// TWO DELIBERATE DIVERGENCES from the bash, both because the bash's
// behaviour is not reproducible in Go without reintroducing a shell:
//
//  1. bash's `for bl in ${CFG_BLACKLIST//:/$'\n'}` is UNQUOTED, so after
//     splitting on ':' the result also undergoes IFS word-splitting (a
//     blacklist entry containing a space becomes two independent patterns)
//     and pathname expansion (a pattern that happens to match files in
//     $PWD is replaced by those filenames before matching even starts).
//     This function splits on ':' only — no word-splitting, no expansion.
//     §9 S6 itself recommends exactly this and calls it a documented
//     divergence, not a bug fix.
//  2. bash's `[[ $path == $bl ]]` is a shell glob, where a bare `*`
//     crosses `/`. Go's filepath.Match stops at path separators (POSIX
//     fnmatch semantics without FNM_PATHNAME cleared). So a blacklist
//     entry `/Users/x/tmp/*` matches `/Users/x/tmp/a/b` in bash but NOT
//     here. This is a second, distinct divergence from the one S6 names —
//     flag it, don't hide it.
func blacklistMatches(blacklist, path string) bool {
	if blacklist == "" {
		return false
	}
	for _, tok := range strings.Split(blacklist, ":") {
		if tok == "" {
			continue
		}
		if ok, err := filepath.Match(tok, path); err == nil && ok {
			return true
		}
	}
	return false
}

// DirRows converts a zoxide path list into picker rows, matching
// BEHAVIOUR.md §2.2.6 field for field. paths must already be in
// `zoxide query --list` order (frecency, descending) — this function
// preserves that order, it does not sort.
//
// Running zoxide is not this package's concern (it isn't a herdr call); the
// caller runs `zoxide query --list` and passes the result here, along with
// the KnownCWDs set built from a ListPanes call.
func DirRows(paths []string, known map[string]bool, blacklist string) []Row {
	rows := make([]Row, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		if blacklistMatches(blacklist, path) {
			continue
		}
		if known[path] {
			continue
		}
		rows = append(rows, Row{
			Type:   RowDir,
			Target: path,
			Status: dirStatus,
			Label:  Basename(path),
			Detail: path,
		})
	}
	return rows
}
