# Sesh Bro

A [sesh](https://github.com/joshmedeski/sesh)-style fuzzy session picker for
[Herdr](https://herdr.io) — workspaces, agents, and zoxide directories in one
`fzf` popup with live terminal previews.

> Installable from GitHub: `herdr plugin install cyperx84/herdr-sesh-bro`
> (also available in the [Herdr plugin marketplace](https://herdr.dev/plugins/)).

![sesh-bro demo](demo/sesh-bro.gif)

```
sesh> alpha                                       ▲ 40%
◆ alpha  1p/1t  [main]                            │
● alpha  claude · Move 3D arcade to dedicated…    │
● beta   claude · Rename arcade route  [feat/x]   │
▸ my-project  /Users/me/github/my-project         │
                                                  ▼
enter connect · ^w workspaces · ^e agents · ^b blocked · ^x dirs · ^o all · ^q close · ^/ create
```

## Features

- **Three sources in one picker** — Herdr workspaces, agents (grouped by
  status: blocked → working → done → idle), and zoxide directories, deduped
  against directories already open in a Herdr pane.
- **Live previews** — the focused workspace's or agent's terminal output is
  rendered in the preview pane with ANSI colors preserved; directories show an
  `eza`/`ls` listing and any README.
- **Git awareness** — workspace rows show the current branch and a `*` when
  the working tree is dirty (like sesh).
- **GitHub worktree links** — ctrl-click a GitHub issue/PR URL in any pane and
  sesh-bro creates or focuses the workspace for that issue, named
  `409 — Add git worktree support…` (title resolved via `gh`).
- **Connect-or-create** — workspaces and agents are focused directly; picking
  a directory focuses the existing workspace containing it, or creates a new
  workspace there (`zoxide add` + `herdr workspace create --focus`).
- **Filter sources from inside fzf** — `^w` workspaces / `^e` agents / `^b`
  blocked / `^x` dirs / `^o` all reload the list; `^/` creates a workspace
  from a directory.
- **Close a workspace from the picker** — `^q` closes the highlighted
  workspace row and reloads the list in place; a no-op (never an error) on
  agent/dir rows and on the workspace the picker itself is running in.
- **Current-first ordering** — the current workspace and its agents float to
  the top, then blocked → working → done → idle. Override with `sort_order`.
- **Configurable** — preview size, default filter, dir sources, blacklist,
  icons, keybinds, aliases, and sort order are all user-settable (see
  [Configuration](#configuration)).
- **Herdr plugin integration** — installs as a plugin with a popup pane,
  workspace-context actions, a startup dependency check, and a link handler.

## Requirements

- [Herdr](https://herdr.io) (>= 0.8.0)
- [fzf](https://github.com/junegunn/fzf)
- [jq](https://jqlang.github.io/jq/)
- [zoxide](https://github.com/ajeetdsouza/zoxide) (for directory entries)
- Optional: `eza` (prettier dir previews), `bat` (README preview),
  `gh` (GitHub issue-title resolution for worktree links)

## Install

### From GitHub (marketplace)

```sh
herdr plugin install cyperx84/herdr-sesh-bro
```

### From a local checkout (development)

```sh
git clone https://github.com/cyperx84/herdr-sesh-bro.git
herdr plugin link ~/herdr-sesh-bro
```

The picker is then available as the **Sesh Bro** popup pane, with these
workspace-context actions:

- **Open sesh-bro picker**
- **Open sesh-bro picker (agents only)**
- **Open sesh-bro picker (blocked agents)**
- **Open GitHub issue/PR worktree** (also fired by ctrl-clicking a GitHub URL)

To bind a key to it, add to `~/.config/herdr/config.toml`:

```toml
[[keys.command]]
key = "alt+e"
type = "plugin_action"
command = "sesh-bro.open"
description = "open the sesh-bro picker"
```

then reload herdr: `herdr server reload-config`.

### Update / uninstall

```sh
herdr plugin install cyperx84/herdr-sesh-bro   # reinstall to update
herdr plugin uninstall sesh-bro                # remove
```

## Usage

Run it directly, or bind it to a key:

```sh
sesh-bro picker          # interactive fzf picker (default command)
sesh-bro list            # picker candidates as tab-separated rows
sesh-bro list --json     # machine-readable candidates
sesh-bro connect TYPE TARGET
sesh-bro close TYPE TARGET   # close a workspace (workspace rows only)
sesh-bro create [PATH]   # create a workspace for a dir (default: current dir)
sesh-bro preview TYPE TARGET
sesh-bro open            # open the picker popup via the Herdr plugin API
sesh-bro last            # focus the previously-focused workspace
sesh-bro root            # focus/create the workspace for the current git root
sesh-bro worktree [URL]  # create/focus the workspace for a GitHub issue/PR
```

### Keybinds

| Key | Action |
|-----|--------|
| `enter` | connect (focus workspace/agent, or create a workspace for a dir) |
| `^w` | reload: workspaces only |
| `^e` | reload: agents only |
| `^b` | reload: blocked agents only |
| `^x` | reload: directories only |
| `^o` | reload: all sources |
| `^q` | close the highlighted **workspace** (no-op on agent/dir rows; refuses the current workspace) |
| `^/` | create a new workspace from a directory |
| `esc` | exit |

> The keybinds deliberately avoid `^a` (Herdr's prefix) and `^h/j/k/l`
> (vim-herdr-navigation / tmux pane keys) so they never fight your editor
> or window-manager chords.

Every keybind above is overridable — see `SESH_BRO_KEY_*` in
[Configuration](#configuration). The picker's header hint always reflects
whatever key is actually bound, not the defaults shown here.

`^q` only closes workspace rows. Selecting an agent or zoxide-directory row
and pressing `^q` prints a message and does nothing — it never errors out
of the picker. Closing the workspace the picker itself is running in is
refused the same way, since that would kill the picker mid-action.

### Flags

`sesh-bro list` (and `picker`) accept:

- `--workspaces` / `--agents` / `--dirs` — limit sources
- `--blocked` / `--working` / `--done` / `--idle` — filter agents by status
- `--hide-current` — drop the current workspace and its agents from the list
- `--json` — machine-readable output (`list` only)

## Configuration

Sesh-bro reads `SESH_BRO_*` environment variables, which Herdr's `[config]`
schema exposes. Set them in your shell, or via Herdr's config UI:

| Env var | Default | Meaning |
|---------|---------|---------|
| `SESH_BRO_PREVIEW_WIDTH` | `60%` | Preview pane width |
| `SESH_BRO_PREVIEW_ENABLED` | `1` | `0` disables the preview pane |
| `SESH_BRO_HIDE_CURRENT` | `0` | `1` hides the current workspace + its agents |
| `SESH_BRO_DIR_SOURCES` | `1` | `0` disables zoxide directory entries |
| `SESH_BRO_CACHE_TTL` | `2` | Pane-list cache TTL in minutes |
| `SESH_BRO_DEFAULT_FILTER` | `all` | `workspaces`/`agents`/`dirs`/`blocked` |
| `SESH_BRO_BLACKLIST` | *(empty)* | Colon-separated globs to exclude (dirs) |
| `SESH_BRO_SORT_ORDER` | *(default)* | e.g. `agents,dirs,workspaces` |
| `SESH_BRO_ICON_WORKSPACE` | `◆` | Workspace icon glyph |
| `SESH_BRO_ICON_AGENT` | `●` | Agent icon glyph |
| `SESH_BRO_ICON_DIR` | `▸` | Directory icon glyph |
| `SESH_BRO_KEY_WORKSPACES` | `ctrl-w` | Reload: workspaces only |
| `SESH_BRO_KEY_AGENTS` | `ctrl-e` | Reload: agents only |
| `SESH_BRO_KEY_BLOCKED` | `ctrl-b` | Reload: blocked agents only |
| `SESH_BRO_KEY_DIRS` | `ctrl-x` | Reload: directories only |
| `SESH_BRO_KEY_ALL` | `ctrl-o` | Reload: all sources |
| `SESH_BRO_KEY_CLOSE` | `ctrl-q` | Close the highlighted workspace |
| `SESH_BRO_KEY_CREATE` | `ctrl-/` | Create a workspace from a directory |
| `SESH_BRO_ALIASES` | *(empty)* | `alias=label:alias2=label2` prefill queries |

`SESH_BRO_KEY_*` values must be a valid fzf key name (`ctrl-a`, `alt-w`,
`f5`, a bare letter, ...); anything that would corrupt the underlying
`--bind` flag — an empty value, a stray `:`/`,`/`(`/`)` — falls back to that
key's default instead of breaking the picker. The header hint (`^w
workspaces · ...`) always reflects the key actually bound.

### Environment

- `HERDR_BIN_PATH` — the `herdr` binary to use (defaults to `herdr` on PATH;
  also how the test suite points at its mock).
- `HERDR_PLUGIN_ID` / `HERDR_PLUGIN_CONTEXT_JSON` / `HERDR_WORKSPACE_ID` —
  provided by Herdr when the plugin runs.
- `SESH_BRO_ARGS` — flags carried into a plugin-opened picker (space-delimited).
- `SESH_BRO_PANE_CACHE` — override the pane-list cache path (defaults to a
  per-user temp file).

## GitHub worktree links

With a GitHub issue or PR URL visible in any pane, **ctrl-click** it. Sesh-bro
extracts `owner/repo` and the number, resolves the title via `gh` (cached for
24h), and:

- focuses the existing workspace if its repo-qualified identity matches that
  issue — equal issue numbers in different owners/repositories stay distinct;
  legacy number-only labels are recognized only when Herdr's worktree/path
  data confirms the repository — or
- otherwise creates a **real `git worktree`** on branch `issue-409`,
  anchored to the repo checked out at `~/github/<repo>` (or `$HOME` if
  `~/github` doesn't exist) — the workspace opens on the **new worktree's
  own path**, not the main checkout — labelled
  `owner/repo#409 — Add git worktree support…`.

This is a real `git worktree add`-equivalent under the hood (herdr's
`worktree.create`), not just a labelled workspace pointed at your existing
checkout — ctrl-clicking two different issues on the same repo gives you two
independent working trees, not two workspaces sharing one. If the target
isn't a git repository, or worktree creation fails for any other reason,
sesh-bro falls back to today's plain workspace (same location, same label)
rather than failing the command outright — you'll see a one-line note on
stderr explaining the fallback.

If the guessed location is not actually a checkout of that repo, sesh-bro says
so and opens a plain workspace instead of creating a worktree there. The guess
is `~/github/<repo>`, falling back to `$HOME` — and on a machine with no
`~/github` whose owner has `git init`'d their home directory, that fallback is
a real repository. Creating a branch and a worktree inside somebody's dotfiles
because they clicked a link is not a trade worth making, so the checkout is
verified against the repo's remote (or, with no remote, its directory name)
before anything is created.

The link handler pattern is `^https://github\.com/[^/]+/[^/]+/(issues|pull)/[0-9]+$`.
You can also run it manually: `sesh-bro worktree https://github.com/.../issues/409`.

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| Picker is empty | Is `herdr` running? (`herdr workspace list`) Is `jq` installed? sesh-bro now errors loudly instead of silently showing nothing. |
| `fzf: command not found` | Install fzf: `brew install fzf` (or your package manager). |
| `herdr daemon is not responding` | Start/attach herdr (`herdr`) before opening the picker. |
| Preview shows nothing for agents | The agent's terminal may be empty; try a workspace row, or check `herdr agent read <id> --source visible --ansi` directly. |
| Worktree link does nothing | `gh` not installed/authenticated? Titles fall back to just the number. Check the ctrl-click modifier (Control on all platforms). |
| Keybind doesn't fire inside herdr | Reload config: `herdr server reload-config`. |

## FAQ

**Why fzf instead of a TUI?** fzf gives us a battle-tested fuzzy matcher,
previews, and reload bindings for free; it's also what sesh integrates with by
default.

**Does it work on Windows?** No — bash-based, `platforms = ["linux", "macos"]`.

**How do I update?** Reinstall from GitHub (`herdr plugin install`). There's no
`plugin update` in Herdr v1.

**Can I add more sources?** Not yet — workspaces, agents, and zoxide dirs are
the three. Tmux sessions could be added later (like sesh).

## Development

```sh
make lint    # shellcheck sesh-bro + test helpers
make test    # bats smoke tests (mock herdr, no live daemon needed)
make check   # both
```

Tests run against `tests/mock-herdr` and a stubbed `zoxide`, so they work
without a running Herdr. CI runs the same targets on GitHub Actions (Linux +
macOS). Releasing: tag `vX.Y.Z` matching `herdr-plugin.toml`; the release
workflow builds the GitHub Release from `CHANGELOG.md`.

## License

[MIT](LICENSE)
