# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
