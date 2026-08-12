# sesh-bro competitive demand mining (2026-08-12)

Crawled the six direct competitors and three adjacent plugins, open and closed
issues plus discussions, and herdrdev/herdr core discussions.

**Method note that changes the answer.** Most of these repos file their own
roadmaps as issues — fullerzz, andrewchng and lmilojevicc author the majority of
their own `feat:` threads as PRD documents. Counting those as demand would
inflate everything. Filtering `author != repo-owner` is what separates "somebody
wants this" from "the maintainer plans it". Only 4 of 10 repos have Discussions
on, and those are nearly empty — unlike herdr core, this plugin layer is too
young to have triaged anything into them.

## Build these three

### 1. Close / remove a workspace from the picker

Four independent repos converge on it: fullerzz shipped it (issues/65),
herdr-bar ships `⌦`-to-close, Navigator has it *and two external bug reports
about it* (nikbrunner, issues/16 and /13 — evidence people actually use the
flow), Sessionizer has a bulk worktree-close PRD (issues/1), and herdr core has
an idea thread (discussions/2415).

We have no close action at all. Cost is trivial: one more `fzf --bind` calling
`herdr workspace close <id>`, reusing the ids already resolved for connect.
`--multi` gives bulk for free.

### 2. Configurable picker keybinds

Navigator issues/26 — opened by aresler, **seconded by a second person**
(MrDwarf7) who specs the exact shape wanted. Still open there.

Unmet by *every* competitor checked, which makes this the one item where we
could lead rather than catch up. Our binds are already `--bind` flags; wrapping
them in `SESH_BRO_KEY_*` matches the existing `SESH_BRO_ICON_*` pattern.

### 3. Real `git worktree` materialisation for the branch path

**Flagged as inference, not demand** — zero third-party issues ask us for this.
But our own README carries a disclaimer that "worktree" here means an issue
workspace and *not* a git worktree, while Sessionizer's entire plugin is real
worktree-per-branch. That is a confusion we built into our flagship feature and
a reviewer or a competitor comparison will surface it.

Scope to the plain-branch path. Treat PR-materialisation as a stretch.

## Do NOT build: declarative TOML session bootstrap

The highest-engagement feature class in the whole sweep — Sessionizer issues/5
drew 8 comments, the most of anything found, and it is not purely self-authored
(Navigator issues/2 is an external report about `[[tabs.panes]]` splits
breaking). Two direct competitors built substantial subsystems around it.

Reject anyway, on cost and fit. It is a TOML schema plus pane split-ratio maths
plus startup-command ordering plus per-repo overrides — what two maintainers
each spent weeks on, nowhere near the "under a day" bar. And sesh-bro's own FAQ
already draws this line: its job is pick-and-connect, not session-layout
bootstrapping. Building it makes us a second Sessionizer instead of a fast
picker.

## Positioning

**The edge:** the only one of the six merging workspaces + agents (state-grouped
blocked→working→done→idle) + zoxide dirs in one fzf list with live
ANSI-preserving previews and a working GitHub-link handler.

**A structural advantage worth stating out loud:** Navigator has burned four
separate issues on herdr theme-sync bugs (issues/3, /5, /20, /28; two external
reporters), and recent-navigator has the same class again. sesh-bro cannot have
that bug — shelling out to fzf means the terminal's live ANSI palette paints it,
so there is no palette table to keep in sync. Not a feature to build; a
consequence of the fzf decision.

**Genuinely behind:** no close action, fixed keybinds, no real git worktree
despite advertising one, herdr-only with no tmux fallback (seshagy does both),
no shell completions (seshagy ships them).

## Cheap parity, low priority

Shell completions (seshagy ships, zero external demand) and `sesh-bro clone`
(fullerzz ships, zero demand thread). Both trivial; neither urgent.

## Rejected on fit

"Expose all herdr actions in the picker" — JanTvrdik/herdr-command-palette
issues/3, 3 reactions, the highest external reaction count in the sweep. Real
demand, wrong tool: it asks sesh-bro to become a command palette over every
herdr and plugin action, which is what that plugin already is.

## Searched, found nothing

Zero hits for "sessionizer" or "zoxide" in herdr core discussions — that demand
lives entirely in the plugin layer.
