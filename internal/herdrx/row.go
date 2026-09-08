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
	// PaneID is the agent's pane, empty for workspace and dir rows. Target
	// cannot serve here: it is the agent NAME when one exists, and the
	// recorded time-in-state is keyed by pane.
	PaneID string
	// Session names the herdr session this row lives in, and is EMPTY for
	// every row in the session the picker is running in.
	//
	// Empty-means-local rather than always naming the session, because the
	// emptiness is the test every mutating command makes: a non-empty Session
	// is a row that cannot be focused, prompted or closed, since no herdr API
	// call takes a session and every call therefore lands on whichever daemon
	// it dialled (docs/MULTI-SESSION.md). Making local the zero value means a
	// command that forgets to check gets the safe answer for the rows that
	// make up almost every picker.
	//
	// It is deliberately NOT a fourth TSV field. Adding one would touch
	// --with-nth, --id-nth, cutField, the header row, the preview bind and
	// every reload path, for information the target already carries after
	// ForeignTarget composes it.
	Session string
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
//   - sort: current-first, then agentRank, then most-recently-changed first,
//     then name ascending (empty name sorts first within a rank, so unnamed
//     agents lead), stable
//
// The state_change_seq tiebreak is new in 0.4.0 (BEHAVIOUR.md §10). Within one
// status rank the bash ordered by name alone, which is stable but arbitrary:
// of five blocked agents the one that just started asking you something sat
// wherever the alphabet put it. Ordering by herdr's monotonic change counter
// descending puts the newest transition first, which is the order a picker
// aimed at "who needs me" wants. Name remains the final tiebreak so the sort
// is still total and deterministic.
func AgentRows(agents []Agent, current, hide string, statusFilter []herdr.AgentStatus) []Row {
	kept := make([]Agent, 0, len(agents))
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
		if si, sj := kept[i].StateChangeSeq, kept[j].StateChangeSeq; si != sj {
			return si > sj
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
			PaneID: a.PaneID,
		}
	}
	return rows
}

// WithAges appends a time-in-state badge to the agent rows that have one.
//
// Blocked, done, and idle rows get it — see isBadgedStatus. Idle is the one
// that was missing, and it was the whole point of the feature: herdr
// discussion #707's complaint is that every idle row looks equally relevant
// whether the agent stopped thirty seconds or three hours ago, and a
// long-idle session is a prompt cache quietly expiring, i.e. real money.
// attention.State's done -> idle clock preservation (sameClock) exists so
// that an agent which finished twenty minutes ago still reads "20m" after
// you glance at its tab — done and idle are the same underlying state, idle
// just means you have now looked at it — and a badge that never renders on
// idle rows makes that preserved clock invisible.
//
// Working rows still do not get one, and that restraint is the point: an
// agent blocked for nine minutes is a different situation from one blocked
// for nine seconds, while an age on a row that is actively progressing is
// noise competing with the rows that actually want the human.
//
// ages is keyed by pane id, which is why Row carries one: the target is a
// name when the agent has one, and names are not stable identifiers for this.
// A row with no entry is left exactly as it was, so a missed transition costs
// a badge rather than showing a wrong one.
func WithAges(rows []Row, ages map[string]string) []Row {
	if len(ages) == 0 {
		return rows
	}
	for i := range rows {
		if rows[i].Type != RowAgent || !isBadgedStatus(rows[i].Status) {
			continue
		}
		if age, ok := ages[rows[i].PaneID]; ok && age != "" {
			rows[i].Detail += " · " + age
		}
	}
	return rows
}

// agentTarget is name, else pane id — see AgentRows's doc comment (§9 S18).
func agentTarget(a Agent) string {
	if a.Name != "" {
		return a.Name
	}
	return a.PaneID
}

// agentLabel is name, else agent kind, else pane id.
func agentLabel(a Agent) string {
	if a.Name != "" {
		return a.Name
	}
	if kind := a.Agent.Agent; kind != nil && *kind != "" {
		return *kind
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
func agentDetail(a Agent) string {
	kind := "?"
	if k := a.Agent.Agent; k != nil {
		kind = *k
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

// SplitAttention partitions assembled agent rows into the ones that want you
// and everything else, preserving each group's existing relative order.
//
// "Attention" is blocked or done, and the choice of those two is herdr's own:
// blocked means it recognised an approval or question UI, and done is the idle
// state reached by work you have NOT looked at yet — herdr keeps an agent in
// done until its tab is seen, then it becomes idle. So blocked-or-done is
// exactly "has something for you that you haven't seen", and idle/working/
// unknown are exactly "nothing to do here right now".
//
// The caller hoists the attention group above every other block, which is what
// makes the picker open with the cursor already on whoever needs you rather
// than on the workspace you are sitting in (BEHAVIOUR.md §10 supersedes the
// current-first ordering of §2.2.4-5 for this case). Splitting here rather
// than sorting inside AgentRows keeps the hoist a presentation decision the
// `list` command can turn off (SESH_BRO_ATTENTION_FIRST) without changing how
// rows are built.
func SplitAttention(rows []Row) (attention, rest []Row) {
	for _, r := range rows {
		if r.Type == RowAgent && isAttentionStatus(r.Status) {
			attention = append(attention, r)
			continue
		}
		rest = append(rest, r)
	}
	return attention, rest
}

// isAttentionStatus reports whether a status means the agent wants the human
// right now: blocked (it recognised an approval or question UI) or done
// (idle with work you have NOT looked at yet). SplitAttention hoists on
// this, and widening it is not a simplification but a behaviour change:
// idle rows would get hoisted above the workspace block and wreck the
// picker's ordering. This is deliberately NOT isBadgedStatus below — the
// two differ on idle and only idle, and that difference is load-bearing.
func isAttentionStatus(status string) bool {
	return herdr.AgentStatus(status) == herdr.StatusBlocked ||
		herdr.AgentStatus(status) == herdr.StatusDone
}

// isBadgedStatus reports whether a row deserves a time-in-state badge:
// blocked, done, or idle — everything except actively working. This is
// deliberately NOT isAttentionStatus above. Idle rows get badges (an agent
// idle for three hours is a prompt cache quietly expiring, herdr discussion
// #707) but must NOT be attention-hoisted (idle means you have already
// looked, so there is nothing demanding you). Two predicates that look
// near-identical and mean different things — do not merge them by accident.
func isBadgedStatus(status string) bool {
	return herdr.AgentStatus(status) == herdr.StatusBlocked ||
		herdr.AgentStatus(status) == herdr.StatusDone ||
		herdr.AgentStatus(status) == herdr.StatusIdle
}

// StatusOrder is the order every counts/summary rendering walks: the order
// the human cares about, which is also agentRank's order. Unknown trails
// because it means herdr could not classify the agent, not that the agent is
// finished (herdr-api's AgentStatus.Settled deliberately excludes it).
var StatusOrder = []herdr.AgentStatus{
	herdr.StatusBlocked,
	herdr.StatusWorking,
	herdr.StatusDone,
	herdr.StatusIdle,
	herdr.StatusUnknown,
}

// CountByStatus tallies agents per normalized status. Statuses with no agents
// are absent from the map rather than present-and-zero, so a caller deciding
// what to show can distinguish "none" from "not counted" without a second
// lookup table; StatusOrder is the canonical iteration order.
//
// Normalization matches AgentRows (§2.2.5): an empty status counts as
// unknown, so the totals always add up to len(agents).
func CountByStatus(agents []Agent) map[herdr.AgentStatus]int {
	counts := make(map[herdr.AgentStatus]int, len(StatusOrder))
	for _, a := range agents {
		counts[NormalizeStatus(a.Status)]++
	}
	return counts
}

// StarKey is the identity a pinned agent is remembered by: its name when it
// has one, its pane id otherwise.
//
// A name survives the pane being recreated, which is what makes a pin worth
// having across restarts. A pane id does not, so pinning an unnamed agent
// lasts exactly as long as that pane — stated plainly in the docs rather than
// papered over by renaming the agent, which would be a side effect nobody
// asked for. The returned kind matches internal/stars' Star.Kind.
func StarKey(a Agent) (kind, key string) {
	if a.Name != "" {
		return "agent", a.Name
	}
	return "pane", a.PaneID
}

// WithStars marks pinned agent rows and floats them to the top of their own
// status group.
//
// Within the rank, not above it. A star saying "this one matters to me" must
// not outrank an agent saying "I am blocked and cannot continue" — the picker
// opening on whoever needs you is the promise the whole ordering rests on, and
// a pin that could bury a blocked agent would quietly break it. So a starred
// idle agent leads the idle ones and still sits below every blocked agent,
// which is both useful and safe.
func WithStars(rows []Row, starred map[string]bool) []Row {
	if len(starred) == 0 {
		return rows
	}
	isStar := func(r Row) bool {
		return r.Type == RowAgent && (starred["agent:"+r.Target] || starred["pane:"+r.PaneID])
	}
	for i := range rows {
		if isStar(rows[i]) {
			rows[i].Label = "★ " + rows[i].Label
		}
	}

	// Partition within contiguous runs of equal-status agent rows, never
	// with a comparator over the whole slice.
	//
	// The obvious implementation — sort.SliceStable with a less that returns
	// "starred beats unstarred, and false whenever the ranks differ" — is not
	// a strict weak ordering. It reports every cross-rank pair equal in both
	// directions while ordering same-rank pairs, so equality is not
	// transitive (blocked-A == idle-B and idle-B == blocked-C, yet
	// blocked-C < blocked-A), and a sort is free to emit any permutation for
	// such a comparator.
	//
	// A run is broken by ANY change in status and by any non-agent row, which
	// is what keeps the rest of the incoming order intact: AgentRows sorts by
	// current-workspace-first ABOVE status rank, so the same rank can appear
	// in two separate places, and a star must lead its own run rather than
	// jump the current-first boundary into someone else's.
	for i := 0; i < len(rows); {
		if rows[i].Type != RowAgent {
			i++
			continue
		}
		j := i
		for j < len(rows) && rows[j].Type == RowAgent && rows[j].Status == rows[i].Status {
			j++
		}
		stablePartitionStarsFirst(rows[i:j], isStar)
		i = j
	}
	return rows
}

// stablePartitionStarsFirst moves the starred rows of one run to its front,
// preserving the relative order of both groups.
func stablePartitionStarsFirst(run []Row, isStar func(Row) bool) {
	starred := make([]Row, 0, len(run))
	rest := make([]Row, 0, len(run))
	for _, r := range run {
		if isStar(r) {
			starred = append(starred, r)
			continue
		}
		rest = append(rest, r)
	}
	if len(starred) == 0 || len(rest) == 0 {
		return
	}
	copy(run, starred)
	copy(run[len(starred):], rest)
}
