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
enter connect · ^w workspaces · ^e agents · ^b blocked · ^x dirs · ^t worktrees · ^o all · ^s star · ^y reply · alt-x close · ^/ create
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
- **Close a workspace from the picker** — `alt-x` closes the highlighted
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
sesh-bro next            # focus the next agent needing attention (blocked, then done)
sesh-bro prev            # ...the previous one
sesh-bro counts          # one line: how many agents are blocked/working/done/idle
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
| `^t` | reload: git worktrees you have not opened |
| `^o` | reload: all sources |
| `^s` | pin the highlighted agent to the top of its status group |
| `^y` | send canned reply 1 to the highlighted agent (blocked agents only) |
| `alt-x` | close the selected **workspaces** (no-op on agent/dir rows; refuses the current workspace) |
| `^/` | create a new workspace from a directory |
| `esc` | exit |

The picker is `--multi`: `tab` selects, and `alt-x` closes everything selected
after showing you the list by label and asking. `enter` on a multi-selection
connects to the **first** selected row, which is the only sane
single-destination reading but will surprise anyone with muscle memory from
other fzf tools.

`^s` pins an agent to the top of **its own status group**, not to the top of
the list — a pinned idle agent leads the idle ones and still sits below every
blocked agent, so a pin can never bury something that needs you. Named agents
are pinned by name and survive their pane being recreated; an unnamed agent is
pinned by pane id and the pin dies with the pane.

`^y` sends the first canned reply (`yes` out of the box) to the highlighted
agent, and refuses any agent that is not blocked, so pressing it on the wrong
row does nothing. Edit the replies one per line in `replies.txt` beside the
state file. There is no key that answers every blocked agent at once, on
purpose.

## Other sessions

Set `SESH_BRO_ALL_SESSIONS=1` (or pass `--all-sessions`) and the picker gains a
row for every other running herdr session, plus one per agent inside them.

They are **read-only**, and that is structural rather than cautious. A herdr
session is a daemon: N sessions are N server processes with N sockets, and no
herdr API call takes a session parameter, so every call lands on whichever
socket it dialled. Prompting a foreign agent would not fail — it would act on
the local session instead, possibly on a same-named agent of yours, and report
success. So `prompt`, `reply`, `star`, `close`, `connect`, `read` and `explain`
all refuse a foreign target with exit 2 and name the session it is in.

`enter` on a foreign row does the one thing that works: opens a terminal
running `herdr session attach`. Configure it with `SESH_BRO_ATTACH_CMD`, a
shell command containing `{session}`. macOS defaults to Ghostty. Other
platforms must set it — terminal emulators vary too much for a default to be
right, and a wrong one opens nothing while looking like it worked.

Foreign rows refresh on a timer rather than by subscription
(`SESH_BRO_FOREIGN_INTERVAL`, default `5s`, `0` disables). It is the only poll
in sesh-bro. Following other sessions properly would mean a held connection per
foreign agent, for rows you cannot act on.

> The keybinds deliberately avoid `^a` (Herdr's prefix) and `^h/j/k/l`
> (vim-herdr-navigation / tmux pane keys) so they never fight your editor
> or window-manager chords.

Every keybind above is overridable — see `SESH_BRO_KEY_*` in
[Configuration](#configuration). The picker's header hint always reflects
whatever key is actually bound, not the defaults shown here.

`alt-x` only closes workspace rows. Selecting an agent or zoxide-directory row
and pressing `alt-x` prints a message and does nothing — it never errors out
of the picker. Closing the workspace the picker itself is running in is
refused the same way, since that would kill the picker mid-action. (The close
key is deliberately a modifier chord, not a bare ctrl key: ctrl-q is one of
fzf's four default abort keys, so the "get me out of here" keystroke must
not be the one that silently closes a workspace.)

### Flags

`sesh-bro list` (and `picker`) accept:

- `--workspaces` / `--agents` / `--dirs` — limit sources
- `--blocked` / `--working` / `--done` / `--idle` — filter agents by status
- `--hide-current` — drop the current workspace and its agents from the list
- `--json` — machine-readable output (`list` only)

## How long has it been waiting

Blocked, done and idle rows carry their age:

```
● claude  claude · Progress check · 9m
● review  codex · Waiting for approval · 42s
```

herdr exposes no timestamp for a status change, so sesh-bro times it itself
from a `[[events]]` hook — herdr runs a plugin command per event, and those fire
for every pane with no registration, so this costs one short-lived process per
state change rather than a background daemon. State lives in
`$HERDR_PLUGIN_STATE_DIR/state.json`.

Two deliberate rules: `done` → `idle` does **not** reset the clock, because
glancing at a finished agent should not erase how long it waited; and if the
recorded status disagrees with the live one, the badge is omitted rather than
shown from a stale start time.

Badges appear on blocked, done and idle rows — never on working ones, where an
age is noise competing with the rows that want you — and never in `--json`.
Idle matters as much as blocked here: a session left three hours is a prompt
cache quietly expiring.

The same recording makes `sesh-bro last` a real most-recently-used jump. It
used to focus the highest-numbered *other* workspace, which is "previous" only
when you have two of them.

## The same key opens and closes it

Whatever chord you bind to `sesh-bro.open` is now a toggle. herdr refuses to
stack a second popup, and sesh-bro reads that refusal as "you meant close".
Escape still works.

## The picker updates while it is open

Open the picker and leave it open: when an agent changes state, the list
re-sorts and the counts change underneath you, without a keypress and without
polling. sesh-bro subscribes to herdr's events and pushes a reload into the
running fzf through its `--listen` socket, so a quiet session costs nothing.

Your cursor stays on the agent you were looking at. fzf tracks the row by its
target rather than its position, which is what makes a list that re-sorts under
you usable at all.

The counts line is pinned at the top and updates with the list.

This needs **fzf 0.66+** for the push, **0.71+** for cursor tracking, and
**0.72+** for the footer. Everything is detected and every absence degrades
rather than fails — on an older fzf the picker opens
exactly as it did before, showing the session as of the moment you pressed the
key. `sesh-bro startup` reports which features your fzf has.

## One keypress: jump to whoever needs you

```sh
sesh-bro next    # focus the next agent needing attention
sesh-bro prev    # ...the previous one
```

"Needing attention" is blocked, then done — herdr's two states meaning "has
something for you that you have not seen". Blocked is an approval or question
prompt; done is the idle state reached by unseen background work, which stays
done until you look at the tab. Working and idle are skipped because neither is
waiting on you, and `unknown` is skipped because herdr could not classify it.

The cycle is stateless: it reads the live session each time, skips the pane you
are already on, and wraps. Press it repeatedly to drain the queue. With nothing
waiting it says `nothing needs you` rather than doing nothing silently.

A toast names the target and how much else is queued (`→ builder blocked (+2
more)`). It is sent **before** focusing, because herdr suppresses a
notification aimed at the tab you are already looking at, which focusing is
about to make it.

Bind it in `~/.config/herdr/config.toml`:

```toml
[[keys.command]]
key = "alt+."
type = "plugin_action"
command = "sesh-bro.next"

[[keys.command]]
key = "alt+,"
type = "plugin_action"
command = "sesh-bro.prev"
```

**Why not herdr's built-in `next_agent`?** It cycles in *panel* order, so it
only follows urgency if you also set `agent_panel_sort = "priority"` — and that
reorders the panel, which breaks the stable positions `focus_agent`'s 1–9
indexes depend on. herdr discussion
[#2761](https://github.com/herdrdev/herdr/discussions/2761) puts it exactly:
"stable numbers or attention-ordered cycling — I can have either, not both."
sesh-bro reorders nothing, so you can have both.

## Zero keypresses: agent counts in the tab bar

The picker costs a keystroke, but the question you ask most often is not
"which session" — it is "does anything need me at all". `counts` answers that
without being asked:

```sh
sesh-bro counts          # 🔴2 🟡5 🔵1 ⚪12   (zero-count states omitted)
sesh-bro counts --ansi   # ● 2 blocked · ● 5 working · ● 1 done · ● 12 idle
sesh-bro counts --json   # {"blocked":2,...} — every state, zeros included
sesh-bro counts --all    # keep zero states, for a fixed-width display
```

It is one line, one `session.snapshot` call, and never empty — an empty session
prints `no agents`, because a blank tab-bar entry reads as a broken command.

herdr 0.8.2 can put that line in the tab bar and refresh it on a timer. Add to
`~/.config/herdr/config.toml`:

```toml
[ui]
tab_bar_right = [
  { type = "command", command = "/absolute/path/to/sesh-bro counts", interval_seconds = 2 },
]
```

Use an absolute path: herdr runs the command through `/bin/sh -lc`, and takes
only the **last line** of stdout. Then `herdr config check && herdr server
reload-config`.

While you are in that file, two herdr settings do most of the same work for
free and are off by default:

```toml
[ui]
agent_panel_sort = "priority"   # agents panel becomes an attention queue
status_indicators = "symbols"   # distinct glyph per state, not colour-only dots

[ui.toast]
delivery = "herdr"              # default is off, so background agents announce nothing
```

The same line drives a SketchyBar item or a shell prompt — export
`HERDR_SOCKET_PATH` (`~/.config/herdr/herdr.sock`) if you call it from outside
a herdr pane.

## Configuration

Sesh-bro reads `SESH_BRO_*` environment variables, which Herdr's `[config]`
schema exposes. Set them in your shell, or via Herdr's config UI:

| Env var | Default | Meaning |
|---------|---------|---------|
| `SESH_BRO_PREVIEW_WIDTH` | `60%` | Preview pane width |
| `SESH_BRO_PREVIEW_ENABLED` | `1` | `0` disables the preview pane |
| `SESH_BRO_HIDE_CURRENT` | `0` | `1` hides the current workspace + its agents |
| `SESH_BRO_DIR_SOURCES` | `1` | `0` disables zoxide directory entries |
| `SESH_BRO_ATTENTION_FIRST` | `1` | `0` keeps the old current-workspace-first order instead of hoisting blocked/done agents |
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
| `SESH_BRO_KEY_WORKTREES` | `ctrl-t` | Reload: unopened git worktrees only |
| `SESH_BRO_KEY_STAR` | `ctrl-s` | Pin the highlighted agent |
| `SESH_BRO_KEY_REPLY` | `ctrl-y` | Send canned reply 1 to a blocked agent |
| `SESH_BRO_KEY_ALL` | `ctrl-o` | Reload: all sources |
| `SESH_BRO_ALL_SESSIONS` | `0` | Show other sessions' read-only rows |
| `SESH_BRO_FOREIGN_INTERVAL` | `5s` | How stale foreign rows may get; `0` disables them |
| `SESH_BRO_ATTACH_CMD` | Ghostty on macOS | Terminal command to attach a session; must contain `{session}` |
| `SESH_BRO_ICON_SESSION` | `▣` | Glyph for a session row |
| `SESH_BRO_ICON_RAGENT` | `○` | Glyph for an agent in another session |
| `SESH_BRO_KEY_CLOSE` | `alt-x` | Close the highlighted workspace |
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
make lint    # shellcheck scripts/build.sh
make gotest  # go vet ./... && go test ./...
make check   # both
```

Tests run against a fake herdr socket server (`internal/herdrx/herdrtest`,
which speaks the same wire protocol as `github.com/cyperx84/herdr-api`'s
client), so they work without a running Herdr. CI runs the same targets on
GitHub Actions (Linux + macOS). Releasing: tag `vX.Y.Z` matching
`herdr-plugin.toml`; the release workflow builds the GitHub Release from
`CHANGELOG.md`.

## License

[MIT](LICENSE)
