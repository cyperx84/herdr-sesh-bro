# sesh-bro — behaviour specification

This document specifies the observable behaviour of `sesh-bro` **as the bash
implementation actually behaves at commit `0d683a3` (v0.2.0, 658 lines)**. It is the
correctness oracle for the Go rewrite: where this document and any other document
disagree, this one wins; where this document and the bash disagree, the bash wins and
this document is wrong and should be fixed.

The bash is a working tool with muscle memory attached to it. Several things it does are
almost certainly bugs (see [§9 Surprises](#9-surprises-bugs-and-traps)). **They are
specified here as normative behaviour anyway.** A reimplementation that "fixes" them
silently is a different tool. Every one is cross-referenced from the section where an
implementer would otherwise write the obvious, wrong thing.

## 0. Conventions

### Provenance labels

Non-obvious claims carry a provenance tag:

| Tag | Meaning |
|---|---|
| `[live]` | Verified against herdr **0.8.0** running on macOS (Darwin 27), 2026-08-11. |
| `[mock]` | Verified only against `tests/mock-herdr` + `tests/sesh-bro.bats`. Not exercised against a live daemon (focus/create mutate the user's UI, so they were deliberately not run). |
| `[code]` | Read directly off the bash; no execution needed to establish it. |
| `[UNVERIFIED]` | Stated behaviour is inferred and **not** confirmed. Flagged individually; each carries the experiment that would settle it. |

### Notation

- `ESC` is the byte `0x1B`. ANSI sequences are written `ESC[0m` and appear in output as
  literal escape bytes, never as the text `\033`.
- `·` is U+00B7 MIDDLE DOT (`0xC2 0xB7`), used as a separator in several strings. `—` in
  worktree labels is U+2014 EM DASH.
- `TAB` is `0x09`. Row fields are tab-separated; **no field is ever quoted or escaped**.
- Quoted bash is verbatim from `sesh-bro` with the source line number given.

### Environment reference

Tool versions on the reference machine, where behaviour is version-sensitive `[live]`:
`fzf 0.74.2`, `jq 1.8.2`, `git 2.55.0`, `zoxide 0.10.0`, `gh 2.97.0`, `eza 0.23.5`,
`bat 0.26.1`, `herdr 0.8.0` (socket protocol 19).

---

## 1. Process-wide startup

Everything in this section runs on **every** invocation, before any subcommand dispatch,
under `set -euo pipefail` (line 4).

### 1.1 `HERDR` binary resolution

```bash
HERDR="${HERDR_BIN_PATH:-herdr}"
```

`$HERDR_BIN_PATH` if set and non-empty, else the literal string `herdr` (resolved through
`PATH` at spawn time). This is the sole hook the test suite uses to point at its mock.

### 1.2 `SELF` — resolving the script's own real path

```bash
if SELF="$(readlink -f "$0" 2>/dev/null)" || SELF="$(realpath "$0" 2>/dev/null)"; then
  :
elif [[ -f $0 ]]; then
  SELF="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"
else
  SELF="$(command -v sesh-bro)"
fi
SELF="${SELF:-}"
```

Four-tier fallback, in order: `readlink -f` → `realpath` → `$(cd dirname && pwd)/basename`
→ `command -v sesh-bro`. Symlinks **are** followed by the first two tiers; this is why
`herdr plugin install`, which symlinks the script onto `PATH`, still finds the manifest.
`[mock: tests/sesh-bro.bats "version resolves through a symlink"]`

`SELF` matters for two reasons and only two: locating `herdr-plugin.toml` (§1.3) and
re-invoking the script from inside fzf (§3).

Note the tier-4 branch is the last statement of the `if`; under `set -e` a failing
`command -v` aborts the script with exit 1 and no message. Reachable only when `$0` is
neither resolvable nor an existing file. `[code]`

### 1.3 `VERSION` — read from the manifest, never hardcoded

```bash
SELF_DIR="$(dirname "$SELF")"
VERSION="$(sed -n 's/^version = "\(.*\)"/\1/p' "$SELF_DIR/herdr-plugin.toml" 2>/dev/null | head -1)"
VERSION="${VERSION:-0.1.0}"
```

Anchored at `^version = "` so `min_herdr_version = "0.8.0"` on line 4 of the manifest does
not match. First match wins. Falls back to `0.1.0` if the manifest is missing or
unparseable. Current value: **`0.2.0`**. `[live]`

The Go port must keep this property — the manifest is the single source of truth and the
bats suite asserts the two agree. Reading the manifest at runtime (rather than baking the
version in at build time) is the behaviour; a build-time constant would drift the moment
someone edits the manifest without rebuilding.

### 1.4 `SELF_Q` — shell-safe form of `SELF` for embedding in fzf strings

```bash
SELF_Q="'${SELF//\'/\'\\\'\'}'"
```

Standard POSIX single-quote escaping: wrap in `'…'`, replace each embedded `'` with
`'\''`. Used **only** inside `--preview=` and `--bind=` argument strings (§3), which fzf
hands to `$SHELL -c`. This is what keeps plugin directories containing spaces or quotes
from breaking the picker.

### 1.5 Config resolution

Twelve `SESH_BRO_*` variables, each with a hardcoded default (lines 35–46). Full table in
[§5](#5-sesh_bro_-configuration). Resolution is plain `${VAR:-default}` — an **empty**
value falls back to the default, a whitespace-only value does not.

### 1.6 Colours

```bash
C_RESET=$'\033[0m'
C_DIM=$'\033[2m'
status_color() {
  case "$1" in
    blocked) printf '\033[31m' ;;
    working) printf '\033[33m' ;;
    idle)    printf '\033[32m' ;;
    done)    printf '\033[34m' ;;
    dir)     printf '\033[36m' ;;
    *)       printf '\033[90m' ;;
  esac
}
```

| Input | Sequence | Colour |
|---|---|---|
| `blocked` | `ESC[31m` | red |
| `working` | `ESC[33m` | yellow |
| `idle` | `ESC[32m` | green |
| `done` | `ESC[34m` | blue |
| `dir` | `ESC[36m` | cyan |
| anything else (incl. `unknown`, `-`, empty) | `ESC[90m` | bright black |

Colours are emitted **unconditionally** — there is no TTY check, no `NO_COLOR` handling,
no `--color` flag. `sesh-bro list > file` writes escape bytes into the file. This is
required: fzf reads the list through a pipe and is given `--ansi`.

`status_color` prints with no trailing newline; it is always used inside `$(…)`.

### 1.7 `current_workspace_id`

```bash
current_workspace_id() {
  if [[ -n ${HERDR_WORKSPACE_ID:-} ]]; then
    printf '%s' "$HERDR_WORKSPACE_ID"
  elif [[ -n ${HERDR_PLUGIN_CONTEXT_JSON:-} ]]; then
    printf '%s' "$HERDR_PLUGIN_CONTEXT_JSON" | jq -r '.. | .workspace_id? // empty' 2>/dev/null | head -1
  fi
}
```

1. `$HERDR_WORKSPACE_ID` verbatim if non-empty. herdr injects this into every pane it
   spawns `[live: confirmed present in a live pane alongside HERDR_PANE_ID, HERDR_TAB_ID,
   HERDR_SOCKET_PATH, HERDR_ENV]`.
2. Otherwise, a **recursive-descent** search of `$HERDR_PLUGIN_CONTEXT_JSON` for the first
   `workspace_id` at any depth (`..` then `.workspace_id?`), first line only. Errors and
   non-object nodes are swallowed.
3. Otherwise the empty string.

An empty result is meaningful and load-bearing: `current == ""` disables current-first
priority sorting and disables `--hide-current` filtering entirely (§2.2).

---

## 2. Subcommands

### 2.0 Dispatch

```bash
main() {
  local cmd="${1:-picker}"
  [[ $# -gt 0 ]] && shift
  case "$cmd" in
    picker) … list) … connect) … create) … preview) … open) …
    startup) … last) … root) … worktree) …
    -h|--help|help) usage ;;
    -v|--version)  echo "sesh-bro $VERSION" ;;
    *) echo "sesh-bro: unknown command $cmd" >&2; usage >&2; exit 2 ;;
  esac
}
```

- No arguments → `picker`.
- `-h` / `--help` / `help` → `usage()` on **stdout**, exit 0.
- `-v` / `--version` → `sesh-bro 0.2.0` on stdout, exit 0.
- Unknown command → `sesh-bro: unknown command <cmd>` on **stderr**, then the entire usage
  text on **stderr**, exit **2**.
- Commands are matched exactly; there is no prefix matching and no global flag parsing
  before the command word.

`usage()` output, verbatim (lines 599–635, `$VERSION` interpolated on the first line):

```
sesh-bro 0.2.0 — sesh-style fuzzy session picker for Herdr

usage: sesh-bro <command> [flags]

commands:
  picker [flags]     open the fzf picker (default)
  list   [flags]     print picker candidates (type, target, display)
  connect TYPE TARGET
                     focus a workspace/agent, or create a workspace for a dir
  create [PATH]      create a workspace for a directory (default: current dir)
  preview TYPE TARGET
                     render the preview used by the picker
  open   [flags]     open the picker popup through the Herdr plugin API
  startup            validate deps + clear stale cache (manifest startup hook)
  last               focus the previously-focused workspace
  root               focus/create the workspace for the current git root
  worktree [URL]     create/focus the workspace for a GitHub issue/PR
  -h, --help         show this help
  -v, --version      print the version

flags:
  --workspaces       only Herdr workspaces
  --agents           only agents (across all workspaces)
  --dirs             only zoxide directories
  --blocked          only blocked agents (needs attention)
  --working          only working agents
  --done             only done agents
  --idle             only idle agents
  --hide-current     hide the current workspace and its agents
  --json             machine-readable list output

environment:
  HERDR_BIN_PATH     the herdr binary to use (default: herdr on PATH)
  SESH_BRO_*         config overrides (preview_width, hide_current, dir_sources,
                     cache_ttl, default_filter, blacklist, icons, aliases...)
```

---

### 2.1 `startup`

Declared in the manifest as `[[startup]] command = ["bash", "sesh-bro", "startup"]`.

```bash
cmd_startup() {
  check_deps || exit 1
  rm -f "${SESH_BRO_PANE_CACHE:-${TMPDIR:-/tmp}/sesh-bro-${UID:-$(id -u)}-panes.json}" 2>/dev/null || true
  echo "sesh-bro: deps ok"
}
```

| | |
|---|---|
| Arguments | none accepted; any are silently ignored (passed to the function, never read) |
| stdout on success | `sesh-bro: deps ok` |
| stderr on failure | one of the three `check_deps` messages (§7.1) |
| exit | 0 on success, 1 on any dep failure |

Deletes the pane cache file unconditionally (failure ignored). herdr logs a startup-hook
failure but does not refuse to start the server.

---

### 2.2 `list`

The core of the tool. Emits one row per candidate on stdout.

#### 2.2.1 Flags

Parsed left to right (lines 131–143):

| Flag | Effect |
|---|---|
| `--workspaces` | `want_ws=1`, `source_flag=1` |
| `--agents` | `want_ag=1`, `source_flag=1` |
| `--dirs` | `want_dir=1`, `source_flag=1` |
| `--blocked` `--working` `--done` `--idle` | appends the bare word to `statuses`, **and sets `want_ag=1`, `want_ws=0`, `want_dir=0`**, `source_flag=1` |
| `--hide-current` | `hide_current=1` (does **not** set `source_flag`) |
| `--json` | `as_json=1` (does **not** set `source_flag`) |
| anything else | `sesh-bro list: unknown flag <arg>` on stderr, **exit 2** |

If no source flag was seen, all three sources are enabled: `want_ws=want_ag=want_dir=1`.

**Flag order is significant** — the status flags clear `want_ws`/`want_dir` at the moment
they are parsed:

- `list --dirs --blocked` → agents only (the `--blocked` clears `want_dir`)
- `list --blocked --dirs` → blocked agents **and** dirs

See [S3](#s3-status-flags-clear-earlier-source-flags-order-dependent).

Multiple status flags accumulate: `--blocked --working` yields `statuses="blocked working"`
and matches either.

#### 2.2.2 Dependency gate

```bash
require_herdr || exit 1
require_jq || exit 1
herdr_ok || { echo "sesh-bro: herdr daemon is not responding" >&2; exit 1; }
```

Note this is **not** `check_deps` — the daemon-down message here has no
`(is 'herdr' running?)` suffix. Both texts must be preserved. `[code]`

#### 2.2.3 Current workspace / hide

```bash
current="$(current_workspace_id)"
[[ $hide_current -eq 1 || $CFG_HIDE_CURRENT -eq 1 ]] && hide_val="$current"
```

`current` is resolved on **every** `list`, including `--dirs`-only. `hide_val` is the
workspace id to drop, or `""` for "drop nothing". If `current` is empty, `--hide-current`
is a no-op — the filter is `select($hide == "" or …)`.

`$CFG_HIDE_CURRENT -eq 1` is an **arithmetic** comparison. See
[S1](#s1-boolean-config-values-crash-the-script).

#### 2.2.4 Workspace block

```bash
ws_block="$("$HERDR" workspace list | jq -r --arg cur "$current" --arg hide "$hide_val" '
  .result.workspaces // []
  | map(select($hide == "" or .workspace_id != $hide))
  | map(.priority = (if $cur != "" and .workspace_id == $cur then 0 else 1 end))
  | sort_by(.priority, .number)
  | .[]
  | ["workspace", .workspace_id, (.agent_status // "unknown"),
     (.label // .workspace_id),
     ((.pane_count | tostring) + "p/" + (.tab_count | tostring) + "t"
      + (if .focused then " · current" else "" end))]
  | @tsv')"
```

Produces 5 TAB-separated fields: `workspace`, `<workspace_id>`, `<agent_status|unknown>`,
`<label|workspace_id>`, `<N>p/<M>t[ · current]`.

Sort: `priority` ascending (0 = the current workspace, 1 = everything else), then `number`
ascending. `jq`'s `sort_by` is **stable** `[live: verified — equal keys preserve input
order]`, so a Go implementation must use a stable sort with `workspace.list` response order
as the tiebreak.

`.pane_count`/`.tab_count` are stringified with jq `tostring`; they are unsigned integers
in the protocol, so plain decimal.

#### 2.2.5 Agent block

```bash
ag_block="$("$HERDR" agent list | jq -r --arg cur "$current" --arg hide "$hide_val" --arg st "$statuses" '
  def rank: {blocked: 0, working: 1, done: 2, idle: 3}[.] // 4;
  .result.agents // []
  | map(.agent_status = (.agent_status // "unknown"))
  | map(select($hide == "" or .workspace_id != $hide))
  | map(. as $agent | select($st == "" or ((" " + $st + " ") | test(" " + $agent.agent_status + " "))))
  | map(.priority = (if $cur != "" and .workspace_id == $cur then 0 else 1 end))
  | sort_by(.priority, (.agent_status | rank), (.name // ""))
  | .[]
  | ["agent", (.name // .pane_id), .agent_status,
     (.name // .agent // .pane_id),
     ((.agent // "?") + " · " + (.terminal_title_stripped // .cwd // ""))]
  | @tsv')"
```

Fields: `agent`, **target = `name` or, if absent, `pane_id`**, `<agent_status>`,
**label = `name` or `agent` or `pane_id`**, `<agent|?> · <terminal_title_stripped|cwd|"">`.

Note target and label differ: the target never falls back to the agent *kind*. Unnamed
agents therefore have target `w49:p1` and label `claude`. This is common in practice —
most live agents carry no `name` `[live]`.

Status ranking: `blocked`(0) → `working`(1) → `done`(2) → `idle`(3) → everything else
including `unknown`(4). Sort keys in order: `priority`, status rank, `name` (null → `""`,
so unnamed agents sort first within a rank). Stable.

Status filter: the accumulated `statuses` string is wrapped in spaces and used as a
**regex** via `test`. Since every status is a bare word this behaves as set membership.
`[code]`

jq's `//` is false-or-null coalescing, so an **empty-string** `terminal_title_stripped`
is kept (renders `claude · ` with a trailing space), not replaced by `cwd`. The Go port
must distinguish absent-or-null from empty. In practice herdr omits the key entirely when
there is no title `[live]`, so mapping absent → `""` and testing `!= ""` reproduces it —
but only because herdr never sends `""`. `[UNVERIFIED that herdr never sends ""]`

#### 2.2.6 Directory block

Gated on all three of: `want_dir=1`, `$CFG_DIR_SOURCES -eq 1`, and `zoxide` on `PATH`
(missing zoxide silently yields no dirs — no warning).

```bash
known="$(pane_list 2>/dev/null | jq -r '.result.panes // [] | .[].cwd // empty' | sort -u || true)"
dz="$(zoxide query --list 2>/dev/null || true)"
while IFS= read -r path; do
  [[ -z $path ]] && continue
  if [[ -n $CFG_BLACKLIST ]]; then
    local bl
    for bl in ${CFG_BLACKLIST//:/$'\n'}; do
      [[ $path == $bl ]] && continue 2
    done
  fi
  if [[ -n $known ]] && printf '%s\n' "$known" | grep -Fxq "$path"; then
    continue
  fi
  base="${path##*/}"; [[ -z $base ]] && base="$path"
  [[ -n $dir_block ]] && dir_block+=$'\n'
  dir_block+="dir"$'\t'"$path"$'\t'"-"$'\t'"$base"$'\t'"$path"
done <<< "$dz"
```

Per zoxide path, in `zoxide query --list` order (frecency, descending) `[live]`:

1. Skip empty lines.
2. **Blacklist**: `$CFG_BLACKLIST` colons are turned into newlines, then the result is
   left **unquoted** in the `for`, so it is subject to IFS word-splitting *and* pathname
   expansion. Each token is matched against the path with bash `[[ == ]]` **glob** matching
   (unquoted RHS, deliberately — `# shellcheck disable=SC2053`). A match skips the path
   (`continue 2` exits the inner `for` and continues the outer `while`). See
   [S6](#s6-blacklist-tokens-are-word-split-and-glob-expanded).
3. **Dedup**: skip if the path appears verbatim (`grep -Fxq`, exact whole-line, literal) in
   the set of `cwd`s from `pane list`. Comparison is byte-exact — no symlink resolution, no
   trailing-slash normalisation. If `pane list` fails, `known` is empty and dedup is
   skipped entirely rather than failing the whole listing.
4. Fields: `dir`, `<path>`, `-`, `<basename>`, `<path>`. Basename is `${path##*/}`, falling
   back to the whole path when that is empty (i.e. for `/`).

Directories are **not** checked for existence here; a stale zoxide entry is listed and only
fails at `connect`/`preview` time.

#### 2.2.7 Block assembly and sort order

```bash
if [[ -n $CFG_SORT_ORDER ]]; then
  IFS=',' read -r -a parts <<< "$CFG_SORT_ORDER"
  for part in "${parts[@]+"${parts[@]}"}"; do
    case "$part" in
      workspaces) [[ -n $ws_block ]] && raw+="${ws_block}"$'\n' ;;
      agents)     [[ -n $ag_block ]] && raw+="${ag_block}"$'\n' ;;
      dirs)       [[ -n $dir_block ]] && raw+="${dir_block}"$'\n' ;;
    esac
  done
else
  [[ -n $ws_block ]] && raw+="${ws_block}"$'\n'
  [[ -n $ag_block ]] && raw+="${ag_block}"$'\n'
  [[ -n $dir_block ]] && raw+="${dir_block}"$'\n'
fi
raw="${raw%$'\n'}"
```

Default order: workspaces, agents, dirs. `$CFG_SORT_ORDER` is a comma-separated list;
unrecognised tokens are silently ignored and **omitted blocks are dropped entirely** — see
[S5](#s5-sort_order-drops-unlisted-blocks). A token may be repeated, which duplicates the
block.

#### 2.2.8 Git enrichment

Runs only when **all** of: `as_json=0`, `raw` non-empty, `git` on PATH, `jq` on PATH.

```bash
ws_cwd="$(pane_list 2>/dev/null | jq -r '.result.panes // [] | group_by(.workspace_id) | map({ws: .[0].workspace_id, cwd: .[0].cwd}) | map("\(.ws)\t\(.cwd)") | .[]' 2>/dev/null || true)"
while IFS=$'\t' read -r w targetcwd; do
  [[ -z $w || -z $targetcwd ]] && continue
  out="$(git -C "$targetcwd" --no-optional-locks status --porcelain --branch 2>/dev/null | head -1 || true)"
  branch="$(printf '%s\n' "$out" | sed -n 's/^## \([^.]*\).*/\1/p' | head -1)"
  dirty=""
  git -C "$targetcwd" --no-optional-locks status --porcelain 2>/dev/null | grep -q . && dirty="*"
  [[ -n $branch ]] && gitmap+="$w"$'\t'"$branch$dirty"$'\n'
done <<< "$ws_cwd"
```

- One entry per **distinct workspace id** in `pane list`, using the cwd of that group's
  **first** pane — `group_by` sorts by `workspace_id`, so this is the lowest-sorting pane
  id in the workspace, **not** the focused pane. `[code]`
- Two `git` invocations per workspace: `status --porcelain --branch | head -1` for the
  branch header, then `status --porcelain` again for dirtiness. (The first call's output
  already contains the dirty information; the code does not use it. Harmless, but a Go port
  reproducing the *observable* result only needs the branch name and a boolean.)
- `--no-optional-locks` on both, to avoid fighting an editor holding the index lock.
- Branch extraction: `sed -n 's/^## \([^.]*\).*/\1/p'` on `## main...origin/main` →
  `main`. Note `[^.]*` stops at the **first `.`**, so a branch named `release/v1.2` yields
  `release/v1` — see [S7](#s7-branch-names-are-truncated-at-the-first-dot).
- Detached HEAD produces `## HEAD (no branch)` → captured as `HEAD ` … actually
  `HEAD (no branch)` contains no `.`, so the whole string is captured. `[UNVERIFIED — no
  detached-HEAD workspace was available to probe]`
- Dirty marker is a literal `*` appended to the branch name.
- Workspaces whose cwd is not a git repo produce no entry and get no suffix.

Enrichment is applied to `workspace` rows only:

```bash
detail="$detail ${C_DIM}[${C_RESET}${gitline}${C_DIM}]${C_RESET}"
```

i.e. a space, then dim `[`, **reset**, the branch (uncoloured), dim `]`, reset. Lookup is
`awk -F'\t' -v w="$target" '$1==w{print $2; exit}'` over the accumulated map.

The enrichment loop re-splits every row on TAB into exactly 5 fields and re-joins them,
so a Go port operating on structs rather than text is equivalent here.

#### 2.2.9 `--json` output

```bash
printf '%s\n' "$raw" | jq -R 'select(length > 0) | split("\t")
  | {type: .[0], target: .[1], status: .[2], label: .[3], detail: .[4]}'
```

**Pretty-printed, concatenated JSON objects — not an array, not JSON Lines.** jq's default
output mode is used (no `-c`), so each object spans 7 lines with two-space indentation and
keys in the fixed order `type, target, status, label, detail`. Verbatim `[live]`:

```json
{
  "type": "workspace",
  "target": "w49",
  "status": "working",
  "label": "ORG",
  "detail": "2p/1t · current"
}
{
  "type": "workspace",
  "target": "w3R",
  "status": "unknown",
  "label": "DOTFILES",
  "detail": "3p/1t"
}
```

See [S8](#s8---json-is-pretty-printed-concatenated-objects). `--json` **skips git
enrichment** (the `[[ $as_json -eq 0 ]]` guard), so `detail` never carries a `[branch]`
suffix, and no ANSI colour ever appears in JSON output. Empty result set → no output,
exit 0.

`--json` is documented as `list`-only. `open` **rejects** it (exit 2). `picker` validates
nothing (§2.10) and forwards it to the `list` subprocess, so `sesh-bro picker --json`
succeeds and feeds fzf pretty-printed JSON as rows — degenerate, but permitted, and the
port must permit it too.

#### 2.2.10 Human row rendering

```bash
while IFS=$'\t' read -r type target status label detail; do
  [[ -z ${type:-} ]] && continue
  color="$(status_color "$status")"
  case "$type" in
    workspace) icon="${color}${CFG_ICON_WORKSPACE}${C_RESET}" ;;
    agent)     icon="${color}${CFG_ICON_AGENT}${C_RESET}" ;;
    dir)       icon="$(status_color dir)${CFG_ICON_DIR}${C_RESET}" ;;
  esac
  printf '%s\t%s\t%s %s %s\n' "$type" "$target" "$icon" "$label" "$C_DIM$detail$C_RESET"
done <<< "$raw"
```

**Three** output fields (§4). Note `dir` rows ignore the row's `status` field (`-`) and
hardcode `status_color dir`. `icon` is not reset between iterations, so a row with an
unrecognised `type` would reuse the previous row's icon — unreachable, since only the three
known types are ever generated. `[code]`

#### 2.2.11 `list` exit codes

| Condition | Exit |
|---|---|
| success (including zero rows) | 0 |
| unknown flag | 2 |
| `herdr` binary not found | 1 |
| `jq` not found | 1 |
| daemon not responding | 1 |
| `herdr workspace list` / `agent list` fails mid-run | 1 (via `set -e` on the assignment; stderr is herdr's) |

---

### 2.3 `connect TYPE TARGET`

```bash
cmd_connect() {
  local type="$1" target="$2"
```

Both arguments are **required and unchecked**. `sesh-bro connect` with fewer than two
arguments dies under `set -u` with bash's own message
(`sesh-bro: line 288: $1: unbound variable`) and **exit 1** — not a usage error, not exit 2.
See [S9](#s9-missing-arguments-die-via-set--u-not-a-usage-message).

| `TYPE` | Behaviour |
|---|---|
| `workspace` | `herdr workspace focus "$target"` with both streams to `/dev/null`. On non-zero exit: `sesh-bro: failed to focus workspace <target>` on stderr, **return 1**. |
| `agent` | `herdr agent focus "$target"` likewise; failure → `sesh-bro: failed to focus agent <target>`, return 1. |
| `dir` | See below. |
| anything else | `sesh-bro connect: unknown type <type>` on stderr, **exit 2** (exits the process, does not return — matters when called from the picker). |

herdr's own error output is **always discarded**; only sesh-bro's message reaches the user.
`[mock]`

#### `connect dir`

```bash
ws="$(pane_list 2>/dev/null | jq -r --arg p "$target" \
  '.result.panes // [] | (map(select(.cwd == $p and .focused)) | .[0].workspace_id) // (map(select(.cwd == $p)) | .[0].workspace_id) // empty')"
```

1. Look for a pane whose `cwd` is byte-identical to the target, preferring a **focused**
   one, else the first match in `pane list` order. If found → `herdr workspace focus <ws>`
   (failure → `sesh-bro: failed to focus workspace <ws>`, return 1).
2. Otherwise: if the target is not an existing directory →
   `sesh-bro: not a directory: <target>` on stderr, **return 1**. `[mock]`
3. `zoxide add "$target"` if zoxide exists; failure ignored.
4. `label = ${target##*/}`, falling back to the whole path if empty.
5. `herdr workspace create --cwd "$target" --label "$label" --focus`, output discarded.
   Failure → `sesh-bro: failed to create workspace for <target>`, return 1.

Success prints **nothing**. Exit 0. Paths with spaces survive intact — every expansion is
quoted. `[mock: "connect dir creates a workspace for an unknown cwd, spaces preserved"]`

---

### 2.4 `create [PATH]`

Path resolution, in order (lines 325–336):

1. `$1` if given and non-empty.
2. Else, if `$HERDR_PLUGIN_CONTEXT_JSON` is set:
   `jq -r '.focused_pane_cwd // .workspace_cwd // empty' | head -1`. Note this is a
   **top-level** lookup, unlike `current_workspace_id`'s recursive descent.
3. Else, if **stdin is a tty**: `read -r -p "Path: " -e -i "$PWD" path` — a readline prompt
   pre-filled with `$PWD`. If `read` fails, `path="$PWD"`.
4. Else `$PWD`.
5. If still empty: `sesh-bro create: no path` on stderr, exit 1. (Unreachable — step 4
   always yields a non-empty `$PWD`.)

Tilde expansion (lines 339–347), applied only to the resolved path:

- `~/…` → `$HOME/…`
- `~user…` → the user's home from `getent passwd <user> | cut -d: -f6`, falling back to
  `eval "printf '%s' \"\$$user\""` (i.e. the value of a shell variable named `user`).
  `getent` does not exist on macOS, so the second branch is what runs there — and it
  resolves to empty for any real username, leaving the path untouched.
  See [S10](#s10-user-expansion-is-broken-on-macos).
  Note `path="$home/${path#*/}"` requires a `/` in the path; `~alice` with no slash
  produces `$home/~alice`. `[code]`
- A bare `~` matches the `~*` branch.

Then:

- Not a directory → `sesh-bro create: not a directory: <path>` on stderr, **exit 1**.
  `[mock]`
- `zoxide add "$path"` if available, failure ignored.
- `label = ${path##*/}` (whole path if empty).
- `herdr workspace create --cwd "$path" --label "$label" --focus >/dev/null` — note
  **stdout only** is discarded here; herdr's stderr passes through. Failure →
  `sesh-bro: failed to create workspace for <path>` on stderr, **exit 1**.

Success prints nothing on stdout, exit 0.

`create` is invoked with no arguments by the picker's `ctrl-/` bind through
`execute-silent`, which is precisely why the tty branch is guarded — see
[S11](#s11-create-under-execute-silent-silently-uses-pwd).

---

### 2.5 `last`

```bash
current="$(current_workspace_id)"
prev="$("$HERDR" workspace list | jq -r --arg cur "$current" '
  .result.workspaces // []
  | map(select(.workspace_id != $cur))
  | sort_by((.focused | not), (-(.number // 0))) | .[0].workspace_id // empty')"
[[ -n $prev ]] || { echo "sesh-bro: no previous workspace" >&2; exit 1; }
"$HERDR" workspace focus "$prev" >/dev/null
```

Drop the current workspace, then sort focused-first (`false` sorts before `true` in jq),
then by **descending** `number`, and take the first. Prints nothing on success; exit code
is `herdr workspace focus`'s (its stdout is discarded, stderr passes through). No workspace
left → `sesh-bro: no previous workspace`, exit 1.

**When `HERDR_WORKSPACE_ID` is unset this is a no-op** — see
[S2](#s2-last-refocuses-the-current-workspace-when-the-context-is-unknown).

---

### 2.6 `root`

```bash
root="$(git -C "$PWD" rev-parse --show-toplevel 2>/dev/null || true)"
[[ -n $root ]] || { echo "sesh-bro: not in a git repo" >&2; exit 1; }
ws="$(pane_list | jq -r --arg p "$root" \
  '.result.panes // [] | (map(select(.cwd == $p)) | .[0].workspace_id) // empty')"
if [[ -n $ws ]]; then
  "$HERDR" workspace focus "$ws" >/dev/null
else
  local label="${root##*/}"; [[ -z $label ]] && label="$root"
  "$HERDR" workspace create --cwd "$root" --label "$label" --focus >/dev/null
fi
```

Unlike `connect dir`, there is **no focused-pane preference** — the first pane matching the
git root wins. `pane_list` here is *not* `2>/dev/null`-guarded, so a cache/daemon error
message leaks to stderr; jq then reads empty input, yields nothing, and the create branch
runs. Exit code is the focus/create call's. Not in a repo → `sesh-bro: not in a git repo`,
exit 1.

---

### 2.7 `worktree [URL]`

Invoked by the `github-worktree` link handler (which sets `HERDR_PLUGIN_CLICKED_URL`) and
by the `worktree` palette action, and runnable directly.

```bash
local url="${HERDR_PLUGIN_CLICKED_URL:-${1:-}}"
```

**The environment variable wins over the argument.** Empty → usage on stderr:
`sesh-bro worktree: no URL (usage: sesh-bro worktree <github-url>)`, exit 1.

Two accepted forms (bash `=~`, both **unanchored at the end**):

| Pattern | Captures |
|---|---|
| `^https://github\.com/([^/]+)/([^/]+)/(issues\|pull)/([0-9]+)` | owner=1, repo=2, num=**4** |
| `^([^/]+)/([^/]+)#([0-9]+)` | owner=1, repo=2, num=3 |

Neither is anchored at the end, so `…/issues/409/comments` and `owner/repo#409x` are
accepted. Unmatched → `sesh-bro worktree: unrecognized URL <url>` on stderr, exit 1.
Note the manifest's `[[link_handlers]] pattern` **is** fully anchored
(`^https://github\.com/[^/]+/[^/]+/(issues|pull)/[0-9]+$`), so herdr only fires the handler
for exact URLs — but a manual invocation is looser.

**Title resolution** (24-hour cache):

- Cache dir: `${HERDR_PLUGIN_STATE_DIR:-${TMPDIR:-/tmp}/sesh-bro-${UID:-$(id -u)}}`,
  `mkdir -p`'d.
- Cache file: `<dir>/gh-title-<owner>-<repo>-<num>`.
- Hit when the file is non-empty **and** `find "$cache_file" -mmin -1440` matches (correct
  negative sign here — "modified less than 1440 minutes ago"). Contrast the pane cache,
  [S4](#s4-the-pane-cache-ttl-test-has-no-minus-sign-and-almost-never-hits).
- Miss and `gh` available: `gh issue view "$num" --repo "$owner/$repo" --json title -q .title`,
  falling back to `gh pr view …` with the same shape, falling back to empty. Note `issue
  view` is tried first even for `/pull/` URLs. A non-empty title is written to the cache
  with `printf '%s'` — **no trailing newline**, so a Go port writing one would produce a
  title with a stray `\n` on the next read (`$(cat …)` strips it, so the difference is
  invisible in bash, but not in Go).
- `gh` missing → empty title, no error.

**Existing-workspace check:**

```bash
existing="$("$HERDR" workspace list | jq -r --arg n "$num" '
  .result.workspaces // [] | map(select(.label | test("(^|[^0-9])" + $n + "([^0-9]|$)"))) | .[0].workspace_id // empty')"
```

Matches **any** workspace whose label contains the number as a standalone integer —
repo-agnostic. If found: `herdr workspace focus <id>` (stdout discarded), then
`sesh-bro: focused workspace <id>` on **stdout**, `exit 0`.

**Otherwise create:**

- `label = "<num>"`, or `"<num> — <title>"` (space, U+2014 EM DASH, space) when a title was
  resolved.
- `cwd = $HOME/github/<repo>` if `$HOME/github` **exists as a directory**, else `$HOME`.
  Note only the *parent* is checked — `$HOME/github/<repo>` may not exist, and herdr is
  handed a non-existent cwd. `[code]`
- `herdr workspace create --cwd "$cwd" --label "$label" --focus >/dev/null`; failure →
  `sesh-bro: failed to create worktree workspace for <owner>/<repo>#<num>`, exit 1.
- Success: `sesh-bro: created workspace for <owner>/<repo>#<num>` on **stdout**, exit 0.

Despite the name, no `git worktree` is ever created — this is an issue-labelled Herdr
workspace. The README says so explicitly.

---

### 2.8 `preview TYPE TARGET`

Both arguments required; missing ones die under `set -u` with `sesh-bro: line 441: $1:
unbound variable` and exit 1, exactly as in `connect` `[live]`.
Always exits **0** for the three known types, including when the target does not exist —
fzf must not see a preview failure.

Every header uses a **hardcoded glyph**, ignoring `SESH_BRO_ICON_*`. See
[S12](#s12-preview-headers-ignore-the-icon-config).

#### `preview workspace <id>`

```bash
ws="$("$HERDR" workspace get "$target" 2>/dev/null || true)"
if [[ -z $ws || $ws == *'"error"'* ]]; then
  printf '%s◆ %s%s  %s(not found)%s\n' "$(status_color unknown)" "$target" "$C_RESET" "$C_DIM" "$C_RESET"
  return 0
fi
printf '%s◆ %s%s  %s(%s)%s\n\n' \
  "$(status_color "…agent_status // \"unknown\"")" "…label // \"\"" "$C_RESET" \
  "$C_DIM" "$target" "$C_RESET"
pane="$("$HERDR" pane list --workspace "$target" | jq -r \
  '.result.panes // [] | (map(select(.focused)) | .[0].pane_id) // .[0].pane_id // empty')"
[[ -n $pane ]] && "$HERDR" pane read "$pane" --source visible --ansi 2>/dev/null
```

- Not-found line: `ESC[90m◆ <target>ESC[0m  ESC[2m(not found)ESC[0m` + newline, exit 0.
- Header: `<status-colour>◆ <label>ESC[0m  ESC[2m(<workspace_id>)ESC[0m` followed by a
  **blank line** (`\n\n`).
- Body: the focused pane's visible screen, else the first pane's, with ANSI preserved. No
  pane → nothing after the header.
- `herdr pane read` writes the terminal text to stdout **raw, not JSON, and with no
  trailing newline appended** `[live: verified byte-exactly with xxd]`.
- Note `pane list --workspace` is a **fresh, uncached** call — the pane cache is only used
  by `pane_list()`, which this path does not use.
- The `*'"error"'*` substring test is dead code in practice: `herdr workspace get` writes
  its error JSON to **stderr** and exits 1, so `$ws` is simply empty. `[live]`

#### `preview agent <target>`

```bash
ag="$("$HERDR" agent get "$target" 2>/dev/null || true)"
# empty or contains "error" → ESC[90m● <target>ESC[0m  ESC[2m(not found)ESC[0m
printf '%s● %s%s  %s%s · %s%s\n\n' \
  "<colour of agent_status>" "<name // agent // \"\">" "$C_RESET" \
  "$C_DIM" "<agent_status // \"unknown\">" "<cwd // \"\">" "$C_RESET"
"$HERDR" agent read "$target" --source visible --ansi 2>/dev/null
```

Header renders as `<colour>● <name>ESC[0m  ESC[2m<status> · <cwd>ESC[0m` + blank line, then
the agent pane's visible screen. Four separate `jq` invocations over the same JSON blob
(status twice, name, cwd) — the Go port collapses these to one decode.

`agent read` failing is swallowed (`2>/dev/null`), and its exit status is the last command
in the branch, so a failure would propagate — except `set -e` is in force and the `case`
branch is the function's last statement, meaning a non-zero `agent read` **does** exit the
process non-zero. **[UNVERIFIED]** — not reproduced; `agent get` succeeding while
`agent read` fails is a narrow race.

#### `preview dir <path>`

```bash
printf '\033[36m▸ %s%s\n\n' "$target" "$C_RESET"
```

Header: `ESC[36m▸ <path>ESC[0m` + blank line. Note the `%s` for `C_RESET` comes **after**
the path and there is no space — output is `ESC[36m▸ /pathESC[0m\n\n`.

- Not a directory → `ESC[2m(directory not found)ESC[0m`, return 0.
- Listing: `eza -la --color=always --group-directories-first --no-user --time-style=relative <path>`
  if `eza` exists, else `ls -la <path>`. Errors from either are suppressed.
- README: `find "$target" -maxdepth 1 -iname 'readme*' -type f | head -1` — case-insensitive
  glob, first match in filesystem order. If found:
  - separator: `\n` + `ESC[2m── <basename> ──ESC[0m` + `\n\n`
  - body: `bat --color=always --style=plain --paging=never --line-range :40 <file>` if `bat`
    exists, else `sed -n '1,40p' <file>` (first 40 lines, no colour).

#### unknown type

`echo "no preview"` on stdout, exit 0.

---

### 2.9 `open [flags]`

The manifest actions (`open`, `agents`, `blocked`) all route through here.

```bash
local plugin_id="${HERDR_PLUGIN_ID:-sesh-bro}"
for a in "$@"; do
  case "$a" in
    --workspaces|--agents|--dirs|--blocked|--working|--done|--idle|--hide-current) ;;
    *) echo "sesh-bro open: unknown flag $a" >&2; exit 2 ;;
  esac
done
if [[ $# -gt 0 ]]; then
  exec "$HERDR" plugin pane open --plugin "$plugin_id" --entrypoint picker \
    --env "SESH_BRO_ARGS=$*"
fi
exec "$HERDR" plugin pane open --plugin "$plugin_id" --entrypoint picker
```

- `--json` is **not** accepted here (exit 2), even though `list` accepts it.
- Flags are joined with a single space (`$*`, default IFS) into one `SESH_BRO_ARGS` value.
  With zero flags the `--env` argument is omitted entirely rather than passed empty.
- `exec` replaces the process: **herdr's stdout, stderr and exit code become sesh-bro's.**
  `[live, from herdr's own plugin log]`:
  - success → stdout `{"id":"cli:plugin","result":{"type":"ok"}}` + newline, exit 0
  - popup already open → stderr
    `{"error":{"code":"plugin_pane_open_failed","message":"popup already open"},"id":"cli:plugin"}`,
    exit 1
- Because of `exec`, nothing after this point in the script ever runs.

---

### 2.10 `picker [flags]` (default command)

```bash
local picker_args=(${SESH_BRO_ARGS:-} "$@")
local hide_flag=""
for arg in "${picker_args[@]+"${picker_args[@]}"}"; do
  [[ $arg == "--hide-current" ]] && hide_flag="--hide-current"
done
if [[ ${#picker_args[@]} -eq 0 ]] && [[ $CFG_DEFAULT_FILTER != "all" ]]; then
  case "$CFG_DEFAULT_FILTER" in
    workspaces) picker_args+=(--workspaces) ;;
    agents)     picker_args+=(--agents) ;;
    dirs)       picker_args+=(--dirs) ;;
    blocked)    picker_args+=(--blocked) ;;
  esac
fi
require_herdr || exit 1
command -v fzf >/dev/null 2>&1 || { echo "sesh-bro: fzf is required" >&2; exit 1; }
```

1. `$SESH_BRO_ARGS` is **word-split on IFS** (deliberate, `# shellcheck disable=SC2206`)
   and prepended to the command-line arguments. Flags are **not validated here** — an
   invalid one surfaces from the `list` subprocess.
2. `--hide-current` anywhere in the combined list sets `hide_flag`, which is interpolated
   into the **reload binds only** (the initial `list` call gets the full argument list).
3. The configured default filter applies only when the combined list is **completely
   empty**. Values outside the four listed cases silently do nothing.
4. Dependency gate here is only `herdr` + `fzf` — **not** jq, not the daemon. Those are
   checked by the `list` subprocess.

Alias prefill:

```bash
if [[ -n $CFG_ALIASES ]]; then
  IFS=':' read -r -a pairs <<< "$CFG_ALIASES"
  if [[ ${#pairs[@]} -eq 1 ]]; then
    pair="${pairs[0]}"
    alias_query="${pair%%=*}"
  fi
fi
```

`SESH_BRO_ALIASES` is `alias=label:alias2=label2`. With **exactly one** pair, the alias
(everything before the first `=`) prefills the fzf query. With two or more pairs, no
prefill at all — the comment explains why: a multi-term query would OR-match unrelated
rows. The label half of each pair is **never used**; the mechanism is purely a query
prefill.

Preview width sanitisation:

```bash
local pw="$CFG_PREVIEW_WIDTH"
[[ $pw =~ ^[0-9]+%?$ ]] || pw="60%"
```

`60%`, `60` accepted; anything else (`half`, `60px`, `-10%`, empty-after-default) → `60%`.

---

## 3. The picker: exact fzf invocation

Lines 536–564, verbatim:

```bash
  local preview_opt=()
  # fzf single-quotes {n} field placeholders itself (see `man fzf`), so a
  # bare {2} keeps targets with spaces (e.g. "Mobile Documents") as one
  # argument — no extra quoting here, which would double-escape it.
  [[ $CFG_PREVIEW_ENABLED -eq 1 ]] && preview_opt=("--preview=$SELF_Q preview {1} {2}")

  # Guard against a malformed preview-width config breaking the fzf arg.
  local pw="$CFG_PREVIEW_WIDTH"
  [[ $pw =~ ^[0-9]+%?$ ]] || pw="60%"

  local selection
  selection="$("$SELF" list "${picker_args[@]+"${picker_args[@]}"}" | fzf \
    --ansi \
    --delimiter='\t' \
    --with-nth='3..' \
    --layout=reverse \
    --tiebreak=index \
    --query="$alias_query" \
    --prompt='sesh> ' \
    --header='enter connect · ^w workspaces · ^e agents · ^b blocked · ^x dirs · ^o all · ^/ create' \
    "${preview_opt[@]+"${preview_opt[@]}"}" \
    --preview-window="right,$pw,border-left" \
    --bind="ctrl-w:reload($SELF_Q list --workspaces $hide_flag)" \
    --bind="ctrl-e:reload($SELF_Q list --agents $hide_flag)" \
    --bind="ctrl-b:reload($SELF_Q list --blocked $hide_flag)" \
    --bind="ctrl-x:reload($SELF_Q list --dirs $hide_flag)" \
    --bind="ctrl-o:reload($SELF_Q list $hide_flag)" \
    --bind="ctrl-/:execute-silent($SELF_Q create)+reload($SELF_Q list $hide_flag)" \
  )" || true
```

### 3.1 Flag-by-flag

| Flag | Value | Note |
|---|---|---|
| `--ansi` | — | required; rows carry SGR escapes |
| `--delimiter` | `\t` | a two-character string that fzf parses as a **regex** matching TAB |
| `--with-nth` | `3..` | display field 3 to end — hides `type` and `target` from the user and from matching |
| `--layout` | `reverse` | prompt at top, list below |
| `--tiebreak` | `index` | equal-score rows keep **input order** — this is what makes the current-first ordering in §2.2 visible |
| `--query` | `$alias_query` | usually empty |
| `--prompt` | `sesh> ` | trailing space is part of it |
| `--header` | `enter connect · ^w workspaces · ^e agents · ^b blocked · ^x dirs · ^o all · ^/ create` | `·` is U+00B7 |
| `--preview` | `<quoted SELF> preview {1} {2}` | present only when `CFG_PREVIEW_ENABLED -eq 1`; one argv element |
| `--preview-window` | `right,<pw>,border-left` | comma-separated form; `<pw>` is the sanitised width |
| `--bind` ×6 | see below | one argv element each |

`--preview-window` is passed **unconditionally**, including when the preview is disabled
(harmless).

There is no `--multi`, no `--exit-0`, no `--select-1`, no `--height` (the popup pane
provides the geometry), and no `--no-mouse`.

### 3.2 Field placeholders

`{1}` and `{2}` are fzf field references against `--delimiter`, i.e. the `type` and
`target` columns. **fzf single-quotes each `{n}` expansion itself** before handing the
command to the shell, which is why the bash passes bare `{2}` rather than `"{2}"` — the
latter double-escapes and breaks paths containing spaces. This was an actual regression,
fixed in `dac159a`, and re-broken and re-fixed in `b59ec21`. The bats suite pins it
(`"preview target with spaces survives fzf-style expansion"`).

### 3.3 Binds

Every reload runs `<quoted SELF> list …` fresh — reload output replaces the list, and the
pane cache (§7.3) is what keeps this from hammering the daemon (in theory; see
[S4](#s4-the-pane-cache-ttl-test-has-no-minus-sign-and-almost-never-hits)).

| Key | Action |
|---|---|
| `ctrl-w` | `reload(<SELF> list --workspaces [--hide-current])` |
| `ctrl-e` | `reload(<SELF> list --agents [--hide-current])` |
| `ctrl-b` | `reload(<SELF> list --blocked [--hide-current])` |
| `ctrl-x` | `reload(<SELF> list --dirs [--hide-current])` |
| `ctrl-o` | `reload(<SELF> list [--hide-current])` — "all" |
| `ctrl-/` | `execute-silent(<SELF> create)` **then** `reload(<SELF> list [--hide-current])` |

When `hide_flag` is empty the reload string still contains the trailing space
(`… list --workspaces )`). Harmless to the shell; a Go port emitting the same strings
byte-for-byte is safest.

Reload binds carry **only** `--hide-current` — never the status filters from
`SESH_BRO_ARGS`, never `SESH_BRO_DEFAULT_FILTER`. So `ctrl-o` always means "all three
sources, unfiltered", by design.

`ctrl-/` is not universally deliverable — many terminals send `ctrl-_` for it. fzf 0.74.2
accepts the bind name `[live: fzf accepts it at parse time]`; whether the keypress arrives
is terminal-dependent. `[UNVERIFIED end-to-end]`

`esc` and `ctrl-c` are fzf defaults (abort); they are not bound here.

### 3.4 Selection parsing and exit

```bash
  [[ -z $selection ]] && exit 0

  local type target
  type="$(printf '%s' "$selection" | cut -f1)"
  target="$(printf '%s' "$selection" | cut -f2)"
  if ! cmd_connect "$type" "$target"; then
    printf 'sesh-bro: failed to connect to %s %s\n' "$type" "$target" >&2
    read -r -p "Press enter to return to the picker..." -n 1 _ </dev/tty 2>/dev/null || sleep 1
    exit 1
  fi
```

- `|| true` on the assignment swallows every fzf exit code — ESC/ctrl-c (130), no-match,
  and a failed `list` upstream all end as an empty `selection` and **exit 0**. A `list`
  failure still prints its message to the inherited stderr first.
- `cut -f1` / `cut -f2` with the default TAB delimiter. `$()` has already stripped the
  trailing newline.
- On connect failure: the message, then a **single-keypress** wait read from `/dev/tty`
  (falling back to `sleep 1` when there is no tty), then **exit 1**. The prompt says
  "return to the picker" but the process exits — see
  [S13](#s13-the-connect-failure-prompt-lies).
- `connect` with an unknown type calls `exit 2` from inside `cmd_connect`, bypassing this
  handler entirely. Unreachable via the picker (only the three generated types occur).
- Success falls off the end of the function → exit 0.

---

## 4. Row format

### 4.1 Wire format

Every human-mode `list` row is exactly three TAB-separated fields:

```
<type> TAB <target> TAB <display>
```

- **field 1 — `type`**: literal `workspace`, `agent`, or `dir`. Consumed by `{1}` and
  `cut -f1`.
- **field 2 — `target`**: the connect/preview handle. Consumed by `{2}` and `cut -f2`.
  Never quoted; may contain spaces (dir paths); may not contain TAB or newline.
- **field 3 — `display`**: everything fzf shows and matches against (`--with-nth=3..`).
  Contains no TAB.

Field 3 is assembled by
`printf '%s\t%s\t%s %s %s\n' "$type" "$target" "$icon" "$label" "$C_DIM$detail$C_RESET"`:

```
<colour><ICON>ESC[0m<space><label><space>ESC[2m<detail>ESC[0m
```

Both separators are exactly one space. There is **no column alignment or padding** — rows
are ragged, and the README's aligned mock-up is illustrative only.

### 4.2 Per-source table

| | workspace | agent | dir |
|---|---|---|---|
| field 1 | `workspace` | `agent` | `dir` |
| field 2 (target) | `workspace_id` | `name` ?? `pane_id` | absolute path |
| status (internal) | `agent_status` ?? `unknown` | `agent_status` ?? `unknown` | literal `-` |
| icon glyph | `$SESH_BRO_ICON_WORKSPACE` (`◆`) | `$SESH_BRO_ICON_AGENT` (`●`) | `$SESH_BRO_ICON_DIR` (`▸`) |
| icon colour | `status_color <status>` | `status_color <status>` | **`status_color dir`** = `ESC[36m` (the `-` status is ignored) |
| label | `label` ?? `workspace_id` | `name` ?? `agent` ?? `pane_id` | basename (`${path##*/}`, whole path if empty) |
| detail | `<N>p/<M>t` + `" · current"` if focused + git suffix | `<agent ?? "?"> · <terminal_title_stripped ?? cwd ?? "">` | the full path |
| connect action | `workspace focus <target>` | `agent focus <target>` | focus the pane's workspace, else create one |

(`??` = jq `//`, i.e. "if null or false".)

Git suffix on workspace rows, appended to `detail` before the dim wrapper:

```
<space>ESC[2m[ESC[0m<branch>[*]ESC[2m]ESC[0m
```

The `ESC[0m` after `[` **terminates the outer dim**, so the branch and everything after
the closing `]` render at normal intensity. That is the actual on-screen appearance and
must be reproduced.

### 4.3 Reference bytes `[live]`

`cat -v` rendering (`^[` = ESC, `M-b M-^W M-^F` = `◆` U+25C6, `M-^O` = the `●` tail,
`M-^V M-8` = `▸` U+25B8, `M-B M-7` = `·` U+00B7):

```
workspace	w49	^[[33mM-bM-^WM-^F^[[0m ORG ^[[2m2p/1t M-BM-7 current^[[0m
workspace	w3R	^[[90mM-bM-^WM-^F^[[0m DOTFILES ^[[2m3p/1t ^[[2m[^[[0mmain*^[[2m]^[[0m^[[0m
workspace	w48	^[[34mM-bM-^WM-^F^[[0m hero-phases ^[[2m5p/2t ^[[2m[^[[0mfeat/stoke-meld^[[2m]^[[0m^[[0m
agent	w49:p1	^[[33mM-bM-^WM-^O^[[0m claude ^[[2mclaude M-BM-7 Explore Herdr skills flag and documentation^[[0m
agent	pagefx	^[[33mM-bM-^WM-^O^[[0m pagefx ^[[2mcodex M-BM-7 hero-pages^[[0m
dir	/Users/cyperx/Library/Mobile Documents/iCloud~md~obsidian/Documents/cyperx	^[[36mM-bM-^VM-8^[[0m cyperx ^[[2m/Users/cyperx/Library/Mobile Documents/iCloud~md~obsidian/Documents/cyperx^[[0m
```

Points these pin down that prose does not:

- `w49` is focused **and** current, so it sorts first and gets ` · current`, yet has **no**
  git suffix — its first-listed pane's cwd (`/Users/cyperx`) is not a repo.
- `w3R` shows `main*` (dirty) with the dim brackets and the trailing double reset.
- `agent w49:p1` is the **unnamed** case: target is the pane id, label is the agent kind.
- The `dir` row's target contains spaces and is not quoted.

---

## 5. `SESH_BRO_*` configuration

All twelve are read once at startup as `${VAR:-default}`. Six are exposed through the
manifest's `[config]` / `[default_config]`; six are env-only.

| Env var | Manifest key | Type as used by the bash | Default | Effect |
|---|---|---|---|---|
| `SESH_BRO_PREVIEW_WIDTH` | `preview_width` (string) | string matching `^[0-9]+%?$`, else `60%` | `60%` | the `<width>` in `--preview-window=right,<width>,border-left` |
| `SESH_BRO_PREVIEW_ENABLED` | `preview_enabled` (boolean) | **integer, compared `-eq 1`** | `1` | `1` adds `--preview=…`; any other numeric value omits it |
| `SESH_BRO_HIDE_CURRENT` | `hide_current` (boolean) | **integer, compared `-eq 1`** | `0` | `1` makes every `list` behave as if `--hide-current` were passed |
| `SESH_BRO_DIR_SOURCES` | `dir_sources` (boolean) | **integer, compared `-eq 1`** | `1` | `0` suppresses the zoxide block entirely |
| `SESH_BRO_CACHE_TTL` | `cache_ttl` (integer) | interpolated **unvalidated** into `find -mmin <val>`; non-numeric → permanent silent cache miss | `2` | pane-cache freshness window — **see [S4](#s4-the-pane-cache-ttl-test-has-no-minus-sign-and-almost-never-hits)** |
| `SESH_BRO_DEFAULT_FILTER` | `default_filter` (string) | one of `all`/`workspaces`/`agents`/`dirs`/`blocked` | `all` | filter applied by `picker` when the argument list is completely empty; other values are silently ignored |
| `SESH_BRO_BLACKLIST` | *(schema only)* | colon-separated glob list, word-split and glob-expanded | *(empty)* | dir paths matching any token are dropped from the dir block |
| `SESH_BRO_SORT_ORDER` | *(schema only)* | comma-separated subset of `workspaces,agents,dirs` | *(empty)* | reorders the blocks; **unlisted blocks are dropped** |
| `SESH_BRO_ICON_WORKSPACE` | *(schema only)* | string | `◆` | workspace glyph in `list` rows only |
| `SESH_BRO_ICON_AGENT` | *(schema only)* | string | `●` | agent glyph in `list` rows only |
| `SESH_BRO_ICON_DIR` | *(schema only)* | string | `▸` | dir glyph in `list` rows only |
| `SESH_BRO_ALIASES` | *(schema only)* | `a=b:c=d` | *(empty)* | with exactly one pair, prefills the fzf query with the part before `=` |

Non-config environment:

| Var | Set by | Effect |
|---|---|---|
| `HERDR_BIN_PATH` | user / tests | the `herdr` binary to invoke |
| `HERDR_WORKSPACE_ID` | herdr, in every spawned pane `[live]` | primary source of "current workspace" |
| `HERDR_PLUGIN_CONTEXT_JSON` | herdr, for plugin panes/actions — **[UNVERIFIED]**, not observed in a live pane, where only `HERDR_ENV`, `HERDR_PANE_ID`, `HERDR_SOCKET_PATH`, `HERDR_TAB_ID` and `HERDR_WORKSPACE_ID` were present | fallback workspace id (recursive `.workspace_id`); `create`'s path (`.focused_pane_cwd` ?? `.workspace_cwd`, top level) |
| `HERDR_PLUGIN_ID` | herdr | plugin id passed to `plugin pane open`; defaults to `sesh-bro` |
| `HERDR_PLUGIN_CLICKED_URL` | herdr link handler | `worktree`'s URL, **overriding** the positional argument |
| `HERDR_PLUGIN_STATE_DIR` | herdr | gh-title cache directory |
| `SESH_BRO_ARGS` | `open`, via `--env` | space-delimited flags word-split into the picker's argument list |
| `SESH_BRO_PANE_CACHE` | user / tests | overrides the pane-cache path |
| `TMPDIR`, `UID`, `HOME`, `PWD`, `IFS` | shell | cache paths, `~` expansion, `create` fallback, word-splitting |

---

## 6. herdr calls (→ herdr-api mapping)

Thirteen distinct `herdr` CLI invocations. The CLI writes `{"id":…,"result":{…}}` to
stdout on success, and `{"error":{"code":…,"message":…},"id":…}` to **stderr** with exit
**1** on failure `[live]` — the bash's `*'"error"'*` stdout checks are therefore belt-and-
braces only.

| # | CLI invocation | Call sites | Fields read | herdr-api equivalent |
|---|---|---|---|---|
| 1 | `herdr workspace list` | `herdr_ok` (output discarded), `list`, `last`, `worktree` | `workspace_id`, `number`, `label`, `focused`, `pane_count`, `tab_count`, `agent_status` | `WorkspaceList(ctx) ([]Workspace, error)` |
| 2 | `herdr workspace get <id>` | `preview workspace` | `label`, `agent_status` | `WorkspaceGet(ctx, id)` |
| 3 | `herdr workspace focus <id>` | `connect workspace`, `connect dir`, `last`, `root`, `worktree` | — (exit code only) | `WorkspaceFocus(ctx, id) error` |
| 4 | `herdr workspace create --cwd <p> --label <l> --focus` | `connect dir`, `create`, `root`, `worktree` | — (exit code only) | `WorkspaceCreate(ctx, WorkspaceCreateParams{CWD: &p, Label: &l, Focus: true})` |
| 5 | `herdr agent list` | `list` | `name`, `agent`, `agent_status`, `workspace_id`, `pane_id`, `terminal_title_stripped`, `cwd` | `AgentList(ctx) ([]Agent, error)` |
| 6 | `herdr agent get <target>` | `preview agent` | `name`, `agent`, `agent_status`, `cwd` | `AgentGet(ctx, target)` |
| 7 | `herdr agent focus <target>` | `connect agent` | — | `AgentFocus(ctx, target) error` |
| 8 | `herdr agent read <target> --source visible --ansi` | `preview agent` | raw text → stdout | `AgentRead(ctx, AgentReadParams{Target: t, Source: ReadSourceVisible, Format: ReadFormatANSI, StripANSI: ptrFalse})` → print `.Text` |
| 9 | `herdr pane list` | `pane_list()` (cached): dir dedup, git enrichment, `connect dir`, `root` | `pane_id`, `workspace_id`, `cwd`, `focused` | `PaneList(ctx, "")` |
| 10 | `herdr pane list --workspace <id>` | `preview workspace` (uncached) | `pane_id`, `focused` | `PaneList(ctx, id)` |
| 11 | `herdr pane read <pane> --source visible --ansi` | `preview workspace` | raw text → stdout | `PaneRead(ctx, PaneReadParams{PaneID: p, Source: ReadSourceVisible, Format: ReadFormatANSI, StripANSI: ptrFalse})` → print `.Text` |
| 12 | `herdr plugin pane open --plugin <id> --entrypoint picker [--env SESH_BRO_ARGS=…]` | `open` (via `exec`) | passthrough | **no herdr-api method** — see below |
| 13 | *(`command -v $HERDR`)* | `require_herdr` | — | replaced by dialing the socket |

Notes for the port:

- **`plugin.pane.open` is not in herdr-api.** The published client wires 41 of the 90
  schema methods and plugin management is not among them. Either keep shelling out to the
  `herdr` CLI for `open` alone, or add the method upstream. Do not silently change what
  `open` does.
- `--ansi` on `pane read` / `agent read` corresponds to `format = "ansi"` **plus**
  `strip_ansi = false`. `strip_ansi` defaults to `true` server-side, so the pointer must be
  set explicitly — a `bool` with `omitempty` would drop it and strip the colours the preview
  exists to show. `[live: `--ansi` output contains SGR escapes; without it, it does not]`
- Read output is printed **verbatim with no added trailing newline** `[live: xxd-verified]`.
- `Agent.Agent` is `*string` (nil for a launch-pending seat); the bash's `.agent // "?"`
  maps to `nil → "?"`. `Agent.Name` is a plain `string`, so absent and `""` are
  indistinguishable in Go — acceptable only because herdr omits the key rather than sending
  `""` `[live]`.
- `Pane.ID` (not `PaneID`), `Workspace.ID`, `Workspace.Status`, `Agent.Status`.
- `Client.Call` dials per request; the server closes after one response. `herdr_ok`'s
  "is the daemon up" probe becomes a dial + one cheap call.
- The bash issues **22 `jq` subprocesses and 13 `herdr` subprocesses**; all of them
  disappear.

---

## 7. External tools

| Tool | Required? | Used for | Missing → |
|---|---|---|---|
| `herdr` | **hard** | everything | `list`/`picker`/`startup` abort with a message, exit 1 |
| `jq` | **hard** (bash only) | all JSON | `list`/`startup` abort, exit 1. **Not** checked by `picker`, `connect`, `create`, `preview`, `last`, `root`, `worktree` — those fail messily. |
| `fzf` | **hard for `picker`** | the picker | `sesh-bro: fzf is required`, exit 1 |
| `git` | soft | branch/dirty enrichment (`list`), `rev-parse --show-toplevel` (`root`) | enrichment skipped silently; `root` reports "not in a git repo" |
| `zoxide` | soft | `query --list` (dir source), `add` (on create/connect) | dir block silently empty; `add` skipped |
| `gh` | soft | issue/PR title for `worktree` | label falls back to the bare number |
| `eza` | soft | `preview dir` listing | falls back to `ls -la` |
| `bat` | soft | `preview dir` README | falls back to `sed -n '1,40p'` |
| `sed`, `awk`, `cut`, `grep`, `find`, `sort`, `head`, `basename`, `getent`, `ls` | assumed | assorted | undefined |

### 7.1 `check_deps` and friends

```bash
require_herdr() {
  command -v "$HERDR" >/dev/null 2>&1 || {
    echo "sesh-bro: herdr binary not found (HERDR_BIN_PATH=$HERDR)" >&2
    return 1
  }
}
require_jq() {
  command -v jq >/dev/null 2>&1 || {
    echo "sesh-bro: jq is required (install with your package manager)" >&2
    return 1
  }
}
herdr_ok() { "$HERDR" workspace list >/dev/null 2>&1; }
check_deps() {
  require_herdr || return 1
  require_jq || return 1
  herdr_ok || {
    echo "sesh-bro: herdr daemon is not responding (is 'herdr' running?)" >&2
    return 1
  }
}
```

Exact message strings (all stderr):

- `sesh-bro: herdr binary not found (HERDR_BIN_PATH=<value of $HERDR>)`
- `sesh-bro: jq is required (install with your package manager)`
- `sesh-bro: herdr daemon is not responding (is 'herdr' running?)` — `startup` only
- `sesh-bro: herdr daemon is not responding` — `list` only (no suffix)
- `sesh-bro: fzf is required` — `picker` only

`check_deps` is called **only** by `startup`. `list` inlines the same three checks with the
shorter daemon message; `picker` checks only herdr + fzf.

Note `command -v "$HERDR"` also succeeds for an absolute path to an executable file, which
is how the mock is wired.

### 7.2 Which commands check what

| Command | herdr bin | jq | daemon | fzf |
|---|---|---|---|---|
| `startup` | ✓ | ✓ | ✓ | — |
| `list` | ✓ | ✓ | ✓ | — |
| `picker` | ✓ | — | — | ✓ |
| `connect` / `create` / `preview` / `last` / `root` / `worktree` / `open` | — | — | — | — |

### 7.3 The pane cache

```bash
pane_list() {
  local cache="${SESH_BRO_PANE_CACHE:-${TMPDIR:-/tmp}/sesh-bro-${UID:-$(id -u)}-panes.json}"
  if [[ -s $cache ]] && find "$cache" -mmin "$CFG_CACHE_TTL" 2>/dev/null | grep -q .; then
    cat "$cache"
    return
  fi
  local out
  out="$("$HERDR" pane list)" || { echo "sesh-bro: herdr pane list failed" >&2; return 1; }
  printf '%s\n' "$out" >"$cache"
  printf '%s\n' "$out"
}
```

- Default path `${TMPDIR:-/tmp}/sesh-bro-${UID}-panes.json`; per-user to avoid collisions on
  shared machines. Not created with restrictive permissions, not written atomically —
  concurrent pickers can interleave writes.
- Freshness test is `find <file> -mmin <TTL>` with **no minus sign**. This is not "younger
  than TTL"; see [S4](#s4-the-pane-cache-ttl-test-has-no-minus-sign-and-almost-never-hits).
- `$CFG_CACHE_TTL` is interpolated straight into the `find` argument with no validation. A
  non-numeric value makes `find` fail; its stderr is suppressed (`2>/dev/null`), `grep -q .`
  sees nothing, and the cache **permanently misses** with no diagnostic. A negative value
  (`-1`) silently becomes the *correct* `-mmin -1` semantics.
- On failure: `sesh-bro: herdr pane list failed` on stderr, return 1. Callers vary in
  whether they suppress that (`list` and `connect dir` do, `root` does not).
- `startup` deletes the file.
- `preview workspace` does **not** use this function — it calls `pane list --workspace`
  directly, uncached.

---

## 8. Edge cases the code visibly guards

| # | Case | Guard | Where |
|---|---|---|---|
| 1 | **Paths with spaces** in dir rows, previews, `create`, `connect` | every expansion quoted; fzf's own `{n}` single-quoting relied on rather than added to | §3.2, bats ×3 |
| 2 | **Symlinked install** (`herdr plugin install` puts a symlink on PATH) | 4-tier `SELF` resolution; manifest read from the *resolved* dir | §1.2 |
| 3 | **Plugin dir with quotes/spaces** in fzf bind strings | `SELF_Q` POSIX-quotes `SELF` | §1.4 |
| 4 | **Missing jq** | `require_jq` in `list`/`startup` | §7.1 |
| 5 | **Malformed `preview_width`** | regex `^[0-9]+%?$`, else `60%` | §2.10 |
| 6 | **Multi-alias query** | prefill only with exactly one pair | §2.10 |
| 7 | **No tty** for `create` | `-t 0` guards the `read` prompt; context JSON then `$PWD` | §2.4, bats |
| 8 | **`execute-silent`** for `ctrl-/` | keeps the picker open; suppresses `create`'s output | §3.3 |
| 9 | **Link-handler flow** | `HERDR_PLUGIN_CLICKED_URL` takes precedence; manifest pattern anchored; two URL forms accepted | §2.7 |
| 10 | **Daemon down mid-list** | `pane_list 2>/dev/null … || true` in the dir and git-enrichment paths — degrade rather than fail | §2.2.6, §2.2.8 |
| 11 | **Missing workspace/agent in preview** | `2>/dev/null \|\| true` + empty/`"error"` test → friendly `(not found)`, exit 0 | §2.8 |
| 12 | **Stale zoxide entry** | not filtered at list time; `connect dir` rejects a non-directory, `preview dir` prints `(directory not found)` | §2.3, §2.8 |
| 13 | **Empty basename** (path `/`) | falls back to the whole path for both label and workspace label | §2.2.6, §2.3, §2.4 |
| 14 | **Null `agent_status`** | normalised to `unknown` before filtering and ranking | §2.2.4–5 |
| 15 | **Empty arrays under `set -u`** | `"${arr[@]+"${arr[@]}"}"` idiom everywhere | §2.2.7, §2.10, §3 |
| 16 | **Editor holding the git index lock** | `--no-optional-locks` on both `git status` calls | §2.2.8 |
| 17 | **Cross-user temp collision** | `${UID}` in the cache filename | §7.3 |
| 18 | **Version drift** between script and manifest | version parsed from the manifest at runtime | §1.3 |

---

## 9. Surprises, bugs and traps

Everything here is **current behaviour and must be reproduced**. Fixing any of it is a
separate decision with a separate changelog entry.

### S1. Boolean config values crash the script

`SESH_BRO_PREVIEW_ENABLED`, `SESH_BRO_HIDE_CURRENT` and `SESH_BRO_DIR_SOURCES` are compared
with `[[ $VAR -eq 1 ]]`, which is **arithmetic** evaluation. A non-numeric value is treated
as a variable name; under `set -u` an unset name is a fatal error.

```
$ SESH_BRO_HIDE_CURRENT=true ./sesh-bro list --workspaces
./sesh-bro: line 156: true: unbound variable
$ echo $?
1
```
`[live]`

The manifest declares all three as `type = "boolean"`. **If herdr renders a boolean config
value as `true`/`false` into the env var, the plugin dies on every `list`.** The reference
machine's config dir (`~/.config/herdr/plugins/config/sesh-bro`) is empty, so no user has
hit it. **[UNVERIFIED]** — how herdr serialises a boolean `[config]` value into a
`SESH_BRO_*` env var is unknown. Settling it requires writing plugin config, which mutates
user state; not done.

**Port decision required before shipping.** A Go `strconv.ParseBool` would accept `true`
and thereby *change* behaviour (crash → works). That is arguably right, but it is a
behaviour change and must be a deliberate, documented one — not an accident of writing
idiomatic Go. Reproducing the bash exactly means: treat the value as an integer, and treat
a non-integer as a fatal error with exit 1.

### S2. `last` refocuses the current workspace when the context is unknown

`cmd_last` excludes `current_workspace_id()`, then sorts **focused-first**. When
`HERDR_WORKSPACE_ID` is unset (e.g. run from a plain shell outside herdr), `current` is
empty, nothing is excluded, and the focused workspace sorts to position 0 — so `last`
focuses the workspace that is already focused. A no-op that looks like a success.

Inside a herdr pane `HERDR_WORKSPACE_ID` is always set `[live]`, so the command works as
intended in its normal habitat, which is why this has survived.

### S3. Status flags clear earlier source flags (order-dependent)

```bash
--blocked|--working|--done|--idle)
  statuses="$statuses ${1#--}"; want_ag=1; want_ws=0; want_dir=0; source_flag=1 ;;
```

`sesh-bro list --dirs --blocked` shows agents only; `sesh-bro list --blocked --dirs` shows
blocked agents *and* dirs. A Go flag package that collects all flags before acting will get
this wrong — the assignment must happen **during** the left-to-right scan.

### S4. The pane-cache TTL test has no minus sign, and almost never hits

```bash
find "$cache" -mmin "$CFG_CACHE_TTL"
```

`-mmin N` means "modified **exactly** N minutes ago". It is **not** `-mmin -N` ("less than
N minutes ago"). On the reference machine (Darwin 27, BSD find) the age in minutes is
**truncated**, so `-mmin N` matches an mtime age in the half-open interval
`[N, N+1)` minutes `[live: 30s→0, 61s→1, 119s→1, 121s→2, 175s→2]`. With the default `2`:

- a cache written 0–119 seconds ago → **miss**
- a cache written 120–179 seconds ago → hit
- a cache written 180+ seconds ago → miss

So rapid `ctrl-w`/`ctrl-o` reloads — the exact scenario the cache was written for — re-query
the daemon every time, and the cache only helps in a one-minute window two minutes after a
write.

**The bucket boundary is platform-dependent**, and the manifest declares
`platforms = ["linux", "macos"]`. BSD and GNU `find` are documented differently for the
`-mtime` family (BSD's man page describes rounding up); the truncating behaviour above is
what Darwin 27 actually does. GNU `find`'s `-mmin` boundary is
**[UNVERIFIED]** — no Linux machine was available. The headline claim (a freshly written
cache never hits) holds under either interpretation; only the exact hit window shifts.

The gh-title cache in `cmd_worktree` uses `-mmin -1440` **correctly**, which makes this
look like a genuine typo rather than intent.

An implementer who writes the obvious `time.Since(mtime) < ttl` produces a *more* effective
cache and therefore *fewer* daemon queries and *staler* rows than the bash. Reproduce the
bucket semantics, or change it deliberately and say so.

### S5. `SORT_ORDER` drops unlisted blocks

`SESH_BRO_SORT_ORDER="agents,workspaces"` does not merely reorder — dirs vanish entirely.
Likewise `"agents"` alone shows only agents regardless of flags. Repeating a token
duplicates that block. The schema documents `sort_order` as "Order of source blocks in the
list", which does not say this.

### S6. Blacklist tokens are word-split and glob-expanded

```bash
for bl in ${CFG_BLACKLIST//:/$'\n'}; do
```

The expansion is unquoted, so after the `:`→newline substitution the result undergoes IFS
word-splitting **and** pathname expansion. Consequences:

- a blacklist entry containing a space becomes two independent patterns;
- a pattern that happens to match files in `$PWD` is replaced by those filenames before the
  loop even starts (e.g. `*` expands to the current directory's contents).

The **match** itself is intentional glob matching (`[[ $path == $bl ]]` with an unquoted
RHS, with the shellcheck suppression to prove it). A Go port should use `filepath.Match`
per token — but must decide what to do about the splitting/expansion, which is not
reproducible in Go in any sane way. Recommend: split on `:` only, document the divergence.
`[behaviour divergence — flag it, don't hide it]`

### S7. Branch names are truncated at the first dot

`sed -n 's/^## \([^.]*\).*/\1/p'` captures up to the first `.`, intended to stop before the
`...upstream` separator. A branch named `release/v1.2` renders as `release/v1`; a branch
named `.hidden` renders as empty (and is then dropped, since `[[ -n $branch ]]` fails). The
correct split is on the literal `...` sequence.

### S8. `--json` is pretty-printed, concatenated objects

Not an array, not JSON Lines. Seven lines per object, two-space indent, fixed key order.
`jq -s` is required to slurp it (as the bats suite does). A Go port emitting compact JSON
Lines, or a JSON array, breaks every existing consumer. Use an `encoding/json` encoder with
`SetIndent("", "  ")` per object and no separators.

### S9. Missing arguments die via `set -u`, not a usage message

`connect` and `preview` read `$1`/`$2` with no arity check. `sesh-bro connect` alone
produces bash's own diagnostic on stderr and **exit 1**:

```
sesh-bro: line 288: $1: unbound variable
```

Not exit 2, not the usage text. A Go port that prints a clean usage error and exits 2 is
changing behaviour — and any fzf bind or herdr action branching on the exit code would
notice.

### S10. `~user` expansion is broken on macOS

`getent` does not exist on macOS `[live: absent on the reference machine]`. The fallback,
`eval "printf '%s' \"\$$user\""`, evaluates a **shell variable** named after the user, which
is empty for any real username, so `[[ -n $home ]]` fails and the path is left as the
literal `~alice`. `create` then reports `not a directory: ~alice`. `~/` (with the slash)
works fine on both platforms, and that is the only form anyone uses.

Also note `path="$home/${path#*/}"` assumes a `/` is present: `~alice` with no slash yields
`$home/~alice`.

### S11. `create` under `execute-silent` silently uses `$PWD`

The `ctrl-/` bind runs `sesh-bro create` with no argument. With no tty and no
`HERDR_PLUGIN_CONTEXT_JSON`, the path resolution falls through to `$PWD` — which is the cwd
of the **fzf process**, i.e. wherever the picker pane was started, not the row the user was
looking at. There is no prompt and no confirmation, and `execute-silent` suppresses the
output, so a workspace is created for `$PWD` with no visible feedback beyond the reload.

Inside a herdr popup, `HERDR_PLUGIN_CONTEXT_JSON` is expected to supply
`focused_pane_cwd`, which makes this useful rather than surprising —
`[UNVERIFIED that herdr actually sets that variable for popup panes]`.

### S12. Preview headers ignore the icon config

`cmd_preview` hardcodes `◆`, `●` and `▸` in its `printf` format strings. Setting
`SESH_BRO_ICON_WORKSPACE=W` changes the list rows but not the preview header. `[code]`

### S13. The connect-failure prompt lies

```
sesh-bro: failed to connect to <type> <target>
Press enter to return to the picker...
```

…then `exit 1`. The picker does not reopen. The `read -n 1` accepts **any** single key, not
just enter, and with no `/dev/tty` it degrades to `sleep 1`.

### S14. The `"error"` substring checks are dead code

`preview` tests `$ws == *'"error"'*` against **stdout**, but the herdr CLI writes error
envelopes to **stderr** and exits 1, so the captured variable is simply empty and the
`-z` half of the test is what fires. `[live]` Harmless, but a Go port need not replicate the
substring scan — the not-found path is "the call returned an error".

### S15. `worktree`'s existing-workspace match is repo-agnostic

`test("(^|[^0-9])<num>([^0-9]|$)")` against every workspace label. Opening
`owner-a/repo#409` when a workspace labelled `409 — something from owner-b/other-repo`
exists focuses the wrong workspace. Also, if `.label` were ever null, jq's `test` raises an
error, the assignment fails, and `set -e` kills the script silently (stderr carries jq's
message). Live herdr always sends a string label `[live]`, so this is latent.

### S16. `picker` exits 0 when `list` fails

`selection="$(… | fzf …)" || true` swallows everything. If `list` aborts (daemon down, jq
missing, bad flag in `SESH_BRO_ARGS`), fzf gets an empty stream, the user sees the error
flash by, and `sesh-bro picker` returns **0**. Any caller branching on the picker's exit
code sees success.

```
$ HERDR_BIN_PATH=/tmp/broken-herdr ./sesh-bro picker </dev/null
sesh-bro: herdr daemon is not responding          # on stderr
$ echo $?
0
```
`[live]`

### S17. Git enrichment picks an arbitrary pane's cwd

`group_by(.workspace_id) | map({ws: .[0].workspace_id, cwd: .[0].cwd})` takes the
lowest-sorting pane in each workspace, not the focused one. A workspace whose first pane
sits in `$HOME` and whose focused pane sits in a repo shows **no branch** — exactly what
`w49` does in the reference capture (§4.3). "The workspace's branch" is really "the first
pane's branch".

### S18. `agent` target vs. label fall back differently

Target is `name // pane_id`; label is `name // agent // pane_id`. So an unnamed agent
displays as `claude` but is connected by pane id. This is deliberate — `herdr agent focus`
accepts either a name or a pane id, and the agent *kind* is not a valid target. Getting
this backwards produces a picker where every unnamed claude row focuses the same agent.

### S19. Two identical `git status` calls per workspace

`status --porcelain --branch | head -1` already reports dirtiness in the lines `head -1`
throws away; the code then runs `status --porcelain` again. Observably identical, twice the
cost. A Go port doing one call and reading both facts is behaviour-preserving.

### S20. Trailing-space detail for a titleless agent

`(.agent // "?") + " · " + (.terminal_title_stripped // .cwd // "")` yields e.g.
`claude · ` when both title and cwd are absent. The row is then wrapped in dim, so the
visible text ends with a space before the reset. Reproduce it; don't trim.

---

## Appendix A — exit code reference

| Command | 0 | 1 | 2 |
|---|---|---|---|
| `startup` | deps ok | dep/daemon failure | — |
| `list` | success (incl. 0 rows) | missing herdr/jq, daemon down, upstream herdr failure | unknown flag |
| `connect` | focused/created | focus/create failed; not a directory; missing args (`set -u`) | unknown TYPE |
| `create` | created | not a directory; create failed; no path; missing dir | — |
| `preview` | always, for `workspace`/`agent`/`dir`/unknown | missing args (`set -u`) | — |
| `picker` | selection connected; ESC/abort; **`list` failed** (S16) | connect failed | — |
| `open` | herdr's exit code (0 on success) | herdr's exit code (1 on "popup already open") | unknown flag |
| `last` | focused | no previous workspace; focus failed | — |
| `root` | focused/created | not in a git repo; focus/create failed | — |
| `worktree` | focused or created | no URL; unrecognised URL; create failed | — |
| `-h`/`--help`/`-v`/`--version` | always | — | — |
| unknown command | — | — | always |

## Appendix B — stderr message catalogue (verbatim)

```
sesh-bro: herdr binary not found (HERDR_BIN_PATH=<bin>)
sesh-bro: jq is required (install with your package manager)
sesh-bro: herdr daemon is not responding (is 'herdr' running?)
sesh-bro: herdr daemon is not responding
sesh-bro: fzf is required
sesh-bro: herdr pane list failed
sesh-bro list: unknown flag <flag>
sesh-bro open: unknown flag <flag>
sesh-bro connect: unknown type <type>
sesh-bro: unknown command <cmd>
sesh-bro: failed to focus workspace <id>
sesh-bro: failed to focus agent <target>
sesh-bro: failed to create workspace for <path>
sesh-bro: not a directory: <target>
sesh-bro create: no path
sesh-bro create: not a directory: <path>
sesh-bro: no previous workspace
sesh-bro: not in a git repo
sesh-bro worktree: no URL (usage: sesh-bro worktree <github-url>)
sesh-bro worktree: unrecognized URL <url>
sesh-bro: failed to create worktree workspace for <owner>/<repo>#<num>
sesh-bro: failed to connect to <type> <target>
```

Stdout messages: `sesh-bro: deps ok`, `sesh-bro <version>`,
`sesh-bro: focused workspace <id>`, `sesh-bro: created workspace for <owner>/<repo>#<num>`,
`no preview`, plus the usage text and all `list`/`preview` output.
