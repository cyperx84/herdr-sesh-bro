# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
