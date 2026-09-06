// cmdList reproduces cmd_list (sesh-bro:129-284, BEHAVIOUR.md §2.2) — the
// core of the tool. It parses list's flags, resolves the current workspace,
// pulls workspace/agent/dir rows, applies git enrichment, and prints either
// pretty-printed concatenated JSON objects (--json, §2.2.9) or three-field
// TSV rows (§2.2.10).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	herdr "github.com/cyperx84/herdr-api"

	"github.com/cyperx84/herdr-sesh-bro/internal/attention"
	"github.com/cyperx84/herdr-sesh-bro/internal/config"
	"github.com/cyperx84/herdr-sesh-bro/internal/external"
	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx"
	"github.com/cyperx84/herdr-sesh-bro/internal/render"
)

// listFlags is cmd_list's parsed argument state (sesh-bro:130).
type listFlags struct {
	wantWS, wantAgent, wantDir bool
	statuses                   []herdr.AgentStatus
	hideCurrent                bool
	asJSON                     bool
	// header emits a pinned counts row as the first line, for fzf's
	// --header-lines=1. Like --json it is an OUTPUT flag: parseListFlags must
	// not let it touch the source selection, or `list --header --agents`
	// would differ from `list --agents --header` (the S3 trap).
	header bool
}

// parseListFlags reproduces sesh-bro:131-144 field for field — CRITICALLY,
// left to right, in one pass: a status flag (--blocked/--working/--done/
// --idle) clears wantWS/wantDir the MOMENT it is parsed (S3, BEHAVIOUR.md
// §9), so `list --dirs --blocked` and `list --blocked --dirs` produce
// different results depending purely on argument order. A design that
// collects all flags into a set first and decides sources afterward gets
// this wrong; do not refactor this into anything but a single left-to-right
// scan.
func parseListFlags(args []string) (listFlags, error) {
	var f listFlags
	sourceFlag := false
	for _, a := range args {
		switch a {
		case "--workspaces":
			f.wantWS, sourceFlag = true, true
		case "--agents":
			f.wantAgent, sourceFlag = true, true
		case "--dirs":
			f.wantDir, sourceFlag = true, true
		case "--blocked":
			f.statuses = append(f.statuses, herdr.StatusBlocked)
			f.wantAgent, f.wantWS, f.wantDir, sourceFlag = true, false, false, true
		case "--working":
			f.statuses = append(f.statuses, herdr.StatusWorking)
			f.wantAgent, f.wantWS, f.wantDir, sourceFlag = true, false, false, true
		case "--done":
			f.statuses = append(f.statuses, herdr.StatusDone)
			f.wantAgent, f.wantWS, f.wantDir, sourceFlag = true, false, false, true
		case "--idle":
			f.statuses = append(f.statuses, herdr.StatusIdle)
			f.wantAgent, f.wantWS, f.wantDir, sourceFlag = true, false, false, true
		case "--hide-current":
			f.hideCurrent = true
		case "--json":
			f.asJSON = true
		case "--header":
			f.header = true
		default:
			return listFlags{}, fmt.Errorf("sesh-bro list: unknown flag %s", a)
		}
	}
	if !sourceFlag {
		f.wantWS, f.wantAgent, f.wantDir = true, true, true
	}
	return f, nil
}

// listFlagError distinguishes "unknown flag" (exit 2) from every other
// listOutput failure (exit 1) without cmdList needing to string-match error
// text. cmdPicker (picker.go) doesn't care about the distinction at all —
// per S16 it treats every listOutput failure identically (print, degrade to
// an empty row stream, keep going) — so only cmdList inspects this type.
type listFlagError struct{ err error }

func (e *listFlagError) Error() string { return e.err.Error() }
func (e *listFlagError) Unwrap() error { return e.err }

func cmdList(ctx context.Context, env *appEnv, args []string) int {
	if err := listOutput(ctx, env, args, env.stdout); err != nil {
		fmt.Fprintln(env.stderr, err)
		var fe *listFlagError
		if errors.As(err, &fe) {
			return 2
		}
		return 1
	}
	return 0
}

// listOutput is cmd_list's full body (sesh-bro:129-284, BEHAVIOUR.md §2.2)
// as a reusable function: everything cmdList does, minus deciding the exit
// code and minus the one-line "how do I report this" choice, so cmdPicker
// (picker.go) can generate the picker's initial row stream through the
// EXACT same logic `sesh-bro list` itself runs — reproducing bash's
// `"$SELF" list ... | fzf`, just in-process instead of through a pipe to a
// self-reinvoked subprocess.
// listSources is every input a row render needs, fetched once.
//
// Splitting the fetch from the render is what lets the live picker (Phase 4)
// re-render on a herdr event without re-querying anything it already has, and
// re-render a DIFFERENT view — workspaces only, blocked only — from the same
// snapshot instead of re-execing `list` per keypress the way the fzf reload
// binds used to. `list` itself calls the two back to back and is unchanged in
// observable behaviour.
type listSources struct {
	snap    herdrx.Snapshot
	current string // current workspace id, "" when unknown
	hide    string // workspace id to omit, "" to omit nothing
	zoxide  []string
	git     *external.GitCache
	// state is what the [[events]] hook recorded: when each agent entered
	// its status, for the time-in-state badges. Empty when nothing has been
	// recorded yet, which costs badges and nothing else.
	state attention.State
}

// loadSources performs every read `list` needs: one session.snapshot, plus
// zoxide and a git cache when the flags and config call for them.
//
// The flag gating is deliberately preserved rather than simplified into
// "fetch everything". BEHAVIOUR.md §9 S1 makes a malformed SESH_BRO_DIR_SOURCES
// an error only for a run that actually wants directories; evaluating it
// unconditionally here would newly break `list --agents` on a machine with a
// typo in that variable.
func loadSources(ctx context.Context, env *appEnv, cfg config.Config, flags listFlags) (listSources, error) {
	client, openErr := openHerdr(env.getenv)
	if err := external.CheckListDeps(ctx, env.herdrBin, aliverFor(client, openErr)); err != nil {
		return listSources{}, err
	}

	// One call replaces workspace.list + agent.list + pane.list twice, and
	// with them the pane cache whose TTL bucket meant it almost never hit
	// (BEHAVIOUR.md §10, superseding §7.3 and S4).
	snap, err := loadSnapshot(ctx, client, openErr)
	if err != nil {
		return listSources{}, err
	}

	src := listSources{snap: snap, git: external.NewGitCache(), state: attention.Load(statePath(env.getenv))}

	src.current = herdrx.CurrentWorkspaceID(env.getenv("HERDR_WORKSPACE_ID"), env.getenv("HERDR_PLUGIN_CONTEXT_JSON"))
	if src.current == "" {
		// New in 0.4.0 (BEHAVIOUR.md §10, superseding §1.7/§2.2.3): fall back
		// to whatever the daemon says is focused. The env vars are only set
		// inside a herdr-spawned pane, so running the binary from a plain
		// shell used to disable current-first ordering entirely and make
		// --hide-current a silent no-op. The daemon knows the answer either
		// way; there is no reason to pretend otherwise.
		src.current = snap.FocusedWorkspace()
	}

	// sesh-bro:156: `[[ $hide_current -eq 1 || $CFG_HIDE_CURRENT -eq 1 ]]`
	// short-circuits on the FLAG — a garbage SESH_BRO_HIDE_CURRENT does not
	// crash `list --hide-current`, only a bare `list` with no flag and a
	// bad config value does (S1, BEHAVIOUR.md §9). Evaluate the config bool
	// ONLY when the flag itself didn't already decide the answer.
	if flags.hideCurrent {
		src.hide = src.current
	} else {
		hc, err := cfg.HideCurrent()
		if err != nil {
			return listSources{}, err
		}
		if hc {
			src.hide = src.current
		}
	}

	// Directory block inputs: sesh-bro:187-211, gated on want_dir AND
	// $CFG_DIR_SOURCES AND zoxide on PATH. CFG_DIR_SOURCES is evaluated
	// (and can crash, S1) ONLY when want_dir is true.
	if flags.wantDir {
		dirSources, err := cfg.DirSources()
		if err != nil {
			return listSources{}, err
		}
		if dirSources && external.ZoxideAvailable() {
			src.zoxide = external.ZoxideList(ctx)
		}
	}

	return src, nil
}

// renderRows turns already-fetched sources into `list`'s output for one view.
// It performs no I/O beyond writing to w and the git subprocesses the cache
// has not already answered, so the live picker can call it once per view.
func renderRows(ctx context.Context, cfg config.Config, flags listFlags, src listSources, w io.Writer) error {
	var wsRows, agRows []herdrx.Row
	if flags.wantWS {
		wsRows = herdrx.WorkspaceRows(src.snap.Workspaces, src.current, src.hide)
	}
	if flags.wantAgent {
		agRows = herdrx.AgentRows(src.snap.Agents, src.current, src.hide, flags.statuses)
		// JSON output stays machine-shaped: a badge is a display affordance,
		// and an age baked into a detail string is not something a consumer
		// should have to parse back out.
		if !flags.asJSON {
			agRows = herdrx.WithAges(agRows, ageBadges(src, time.Now()))
		}
	}

	var dirRows []herdrx.Row
	if flags.wantDir && len(src.zoxide) > 0 {
		known := herdrx.KnownCWDs(src.snap.Panes)
		dirRows = herdrx.DirRows(src.zoxide, known, cfg.Blacklist)
	}

	// Attention hoist (BEHAVIOUR.md §10, superseding the ordering of
	// §2.2.4-2.2.7). Blocked and done agents leave the agents block and go
	// above every other block, from whichever workspace they belong to, so
	// the cursor opens on whoever needs you instead of on the workspace you
	// are already sitting in. Everything else keeps its existing order, and
	// SESH_BRO_SORT_ORDER still governs the blocks below (S5's "a block
	// absent from a non-empty sort_order is dropped" is unchanged — the
	// hoist only reorders agent rows that were going to be emitted anyway).
	attentionFirst, err := cfg.AttentionFirst()
	if err != nil {
		return err
	}
	var raw []herdrx.Row
	if attentionFirst && flags.wantAgent {
		attention, rest := herdrx.SplitAttention(agRows)
		raw = append(raw, attention...)
		raw = append(raw, assembleBlocks(cfg.SortOrder, wsRows, rest, dirRows)...)
	} else {
		raw = assembleBlocks(cfg.SortOrder, wsRows, agRows, dirRows)
	}

	// Git enrichment: sesh-bro:235-265, BEHAVIOUR.md §2.2.8 — JSON output
	// skips it entirely (§2.2.9).
	if !flags.asJSON && len(raw) > 0 && external.GitAvailable() {
		wsCWD := external.WorkspaceCWDs(panesToExternal(src.snap.Panes))
		gitMap := src.git.Enrich(ctx, "", wsCWD)
		for i := range raw {
			if raw[i].Type != render.KindWorkspace {
				continue
			}
			if gs, ok := gitMap[raw[i].Target]; ok {
				raw[i].Detail += render.GitSuffix(gs.Branch, gs.Dirty)
			}
		}
	}

	if flags.asJSON {
		// No header row in JSON: it is a display affordance for fzf, not a
		// candidate, and a consumer parsing rows should not have to skip it.
		writeJSONRows(w, raw)
		return nil
	}

	if flags.header {
		// The counts describe the whole session, not the filtered view: the
		// question the header answers is "is anything waiting anywhere",
		// which must not change because the user pressed the dirs key.
		counts := herdrx.CountByStatus(src.snap.Agents)
		fmt.Fprint(w, render.HeaderRow(render.CountsLine(statusCounts(counts), statusOrder(), render.CountsANSI, false)))
	}

	icons := cfg.Icons()
	for _, r := range raw {
		line, err := render.FormatRow(r.Type, r.Target, r.Status, r.Label, r.Detail, icons)
		if err != nil {
			// Unreachable via any row this package itself produces (only
			// the three known Kinds are ever generated) — see FormatRow's
			// own doc comment.
			continue
		}
		fmt.Fprint(w, line)
	}
	return nil
}

func listOutput(ctx context.Context, env *appEnv, args []string, w io.Writer) error {
	// sesh-bro:131-143 runs before ANY dependency check — an unknown flag
	// exits 2 even with herdr missing or the daemon down (BEHAVIOUR.md
	// §2.2.11's own table: "unknown flag" is its own row, independent of
	// the dependency rows).
	flags, err := parseListFlags(args)
	if err != nil {
		return &listFlagError{err}
	}

	cfg := config.Load(env.getenv)
	src, err := loadSources(ctx, env, cfg, flags)
	if err != nil {
		return err
	}
	return renderRows(ctx, cfg, flags, src, w)
}

// assembleBlocks reproduces sesh-bro:215-230 (BEHAVIOUR.md §2.2.7): with
// $CFG_SORT_ORDER empty, the fixed default order workspaces, agents, dirs;
// otherwise a comma-separated list of those three tokens, in whatever order
// and repetition it names — an unrecognised token contributes nothing, and
// (S5, BEHAVIOUR.md §9) a token simply ABSENT from a non-empty SORT_ORDER
// means that entire block is dropped, not merely deprioritised. Appending an
// empty row slice is a silent no-op, which is exactly bash's own
// `[[ -n $ws_block ]] && raw+=...` guard — no separate emptiness check is
// needed here.
func assembleBlocks(sortOrder string, ws, ag, dir []herdrx.Row) []herdrx.Row {
	order := []string{"workspaces", "agents", "dirs"}
	if sortOrder != "" {
		order = bashSplit(sortOrder, ',')
	}
	var raw []herdrx.Row
	for _, part := range order {
		switch part {
		case "workspaces":
			raw = append(raw, ws...)
		case "agents":
			raw = append(raw, ag...)
		case "dirs":
			raw = append(raw, dir...)
		}
	}
	return raw
}

// bashSplit reproduces `IFS=<sep> read -r -a parts <<< "$s"` field
// splitting: empty input yields zero fields, interior consecutive
// delimiters yield empty fields, but exactly one trailing delimiter is
// absorbed rather than producing a trailing empty field. Identical in shape
// to internal/picker's unexported bashSplitColon (which this package cannot
// import — that helper is deliberately unexported, per its own doc comment,
// to internal/picker's one call site) but parameterised on the separator
// rune, since sort_order splits on ',' where picker's aliases split on ':'.
func bashSplit(s string, sep rune) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, string(sep))
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

// panesToExternal narrows []herdr.Pane to []external.Pane — the two shapes
// are NOT structurally interchangeable in Go despite sharing field names
// (external.Pane is its own package's type; herdr.Pane values do not
// satisfy it without an explicit per-field copy), so this loop is required,
// not a formality.
func panesToExternal(panes []herdr.Pane) []external.Pane {
	out := make([]external.Pane, len(panes))
	for i, p := range panes {
		out[i] = external.Pane{WorkspaceID: p.WorkspaceID, CWD: p.CWD}
	}
	return out
}

// jsonRow is the exact key set and order BEHAVIOUR.md §2.2.9 documents:
// type, target, status, label, detail — encoding/json preserves struct
// field declaration order, so this ordering is load-bearing, not cosmetic.
type jsonRow struct {
	Type   string `json:"type"`
	Target string `json:"target"`
	Status string `json:"status"`
	Label  string `json:"label"`
	Detail string `json:"detail"`
}

// writeJSONRows reproduces sesh-bro:268 (BEHAVIOUR.md §2.2.9, §9 S8): each
// row is a SEPARATE pretty-printed JSON object, two-space indented,
// concatenated with no enclosing array and no blank line between them — NOT
// an array and NOT JSON Lines. json.Encoder.Encode appends exactly one
// trailing newline per call, which is what makes consecutive objects abut
// the way jq's default (non -c) output mode does. SetEscapeHTML(false) is
// required: encoding/json's default HTML-escapes &, <, > inside strings
// (e.g. a workspace label or terminal title containing one), and jq's
// output never does — a workspace labelled "A&B" must round-trip as
// literal "A&B", not "A&B".
func writeJSONRows(w io.Writer, rows []herdrx.Row) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	for _, r := range rows {
		// Encoder.Encode's only failure mode for this concrete, always-
		// marshalable struct is a write error on w — nothing left to do
		// about that from here (bash's own `printf | jq` has the identical
		// property: a broken stdout pipe just truncates the output).
		_ = enc.Encode(jsonRow{
			Type:   string(r.Type),
			Target: r.Target,
			Status: r.Status,
			Label:  r.Label,
			Detail: r.Detail,
		})
	}
}

// ageBadges maps pane id to a rendered time-in-state, for the rows that show
// one. A pane whose recorded status disagrees with the live snapshot is
// skipped by attention.Since: the hook missed a transition, and a badge from a
// stale start time would be confidently wrong.
func ageBadges(src listSources, now time.Time) map[string]string {
	if len(src.state.Panes) == 0 {
		return nil
	}
	out := make(map[string]string, len(src.snap.Agents))
	for _, a := range src.snap.Agents {
		status := string(herdrx.NormalizeStatus(a.Status))
		if d, ok := attention.Since(src.state, a.PaneID, status, now); ok {
			out[a.PaneID] = attention.FormatAge(d)
		}
	}
	return out
}
