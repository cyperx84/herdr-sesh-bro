# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed
- The picker toggle works again on herdr 0.9.0. herdr reworded its refusal to
  stack a second popup — `plugin_pane_open_failed` / "popup already open"
  became `ui_busy` / "a popup pane is already open" — and `open` recognised
  only the old wording, so the close branch never ran. The popup stayed up
  permanently, every later press of the chord errored against it, and — because
  an open popup holds the UI — every other herdr keybinding went dead with it.
  A one-string mismatch presented as "the update broke my keybinds". Both
  wordings are now matched, so the toggle works on 0.8.x and 0.9.0 alike.

## [0.6.0] - 2026-09-07

The theme: acting on agents, not just finding them. Everything here is a
command before it is a keybind, so a driving agent gets it on the same terms a
human does.

### Added
- **`prompt`** sends text to one or more agents as if typed. Its wait set
  includes `blocked`, which is the point: the agent you most want to answer is
  the one sitting on an approval prompt. `--all-blocked` targets every waiting
  agent, `--from-file` takes the picker's multi-selection, and `--text -` reads
  stdin. Several targets report one result each, and one failure never abandons
  the batch.
- **`reply`** is `prompt` with the typing removed — canned answers, one per
  line in `replies.txt` beside the state file, sent with `ctrl-y` from the
  picker. It refuses any agent that is not blocked, exiting 3, so pressing the
  key on the wrong row does nothing. That refusal is what makes a one-keypress
  answer safe enough to sit beside the filter keys.
- **`star`** pins an agent to the top of **its own status group**, on `ctrl-s`.
  Within the rank, never above it: a pinned idle agent leads the idle ones and
  still sits below every blocked agent, so a pin cannot bury something that
  actually needs a human. Named agents are pinned by name and survive their
  pane being recreated; an unnamed agent is pinned by pane id and the pin dies
  with the pane. sesh-bro will not rename an agent on your behalf to make a pin
  durable.
- **Multi-select and a close that shows you what it will destroy.** `tab`
  selects; `alt-x` prints every row it will close, by label, and reads y/N from
  `/dev/tty`. Non-workspace rows are reported rather than silently dropped, and
  headless use requires `--yes`, which is mandatory when stdin is not a tty.
  Note that `enter` on a multi-selection connects to the **first** row.
- **`SESH_BRO_KEY_WORKTREES` and `SESH_BRO_KEY_STAR`** are now read. Both keys
  existed in 0.5.0 and neither was overridable — the config never looked at the
  variables, so any value set was ignored in favour of the default.
- **`list --view-dir DIR`** renders whichever view a picker runtime directory's
  marker says is on screen. Internal plumbing for the binds below.

### Fixed
- **A star now appears the moment you press the key.** The bind reloaded
  through `rows`, which cats the row file rendered *before* the toggle.
  Starring moves no herdr state, so no event fires, so nothing ever re-rendered
  those files: the pin stayed invisible until some unrelated event happened to
  repaint. Close and create moved to the same fresh-render bind, one step
  weaker — they do fire events, but the reload ran first and showed the closed
  workspace still sitting there until the push landed. The filter keys stay on
  the cheap pre-rendered path; they change what is on screen without changing
  what is true.
- **`stars.json` grew forever.** `stars.Prune` had no caller. It now runs in
  `startup` beside the attention prune, judging agent-kind stars against live
  agent *names* rather than the pane map.
- **`WithStars` sorted with a comparator that is not a strict weak ordering.**
  It reported every cross-rank pair equal in both directions while ordering
  same-rank pairs, so equality was not transitive and the sort was free to emit
  any permutation. Replaced with a stable partition inside contiguous runs of
  equal status, which also stops a star crossing the current-workspace
  boundary into the group above it.

### Not built, on purpose
- **Approve-all / auto-yes.** Recorded with its reasoning in
  `docs/FEATURE-DEMAND.md`. A blanket yes removes the confirmations coding
  agents implement on purpose, for every agent on the machine at once,
  including the ones you are not watching. Answering one agent you are looking
  at is a different act from a policy that answers all of them.

## [0.5.0] - 2026-09-07

The theme: sesh-bro became drivable by another coding agent, and gained a
fourth source.

### Added
- **`sesh-bro --skill`** prints `docs/AGENTS.md` — the commands, exit codes and
  output shapes another program needs. A coverage test walks the command table
  against it, so a command cannot exist without being documented.
- **`agents`** — `list --agents` without the flag-order trap. In the long form a
  status flag clears earlier source flags, so `--blocked --agents` and
  `--agents --blocked` differ and the wrong order returns a plausible wrong
  answer rather than an error.
- **`wait --target A --until blocked,done`** blocks until an agent settles,
  using herdr's server-side wait rather than a poll. Exit 3 on timeout.
- **`read TARGET --source recent-unwrapped`** prints an agent's scrollback
  rather than just the visible viewport — typically twice as much.
- **`next --dry-run`** reports who needs attention without focusing them.
  Focusing a `done` agent marks it seen, so a survey that focused would destroy
  the signal it was surveying.
- **`--jsonl`** on row commands: one compact object per line. `--json` emits
  concatenated pretty objects that `json.loads` cannot parse in one call, and
  is pinned that way for compatibility.
- **Git worktrees as a fourth source** (`^t`). Shows worktrees on disk with no
  workspace open — anything already open is skipped, since the workspace source
  lists it. Enter opens one.
- **`last` and `root` are bindable plugin actions** (closes #2). Both worked as
  commands and neither was declared in the manifest, so `sesh-bro.last` had
  nothing to bind to.
- **Exit codes an agent can branch on**: 0 success, 1 runtime failure, 2 usage
  error, **3 nothing matched**. Without the last one, a quiet queue and a broken
  daemon are indistinguishable without reading English.

### Changed
- sesh-bro **finds the running herdr session itself** via `herdr session list`,
  so every command works from an ordinary shell. Previously anything outside a
  herdr-spawned pane failed on a missing environment variable. It refuses to
  guess when several sessions run and none is default, because every herdr call
  is scoped to the socket it dialled and attaching to the wrong one is
  undetectable downstream.
- Dispatch and usage generate from one command table, so they cannot disagree.
- `next` exits 3 rather than 0 when nothing needs attention.


### Changed
- **The live picker stopped paying for itself.** Opening it read the session
  twice and every herdr event re-ran the whole source load — a fresh git cache
  (so `git status` per workspace cwd), a zoxide subprocess, and a liveness
  probe, for data a pane changing state cannot have touched. It now reads once
  at open and reuses what an event cannot invalidate. The git cache still
  expires after 5s; the zoxide list does not, because a directory ranking that
  shifts mid-picker is noise rather than news.
- `startup` reports the fzf version and which live features that build
  supports. It lands in `herdr plugin log list`, which is where someone asking
  "why isn't my picker updating?" actually looks.
- A re-render prefers the daemon's live focus over `HERDR_WORKSPACE_ID`, which
  is captured once when the popup spawns. A picker left open while you moved
  around kept sorting the launch workspace first while the `· current` label
  followed you. One-shot `list` keeps the env var.

### Fixed
- **Runtime directories leaked.** The cleanup only ran on a normal return, so
  herdr tearing down the popup — or any kill — left the socket and row files
  behind, and nothing ever swept them. There is now a signal handler plus a
  sweep of dead siblings, which believes a dead pid only after a minute because
  pids are recycled.
- **A lost-update race in the recorded state.** `Load` was unlocked while
  `Save` locked only the write, so two event hooks could both read, both write,
  and lose one change while both writes reported success. `attention.Update`
  holds the lock across the whole read-modify-write.
- Recorded panes are pruned at startup. Nothing removed them before — the hook
  only ever adds — so the file grew an entry for every pane the machine had
  ever run an agent in.
- The first render no longer pushes to fzf's listen socket before fzf has
  created it. That push always failed and was always swallowed.

### Added
- `docs/MULTI-SESSION.md` — why cross-session focus is structurally impossible,
  and what a picker can do instead.

## [0.4.0] - 2026-09-07

### Added
- **Time-in-state badges.** Blocked and done rows show how long they have been
  waiting (`claude · Progress check · 9m`). herdr exposes no status timestamp —
  discussions #707 (7 upvotes, "such a killer feature") and #3619 both ask for
  one — so sesh-bro times it from a `[[events]]` hook, which herdr invokes per
  event for every pane with no per-pane registration. That is one short-lived
  process per state change rather than a resident daemon. `done` → `idle` does
  NOT reset the clock, since looking at a finished agent should not erase how
  long it waited; a recorded status that disagrees with the live one yields no
  badge rather than a wrong one.
- **`last` is a real most-recently-used jump.** It previously focused the
  highest-numbered OTHER workspace, which is "previous" only when you have two
  — `docs/FEATURE-DEMAND.md` listed this as already shipped and it was not. The
  same hook records `workspace.focused`, so there is now a real history; with
  none recorded yet the old rule remains the fallback.
- **The picker updates while it is open.** A subscription to herdr's events
  drives a re-render and a `reload` pushed into the running fzf over its
  `--listen` unix socket — no polling, so a quiet session costs nothing. The
  cursor stays on the agent you were looking at (`--track --id-nth` follows the
  row's target, not its index), the pinned counts line updates with the list,
  and filter keys now read a pre-rendered file instead of re-executing the
  binary. Requires fzf 0.66+ for the push, 0.71+ for tracking, 0.72+ for the
  footer; every feature is detected and every absence degrades to the 0.3.0
  picker rather than failing.
- **`next` / `prev`** — one keypress to the agent that wants you, no picker in
  between. The set is blocked then done, ordered by most recent state change;
  the cycle is stateless (read the snapshot, skip the focused pane, wrap), so
  it stays correct when you navigate by other means between presses. A toast
  names the target and the queue depth, sent *before* focusing because herdr
  suppresses notifications aimed at the tab focus is about to activate. Nothing
  waiting says `nothing needs you` rather than failing silently. Registered as
  the `sesh-bro.next` / `sesh-bro.prev` plugin actions for `[[keys.command]]`.
  herdr's own `next_agent` cycles in panel order, which only tracks urgency if
  `agent_panel_sort = "priority"` — and that reorders the panel, breaking
  `focus_agent`'s stable 1–9 positions (herdr discussion #2761). This reorders
  nothing. Demand: herdr discussions #682, #778. Cycle design after
  `milkyskies/herdr-attention`.
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
- **`open` toggles.** Pressing the bound chord while the picker is open now
  closes it, instead of surfacing herdr's "popup already open" refusal while
  the popup sits there. herdr runs as a child process rather than via `exec` so
  that failure is observable; every outcome other than the toggle forwards
  herdr's output and exit code verbatim.
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
- **The picker ate a row on several reload paths.** fzf is told
  `--header-lines=1`, so every stream it loads must begin with a header row,
  but three reload binds re-executed `list` without `--header` — close, create,
  and the fallback used when fzf is too old for `--listen`. The first real
  candidate silently became an unselectable header, and on an fzf below 0.66
  that happened on *every* filter keypress, which falsified this release's own
  claim that an older fzf degrades cleanly. Reloads now route through a hidden
  `rows` subcommand that reads the view marker and cats the matching
  pre-rendered file, so one place decides what a reload emits. That also fixes
  a second defect in the same binds: close and create reloaded the default
  all-sources view regardless of the active filter and left the view marker
  untouched, so the next live push switched the list back.
- **Idle rows never showed their age.** `done → idle` deliberately preserves an
  agent's start time — those are one underlying state, and `idle` only means
  you have now looked at it — but badges were gated on blocked and done, so
  that preserved clock rendered nowhere. Idle rows are now badged; working rows
  still are not. herdr discussion #707's complaint is precisely that an idle row
  left three hours looks identical to one left thirty seconds.
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
