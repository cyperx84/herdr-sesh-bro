# Sesh Bro

A [sesh](https://github.com/joshmedeski/sesh)-style fuzzy session picker for
[Herdr](https://herdr.io) — workspaces, agents, and zoxide directories in one
`fzf` popup with live terminal previews.

```
sesh> alpha                                       ▲ 40%
◆ alpha  1p/1t                                    │
● alpha  claude · Move 3D arcade to dedicated…    │
● beta   claude · Rename arcade route             │
▸ my-project  /Users/me/github/my-project         │
                                                  ▼
enter connect · ^w workspaces · ^e agents · ^b blocked · ^x dirs · ^o all · ^/ create
```

## Features

- **Three sources in one picker** — Herdr workspaces, agents (grouped by
  status: blocked → working → done → idle), and zoxide directories, deduped
  against directories already open in a Herdr pane.
- **Live previews** — the focused workspace's or agent's terminal output is
  rendered in the preview pane with ANSI colors preserved; directories show an
  `eza`/`ls` listing and any README.
- **Connect-or-create** — workspaces and agents are focused directly; picking
  a directory focuses the existing workspace containing it, or creates a new
  workspace there (`zoxide add` + `herdr workspace create --focus`).
- **Filter sources from inside fzf** — `^w` workspaces / `^e` agents / `^b`
  blocked / `^x` dirs / `^o` all reload the list; `^/` creates a workspace
  from a directory.
- **Current-first ordering** — the current workspace and its agents float to
  the top, then blocked → working → done → idle.
- **Herdr plugin integration** — installs as a plugin with a popup pane and
  workspace-context actions.

## Requirements

- [Herdr](https://herdr.io) (>= 0.7.4)
- [fzf](https://github.com/junegunn/fzf)
- [jq](https://jqlang.github.io/jq/)
- [zoxide](https://github.com/ajeetdsouza/zoxide) (for directory entries)
- Optional: `eza` (prettier dir previews), `bat` (README preview)

## Install

```sh
herdr plugin link ~/github/herdr-sesh-bro
```

The picker is then available as the **Sesh Bro** popup pane, and these actions
are added to workspace contexts:

- **Open sesh-bro picker**
- **Open sesh-bro picker (agents only)**
- **Open sesh-bro picker (blocked agents)**

You can also run it directly from a terminal:

```sh
sesh-bro picker          # interactive fzf picker (default command)
sesh-bro list            # picker candidates as tab-separated rows
sesh-bro list --json     # machine-readable candidates
sesh-bro connect TYPE TARGET
sesh-bro create [PATH]   # create a workspace for a dir (default: current dir)
sesh-bro preview TYPE TARGET
sesh-bro open            # open the picker popup via the Herdr plugin API
```

## Usage

| Key | Action |
|-----|--------|
| `enter` | connect (focus workspace/agent, or create a workspace for a dir) |
| `^w` | reload: workspaces only |
| `^e` | reload: agents only |
| `^b` | reload: blocked agents only |
| `^x` | reload: directories only |
| `^o` | reload: all sources |
| `^/` | create a new workspace from a directory |
| `esc` | exit |

> The keybinds deliberately avoid `^a` (Herdr's prefix) and `^h/j/k/l`
> (vim-herdr-navigation / tmux pane keys) so they never fight your editor
> or window-manager chords.

### Flags

`sesh-bro list` (and `picker`) accept:

- `--workspaces` / `--agents` / `--dirs` — limit sources
- `--blocked` / `--working` / `--done` / `--idle` — filter agents by status
- `--hide-current` — drop the current workspace and its agents from the list
- `--json` — machine-readable output (`list` only)

### Environment

- `HERDR_BIN_PATH` — the `herdr` binary to use (defaults to `herdr` on PATH;
  also how the test suite points at its mock).
- `HERDR_PLUGIN_ID` / `HERDR_PLUGIN_CONTEXT_JSON` — provided by Herdr when the
  plugin runs; `SESH_BRO_ARGS` carries flags into a plugin-opened picker.

## Development

```sh
make lint    # shellcheck sesh-bro + test helpers
make test    # bats smoke tests (mock herdr, no live daemon needed)
make check   # both
```

Tests run against `tests/mock-herdr` and a stubbed `zoxide`, so they work
without a running Herdr. CI runs the same targets on GitHub Actions.
