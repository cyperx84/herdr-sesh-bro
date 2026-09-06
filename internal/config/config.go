// Package config resolves sesh-bro's twelve SESH_BRO_* environment
// variables (sesh-bro:35-46, BEHAVIOUR.md §5) into typed values, replicating
// bash's exact resolution and malformed-value behaviour for each.
//
// Resolution rule (BEHAVIOUR.md §1.5): every variable is read as
// `${SESH_BRO_X:-default}`. Bash's `:-` treats "unset" and "the empty
// string" identically — both fall back to the default — while ANY
// non-empty value, including one that is whitespace-only, is used verbatim.
// Go's os.Getenv makes the same unset/empty conflation for free (it returns
// "" for both), so a plain equality check against "" reproduces this
// exactly; see orDefault.
//
// What this package does NOT do, on purpose: several of these variables are
// already parsed, elsewhere in this rewrite, by pure functions that take
// the raw string directly and cite their own bash line numbers —
// internal/picker owns SESH_BRO_PREVIEW_WIDTH's regex guard (sanitizeWidth,
// sesh-bro:543-544) and SESH_BRO_ALIASES's colon-split-and-query derivation
// (aliasQuery, sesh-bro:522-534); internal/herdrx owns SESH_BRO_BLACKLIST's
// colon-split-and-glob-match (blacklistMatches, sesh-bro:196-203). Config
// exposes those three as plain resolved strings — Load applies the default,
// nothing more — so callers hand them straight to whichever package already
// does the real work, rather than this package re-implementing (and risking
// disagreeing with) logic that already exists and is already tested.
//
// The seven SESH_BRO_KEY_* variables (docs/COMPETITIVE-DEMAND.md #2,
// Navigator issues/26 — no bash equivalent, this port's own feature) follow
// the exact same split: Load resolves unset/empty to each key's hardcoded
// default, and internal/picker's KeyBindings.resolved (sanitizeKey) owns the
// "present but would corrupt a --bind argv string" guard, mirroring
// PreviewWidth's own two-layer default exactly. See Keys below.
//
// What DOES belong here, because no sibling package claims it: the three
// boolean-flavoured variables' S1 crash-or-not guard (PreviewEnabled,
// HideCurrent, DirSources — see ParseBoolFlag), and small derived
// conveniences for the variables nobody else touches yet (DefaultFilterFlag,
// Icons).
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/cyperx84/herdr-sesh-bro/internal/picker"
	"github.com/cyperx84/herdr-sesh-bro/internal/render"
)

// Config is every SESH_BRO_* variable, resolved (default applied, nothing
// else) at the point Load returns. Fields are grouped by how the rest of
// the port consumes them — see the package doc comment.
type Config struct {
	// PreviewWidth is CFG_PREVIEW_WIDTH (sesh-bro:35), raw. The
	// ^[0-9]+%?$-or-"60%" guard (BEHAVIOUR.md §2.10, edge case #5) is
	// internal/picker's sanitizeWidth, applied when this value is actually
	// spliced into fzf's --preview-window argument — not here. Passing
	// this straight through unsanitised, e.g. into picker.Options.PreviewWidth,
	// is correct.
	PreviewWidth string

	// Aliases is CFG_ALIASES (sesh-bro:46), raw ("alias=label:alias2=label2"
	// or ""). internal/picker's aliasQuery derives the fzf --query prefill
	// from this string directly (sesh-bro:522-534) — including the
	// exactly-one-pair rule and the fact that the label half of every pair
	// is dead data, never read anywhere. Nothing to add here.
	Aliases string

	// Blacklist is CFG_BLACKLIST (sesh-bro:41), raw (colon-separated globs,
	// or ""). internal/herdrx's blacklistMatches does the colon-split and
	// filepath.Match glob test directly against this string (BEHAVIOUR.md
	// §9 S6) — including the documented divergence from bash's word-split
	// + pathname-expansion mess, which is not reproducible in Go and is
	// called out there, not duplicated here.
	Blacklist string

	// DefaultFilter is CFG_DEFAULT_FILTER (sesh-bro:40), raw. See
	// DefaultFilterFlag for the enum-to-flag guard bash applies at
	// cmd_picker:510-517.
	DefaultFilter string

	// SortOrder is CFG_SORT_ORDER (sesh-bro:42), raw (comma-separated, or
	// ""). Deliberately left unsplit and uninterpreted: whether a custom
	// order applies at all is `-n $CFG_SORT_ORDER` on this exact raw
	// string (empty means "use the default workspaces,agents,dirs order",
	// BEHAVIOUR.md §2.2.7), and interpreting each token — an unrecognised
	// token is silently dropped, a repeated token duplicates its block,
	// and BEHAVIOUR.md §9 S5's headline surprise is that any block simply
	// absent from this list is OMITTED ENTIRELY, not merely deprioritised —
	// is assembly logic for whichever package builds `list`'s block order,
	// not a config-resolution concern.
	SortOrder string

	// IconWorkspace, IconAgent, IconDir are CFG_ICON_WORKSPACE/AGENT/DIR
	// (sesh-bro:43-45), raw, defaults ◆ ● ▸. Any string is valid — there is
	// no malformed case, bash never validates these beyond the resolution
	// default. See Icons for the render.Icons convenience built from them.
	// Note BEHAVIOUR.md §9 S12: these affect `list` rows only — the
	// `preview` headers hardcode ◆/●/▸ regardless of this config, and
	// stay that way here too (see internal/render's Preview* functions).
	IconWorkspace string
	IconAgent     string
	IconDir       string

	// KeyWorkspaces, KeyAgents, KeyBlocked, KeyDirs, KeyAll, KeyCreate,
	// KeyClose are SESH_BRO_KEY_WORKSPACES/AGENTS/BLOCKED/DIRS/ALL/CREATE/
	// CLOSE, raw, one per fzf --bind action (docs/COMPETITIVE-DEMAND.md #2).
	// Defaults are the six key literals bash hardcoded (sesh-bro:558-563)
	// plus alt-x for KeyClose — see internal/picker.DefaultKeyBindings,
	// which these seven must keep agreeing with (and alt-x, not ctrl-q,
	// is the deliberate choice there: ctrl-q is one of fzf's four default
	// abort keys, so binding a silent, irreversible workspace close to it
	// makes "get me out of here" destroy a workspace. This default
	// disagreed with the picker's for one release and shipped exactly that
	// bug — TestLoad_KeyCloseDefaultAgreesWithPicker pins the two). Any
	// string is accepted here, same as the icon fields; the guard against a
	// value that would break fzf's --bind grammar is internal/picker's
	// KeyBindings.resolved (sanitizeKey), applied where these are actually
	// spliced into an argv element — not here. See Keys.
	KeyWorkspaces string
	KeyAgents     string
	KeyBlocked    string
	KeyDirs       string
	KeyAll        string
	KeyCreate     string
	KeyClose      string

	// previewEnabledRaw, hideCurrentRaw, dirSourcesRaw hold
	// CFG_PREVIEW_ENABLED/HIDE_CURRENT/DIR_SOURCES after the resolution
	// default is applied, but BEFORE the `-eq 1` arithmetic test bash
	// performs at the one specific call site each variable has
	// (sesh-bro:540, :156, :187). They are unexported and parsed lazily,
	// on demand, by PreviewEnabled/HideCurrent/DirSources below — not
	// eagerly here in Load — because that is what bash itself does: three
	// SEPARATE comparisons, in three different subcommands, each capable
	// of crashing independently (BEHAVIOUR.md §9 S1). A `sesh-bro create`
	// run never evaluates CFG_PREVIEW_ENABLED at all, so a garbage
	// SESH_BRO_PREVIEW_ENABLED value must not make `create` fail — an
	// eager Load-time validation covering all three would do exactly that,
	// which is a behaviour change disguised as a config-loading nicety.
	// attentionFirstRaw holds SESH_BRO_ATTENTION_FIRST, new in 0.4.0. It
	// follows the same lazy-parse discipline as the three below for the same
	// reason: only `list` and the picker consult it, so a malformed value
	// must not break `create` or `worktree`.
	attentionFirstRaw string

	previewEnabledRaw string
	hideCurrentRaw    string
	dirSourcesRaw     string
}

// orDefault reproduces bash's `${VAR:-default}`: raw is used verbatim
// unless it is exactly the empty string, in which case def is returned.
// A whitespace-only raw is NOT replaced — bash's `:-` tests nullity, not
// blankness (BEHAVIOUR.md §1.5).
func orDefault(raw, def string) string {
	if raw == "" {
		return def
	}
	return raw
}

// Load resolves every SESH_BRO_* variable via getenv, applying each
// variable's default exactly as sesh-bro:35-46 does. getenv is normally
// os.Getenv (see LoadEnv) — it is a parameter so tests can supply a fake
// environment without mutating process-wide state.
//
// Load never fails: none of the twelve variables can produce an error at
// resolution time in bash either — CFG_X="${SESH_BRO_X:-default}" is a
// plain assignment. The one place bash CAN die on a bad value (S1) happens
// at a specific USE site, later and conditionally; see the accessor methods
// below, not Load.
func Load(getenv func(string) string) Config {
	return Config{
		PreviewWidth:      orDefault(getenv("SESH_BRO_PREVIEW_WIDTH"), "60%"),
		Aliases:           orDefault(getenv("SESH_BRO_ALIASES"), ""),
		Blacklist:         orDefault(getenv("SESH_BRO_BLACKLIST"), ""),
		DefaultFilter:     orDefault(getenv("SESH_BRO_DEFAULT_FILTER"), "all"),
		SortOrder:         orDefault(getenv("SESH_BRO_SORT_ORDER"), ""),
		IconWorkspace:     orDefault(getenv("SESH_BRO_ICON_WORKSPACE"), "◆"),
		IconAgent:         orDefault(getenv("SESH_BRO_ICON_AGENT"), "●"),
		IconDir:           orDefault(getenv("SESH_BRO_ICON_DIR"), "▸"),
		KeyWorkspaces:     orDefault(getenv("SESH_BRO_KEY_WORKSPACES"), "ctrl-w"),
		KeyAgents:         orDefault(getenv("SESH_BRO_KEY_AGENTS"), "ctrl-e"),
		KeyBlocked:        orDefault(getenv("SESH_BRO_KEY_BLOCKED"), "ctrl-b"),
		KeyDirs:           orDefault(getenv("SESH_BRO_KEY_DIRS"), "ctrl-x"),
		KeyAll:            orDefault(getenv("SESH_BRO_KEY_ALL"), "ctrl-o"),
		KeyCreate:         orDefault(getenv("SESH_BRO_KEY_CREATE"), "ctrl-/"),
		KeyClose:          orDefault(getenv("SESH_BRO_KEY_CLOSE"), "alt-x"),
		attentionFirstRaw: orDefault(getenv("SESH_BRO_ATTENTION_FIRST"), "1"),
		previewEnabledRaw: orDefault(getenv("SESH_BRO_PREVIEW_ENABLED"), "1"),
		hideCurrentRaw:    orDefault(getenv("SESH_BRO_HIDE_CURRENT"), "0"),
		dirSourcesRaw:     orDefault(getenv("SESH_BRO_DIR_SOURCES"), "1"),
	}
}

// LoadEnv is Load(os.Getenv) — the entry point real callers use; Load
// itself stays parameterised for tests.
func LoadEnv() Config {
	return Load(os.Getenv)
}

// ErrMalformedBool is the sentinel BoolVarError wraps — test failures
// against it with errors.Is rather than a type assertion when the specific
// variable name doesn't matter.
var ErrMalformedBool = errors.New("sesh-bro: config value is not a valid integer")

// BoolVarError reports that a SESH_BRO_* boolean-flavoured variable held a
// value bash's `[[ $VAR -eq 1 ]]` cannot evaluate (BEHAVIOUR.md §9 S1).
type BoolVarError struct {
	Var   string // e.g. "SESH_BRO_HIDE_CURRENT"
	Value string // the resolved (post-default) value that failed to parse
}

func (e *BoolVarError) Error() string {
	return fmt.Sprintf("sesh-bro: %s=%q is not a valid integer (bash evaluates this with `-eq 1`, an arithmetic test that dies on any non-numeric value — see BEHAVIOUR.md §9 S1)", e.Var, e.Value)
}

func (e *BoolVarError) Unwrap() error { return ErrMalformedBool }

// ParseBoolFlag reproduces bash's `[[ $val -eq 1 ]]` (S1) for one of the
// three boolean-flavoured config variables: PREVIEW_ENABLED, HIDE_CURRENT,
// DIR_SOURCES. name is used only to build a useful error message (e.g.
// "SESH_BRO_HIDE_CURRENT"); val is the already-default-resolved value.
//
// Divergence, stated once here rather than at each call site: bash's
// `-eq` is full shell arithmetic — it accepts C-style hex/octal literals
// and even other variable names as operands, and a value that isn't valid
// arithmetic at all triggers `set -u`'s "unbound variable" abort (exit 1,
// no sesh-bro-authored message, just bash's own diagnostic naming a source
// line). Replicating that whole grammar would mean embedding a bash
// arithmetic evaluator for a config surface whose real values are always
// "0" or "1" (or, per the manifest's `type = "boolean"`, conceivably
// "true"/"false" — see BEHAVIOUR.md §9 S1's "port decision required"
// note). This function accepts base-10 integers only via strconv.ParseInt
// and treats everything else — "true", "false", "yes", hex, whitespace —
// as the same failure bash's unbound-variable abort represents: a non-nil
// *BoolVarError, for the caller to turn into exit 1. What's preserved is
// the OBSERVABLE contract that matters: a non-integer value is a hard
// failure, not a silent default: — not the literal bash diagnostic text,
// which has no Go equivalent (there is no source line to blame).
func ParseBoolFlag(name, val string) (bool, error) {
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return false, &BoolVarError{Var: name, Value: val}
	}
	return n == 1, nil
}

// PreviewEnabled reproduces `[[ $CFG_PREVIEW_ENABLED -eq 1 ]]`
// (sesh-bro:540, cmd_picker only) — true only for the literal integer 1;
// any other integer is false; any non-integer is a *BoolVarError a caller
// should treat as exit 1, exactly where bash would have crashed running
// `picker` (never `list`, `create`, or anything else that doesn't
// reference this variable — see the Config.previewEnabledRaw doc comment).
func (c Config) PreviewEnabled() (bool, error) {
	return ParseBoolFlag("SESH_BRO_PREVIEW_ENABLED", c.previewEnabledRaw)
}

// AttentionFirst reports whether blocked and done agents are hoisted above
// every other block, so the picker opens with the cursor on whoever needs the
// human (BEHAVIOUR.md §10). Default on: the whole point of the 0.4.0 picker is
// that the answer to "does anything want me" is already on screen. Turning it
// off restores the pre-0.4.0 current-workspace-first ordering.
func (c Config) AttentionFirst() (bool, error) {
	return ParseBoolFlag("SESH_BRO_ATTENTION_FIRST", c.attentionFirstRaw)
}

// HideCurrent reproduces `[[ $CFG_HIDE_CURRENT -eq 1 ]]` (sesh-bro:156,
// cmd_list only — it is ORed with the --hide-current flag, BEHAVIOUR.md
// §2.2.3). See PreviewEnabled's doc comment for the crash contract.
func (c Config) HideCurrent() (bool, error) {
	return ParseBoolFlag("SESH_BRO_HIDE_CURRENT", c.hideCurrentRaw)
}

// DirSources reproduces `[[ $CFG_DIR_SOURCES -eq 1 ]]` (sesh-bro:187,
// cmd_list's dir-block gate, ANDed with `want_dir` and `command -v
// zoxide`). See PreviewEnabled's doc comment for the crash contract.
func (c Config) DirSources() (bool, error) {
	return ParseBoolFlag("SESH_BRO_DIR_SOURCES", c.dirSourcesRaw)
}

// Icons builds the render.Icons value FormatRow needs directly from
// IconWorkspace/IconAgent/IconDir. This is pure convenience — a 1:1 field
// copy with no guard or parsing behind it — offered because nothing else
// in this port constructs a render.Icons from config, unlike PreviewWidth,
// Aliases, and Blacklist, which are consumed as raw strings elsewhere by
// design (see the package doc comment).
func (c Config) Icons() render.Icons {
	return render.Icons{Workspace: c.IconWorkspace, Agent: c.IconAgent, Dir: c.IconDir}
}

// Keys builds the picker.KeyBindings BuildArgs needs directly from the
// seven SESH_BRO_KEY_* fields — Icons' exact counterpart for Feature B
// (docs/COMPETITIVE-DEMAND.md #2): a plain field copy, no guard. The
// malformed-value guard (sanitizeKey) lives in internal/picker and runs
// against whatever this returns, the same ownership split Icons' sibling
// PreviewWidth/Aliases already use (see the package doc comment).
func (c Config) Keys() picker.KeyBindings {
	return picker.KeyBindings{
		Workspaces: c.KeyWorkspaces,
		Agents:     c.KeyAgents,
		Blocked:    c.KeyBlocked,
		Dirs:       c.KeyDirs,
		All:        c.KeyAll,
		Create:     c.KeyCreate,
		Close:      c.KeyClose,
	}
}

// defaultFilterFlags maps CFG_DEFAULT_FILTER's four recognised values to
// the flag cmd_picker prepends (sesh-bro:511-516). "all" and anything
// outside this map are handled by DefaultFilterFlag, not listed here.
var defaultFilterFlags = map[string]string{
	"workspaces": "--workspaces",
	"agents":     "--agents",
	"dirs":       "--dirs",
	"blocked":    "--blocked",
}

// DefaultFilterFlag reproduces cmd_picker's default-filter case statement
// (sesh-bro:510-517, BEHAVIOUR.md §2.10 point 3): the single flag to
// prepend when the picker's combined argument list is otherwise completely
// empty. ok is false for "all" (bash's own explicit no-op,
// `$CFG_DEFAULT_FILTER != "all"` gates the whole case) and for any value
// the four-armed case statement does not recognise — including agent
// statuses that are valid ELSEWHERE in this tool, like "working", "done",
// or "idle". That is not an oversight in bash and not one here: the case
// has no default arm, so an unrecognised value falls through to "do
// nothing" exactly like "all" does, and the caller must treat both
// identically (BEHAVIOUR.md §5's config table: "other values are silently
// ignored").
//
// This method exists for whichever package assembles the picker's argument
// list (cmd_picker's own job, sesh-bro:501-517) — internal/picker
// explicitly disclaims owning "CFG_DEFAULT_FILTER substitution" in its own
// package doc comment, so something else must; this is that translation,
// centralised and tested once rather than re-derived at the call site.
func (c Config) DefaultFilterFlag() (flag string, ok bool) {
	if c.DefaultFilter == "all" {
		return "", false
	}
	flag, ok = defaultFilterFlags[c.DefaultFilter]
	return flag, ok
}
