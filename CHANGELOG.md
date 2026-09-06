# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.4.0] - Unreleased

### Added
- **`counts`** — one line saying how many agents are blocked/working/done/idle,
  cheap enough to run on a timer. `--ansi` colours it, `--json` emits every
  state including zeros, `--all` keeps zero states in the human line. Built for
  herdr 0.8.2's right-aligned `tab_bar_right` command entries, so the answer to
  "does anything need me" can live in the tab bar and never be asked for; it
  also drives a SketchyBar item or a shell prompt. An empty session prints
  `no agents` rather than an empty line, because a blank tab-bar entry reads as
  a broken command. Idea from `wjarka/herdr-ghostty-tab-title`, which puts the
  same counts in a Ghostty tab title.
- **Attention-first ordering** — agent rows whose status is `blocked` or `done`
  are hoisted above every other block, from whichever workspace they belong to,
  so the picker opens with the cursor on whoever needs you instead of on the
  workspace you are already in. Those are exactly herdr's two "has something
  for you that you have not seen" states. `SESH_BRO_ATTENTION_FIRST=0` restores
  the previous current-workspace-first order.
- `internal/herdrx/herdrtest`, a fake herdr socket server for tests: real
  newline-delimited JSON-RPC matching herdr-api's wire format, with
  `events.subscribe` held open for pushed events. Success paths that previously
  had no test — `list`, `counts`, `connect` — now have one.

### Changed
- `list` makes **one** `session.snapshot` call instead of `workspace.list` +
  `agent.list` + `pane.list` twice.
- Agent rows break ties within a status rank by `state_change_seq` descending
  before name, so the agent that just changed state leads the ones that have
  been sitting a while. It is herdr's only recency signal — there are no status
  timestamps anywhere in the API.
- An unset `HERDR_WORKSPACE_ID` now falls back to the snapshot's
  `focused_workspace_id`, so current-first ordering and `--hide-current` work
  when the binary runs outside a herdr-spawned pane.
- `git status` is memoised per working directory for the process's lifetime, so
  a re-render costs no subprocesses.
- `min_herdr_version` raised from `0.8.0` to `0.8.2`: `popup.close` and
  `session.snapshot` are only confirmed present in the 0.8.2 schema.

### Removed
- The pane-list file cache, `SESH_BRO_CACHE_TTL`, `SESH_BRO_PANE_CACHE` and the
  manifest's `cache_ttl`. `session.snapshot` removed the calls the cache
  existed to soften, and the cache's TTL bucket reproduced `find -mmin`
  semantics under which the default value almost never produced a hit
  (docs/BEHAVIOUR.md §9 S4).
- The bash implementation and its bats harness. `tests/mock-herdr` mocked the
  herdr **CLI**, and the Go binary speaks the socket, so it could never drive
  this code; `go test` now runs in CI in its place.

### Fixed
- The `SESH_BRO_KEY_CLOSE` default was `ctrl-q` — one of fzf's four default
  abort keys (`ctrl-c`, `ctrl-g`, `ctrl-q`, `esc`) — while the picker's own
  default was `alt-x`, deliberately chosen for exactly that reason
  (see `internal/picker.DefaultKeyBindings`). Config wins at runtime, so the
  keystroke fzf trains users to press for "get me out of here" silently
  closed a workspace, with `execute-silent` swallowing any message. The
  default is now `alt-x` everywhere, and a regression test
  (`TestLoad_KeyCloseDefaultAgreesWithPicker`) pins config's default to
  `picker.DefaultKeyBindings.Close` so the two cannot drift apart again.

### Changed
- `min_herdr_version` raised from `0.8.0` to `0.8.2`: `popup.close` and
  `session.snapshot` are only confirmed present in the 0.8.2 schema.

## [0.3.0] - 2026-08-11

### Changed
- Rewritten from bash to Go, against a behaviour spec extracted from the
  0.2.0 bash script (`docs/BEHAVIOUR.md`) so the port is verifiable line by
  line rather than reimplemented from memory. 6 packages, full test coverage
  on every command (`list`, `connect`, `create`, `preview`, `open`, `startup`,
  `last`, `root`, `worktree`).
- `[[build]]` now compiles the binary via `scripts/build.sh` on
  `plugin install`: local Go toolchain preferred, SHA256-verified prebuilt
  release download as fallback (no tagged release exists yet, so the
  fallback path is a deliberate hard-fail until one is cut — see the
  script's own comments).
- Every manifest `command` now points at `./bin/sesh-bro` (was `["bash",
  "sesh-bro", ...]`). Herdr resolves plugin commands via PATH lookup, not
  relative to the plugin root, and does not itself prepend `./` — a bare
  `bin/sesh-bro` fails with `No viable candidates found in PATH` even
  though the file exists right there. Bash's own `["bash", "sesh-bro"]`
  never hit this because bash resolves relative paths against its own cwd.
  Found and fixed by driving every action through
  `herdr plugin action invoke` end-to-end after the rewrite, not just
  `go build`/`go test` — the manifest-to-herdr dispatch path isn't
  exercised by either.

### Verified (manual end-to-end pass against a live herdr 0.8.0 session)
- `list`/`list --json`: all three sources (workspaces, agents, zoxide dirs)
  match live state; `--workspaces`/`--agents`/`--idle`/`--blocked`/
  `--hide-current` filters all correct (`--hide-current` requires
  `$HERDR_WORKSPACE_ID`, herdr-injected at real pane launch — a standalone
  invocation with it unset correctly no-ops rather than guessing).
- `preview workspace/agent/dir`: all three render real content (live ANSI
  pane output, live agent transcript, `eza`-style dir listing).
- `worktree <url>`: creates a labelled workspace end to end against a real
  (nonexistent) issue number, degrading gracefully to a numeric-only label
  when `gh` can't resolve a title — no crash, matches the documented
  degradation path.
- `agents`/`blocked` actions correctly refuse to stack a second popup
  while one is already open.
- `startup` hook and `worktree` action confirmed resolving and executing
  correctly through `herdr plugin action invoke` on the relative
  `./bin/sesh-bro` path (the actual fix, see above). `open`/`agents`/
  `blocked` share the same `./bin/sesh-bro` resolution — confirmed via the
  now-fixed absolute-path form mid-session before the relative-path fix
  was finalized (`exit_code: 0`/`succeeded`), but not independently
  re-confirmed after switching to `./bin/sesh-bro` because a stray popup
  from manual testing got stuck open server-side (a herdr session/client
  quirk unrelated to this plugin — there was no attached UI client to
  dismiss it against). Re-verify these three against a live picker
  keypress before cutting a release.

## [0.2.0] - 2026-08-10

### Added
- MIT license, manifest metadata (`repository`, `license`, `authors`)
- `[config]` schema + `default_config` so users can configure via Herdr
- `[[startup]]` hook validating dependencies (herdr/fzf/jq) and clearing stale cache
- `[[link_handlers]]`: ctrl-click a GitHub issue/PR URL creates/focuses a worktree workspace
- `worktree` command (parse URL → resolve issue title via `gh` → create/focus workspace)
- Git branch + dirty status shown in list rows (clean green, dirty yellow)
- GitHub issue-title enrichment for labels matching `#N` (24h cached via `gh`)
- Blacklist config (globs) for dirs/workspaces
- Session aliases + alias chip in picker + exact-alias jump
- Configurable sort order and per-source icons
- `last` and `root` convenience commands
- Clear errors when herdr/jq are missing or the daemon is down (no silent empty picker)
- Path-safe `SELF` quoting in fzf preview/reload binds
- Per-user pane cache path
- macOS CI job, expanded mock + bats coverage, release workflow
- README overhaul: demo, install via GitHub, config/keybind tables, troubleshooting, FAQ

### Fixed
- Preview of targets with spaces (e.g. `~/Library/Mobile Documents/...`) —
  fzf single-quotes `{n}` placeholders, so the preview bind now uses a bare
  `{2}` instead of double-quoting it (which broke the path)
- Git branch enrichment: parse `## branch...upstream` correctly and batch to
  one `git status --porcelain --branch` per distinct workspace cwd with
  `--no-optional-locks`
- `create` no longer errors on a missing tty (popup/^/ context): falls back to
  the invoking pane's cwd from `HERDR_PLUGIN_CONTEXT_JSON`, then `$PWD`;
  `^/` uses `execute-silent` so the picker stays open after creating
- `connect`/`preview` no longer leak raw herdr error JSON; missing
  workspaces/agents/dirs render a friendly "(not found)" and connect failures
  exit non-zero with a clean message
- `connect dir` rejects non-directory targets
- `SELF` resolves through symlinks, so PATH/symlinked installs (`herdr plugin
  install`) find the manifest and version correctly
- `last` fixed (jq `-(.number)` parens) and `root`/`create` exit codes propagate
- Alias prefill only applies with a single alias (multi-alias OR-match removed)
- Malformed `SESH_BRO_PREVIEW_WIDTH` falls back to `60%`

### Changed
- `min_herdr_version` bumped to `0.8.0` (all used APIs verified against it)
- Version single-sourced from the manifest
- Cache defaults to per-user temp path

## [0.1.0] - 2026-08-09

### Added
- Initial release: sesh-style fuzzy picker for Herdr workspaces, agents, and zoxide dirs
- fzf popup with live terminal previews, status-colored icons, source filters
- Conflict-free keybinds (`^w`/`^e`/`^b`/`^x`/`^o`/`^/`)
- Bats test suite with mock herdr/zoxide, shellcheck, CI
