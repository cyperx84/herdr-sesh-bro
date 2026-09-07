# sesh-bro feature demand — mining report (2026-08-11)

Scope: what to add during the Go rewrite that is high-demand AND cheap AND not already
done by another herdr picker. Sources: herdrdev/herdr Discussions (864, via GraphQL —
feature requests are triaged out of Issues into Discussions), every `topic:herdr-plugin`
picker/sessionizer repo, and joshmedeski/sesh (our direct inspiration).

## Already ours — ruled out as candidates

MRU `last` toggle (matches herdr#665, 13 upvotes) — NOTE (0.4.0): this was
listed here as already shipped and was not. `last` picked the highest-numbered
OTHER workspace, which is "previous" only when you have two. It became a real
MRU in 0.4.0 via the `[[events]]` hook (docs/BEHAVIOUR.md §10.8), blocked→working→done→idle sort, git
branch + dirty marker, GitHub issue/PR → workspace link handler with 24h-cached title,
`root`, blacklist, aliases-as-prefill, configurable sort_order, zoxide dirs with eza
preview.

## Build these three

### 1. Jump straight to the next blocked / needs-attention agent — no picker

- **Evidence:** herdr discussions #682 (6 upvotes, "Keybindings for next/previous blocked
  agent") and #778 (3 upvotes, "Default built-in shortcut to jump to the first blocked
  agent") — 4 distinct askers. #547 (3 upvotes) asks for the same thing as a filter. One
  asker wants blocked-OR-done: "an agent in the queue that has an update for me".
- **Cost:** trivial. We already fetch and filter agent status for `--blocked`; add a
  subcommand that focuses the first (or next, via a small cursor file — same pattern as
  the existing gh-title cache) match instead of opening fzf.
- **Differentiation:** no herdr picker does a *direct* jump. `jeffarese/herdr-bar` floats
  blocked/done to the top of a list you still have to browse; that is the closest.
- **Why:** core has left this open across two discussions, and herdr's own maintainer
  pointed a requester at the plugin layer for exactly this class of thing.

### 2. Close a workspace from inside the picker

- **Evidence:** `fullerzz/herdr-plugin-sesh` — the other explicit sesh-for-herdr port,
  i.e. the plugin we will be compared against — shipped it (issue #65).
- **Cost:** trivial. One free keybind (`^w ^e ^b ^x ^o ^/` are taken; `ctrl-q` is free),
  `workspace.close`, reload. Same shape as the existing reload bindings.
- **Caveat:** that thread reports a focus flicker on herdr <0.7.5 when the closed
  workspace is not the active one. Check the version floor before shipping.

### 3. Clone-and-connect + mkdir-and-connect (one change)

- **Evidence:** both are headline `sesh` features. Our `cmd_create` currently hard-errors
  when the path does not exist, so mkdir-connect is a one-line relaxation of that guard
  and clone is `git clone` then reuse the same path.
- **Cost:** trivial, well under an hour for both.
- **Honesty:** competitive parity, not novelty — fullerzz already ported clone-connect,
  and no herdr discussion asked for either by name. The justification is "a sesh-literate
  user notices these missing in the first five minutes", not upvotes.

## Queued next, not in the top three

**Browse open issues/PRs on the current repo → pick one → open it as a worktree
workspace.** Extends the link handler we already differentiate on. `tdi/herdr-worktree-from-pr`
exists as a whole dedicated plugin for it, and `andrewchng/herdr-sessionizer` has a
backlog PRD (#33). But that is builder-revealed demand, not upvote-counted — and it is
"small", not trivial. Right after the top three.

## The highest-demand thing we are NOT building — BUILT IN 0.4.0, see the note below

**Show how long each agent has been in its current state.** herdr discussion #707, 6
upvotes, 3 corroborating comments — one calling it "such a killer feature", another tying
it to real token waste from idle sessions. The strongest single demand signal in the whole
sweep.

Rejected on a hard technical fact, not taste: herdr's socket API exposes **no timestamp
for when a status began**. The schema was searched for `created_at`, `focused_at`,
`updated_at`, `_since`, `elapsed`, `duration`, `timestamp`, `last_focused` — zero hits,
including on `pane_agent_status_changed` itself. The only way to answer "how long has this
been blocked" is to subscribe to events and keep our own clock, which turns an on-demand
fzf script into a persistent daemon with its own state store. That is a footprint-ladder
jump we have no justification for.

Do not confuse this with herdr-bar's "running time" column — that is uptime since pane
creation, which *is* derivable, and is a different and much cheaper thing.

Same root cause, same rejection: true N-deep recency/frecency sort beyond our existing
single-slot `last` (#1407, #1886; `beyondlex/herdr-recent-navigator` builds it by running
a persistent event listener).

> **NOTE (0.4.0): the rejection above was wrong, and both items shipped.** The technical
> fact still holds — herdr exposes no status timestamp, and 0.8.2's schema confirms it
> (#3619 asks for one; #2034 asked and was closed). What was wrong is the inference that
> keeping our own clock means "a persistent daemon with its own state store".
>
> herdr's `[[events]]` manifest hook runs a plugin command *per event*, and — verified
> against 0.8.2 with a throwaway plugin — those hooks fire for every pane with **no
> per-pane registration**, unlike `events.subscribe`, whose status subscription demands a
> `pane_id`. So the cost is one short-lived process per state change, on the order of a
> handful per minute, not a resident listener. `record-event` writes
> `$HERDR_PLUGIN_STATE_DIR/state.json` under a lock and never exits non-zero.
>
> That is the whole footprint, and it is well below the ladder jump this section refused.
> Time-in-state badges and a real MRU `last` both ship on it (docs/BEHAVIOUR.md §10.8).
> The lesson worth keeping: "we would have to keep our own clock" was true, and
> "therefore we need a daemon" did not follow — the host already had a cheaper hook.

## Flagged, below the demand bar

`sesh`'s alias system — exact-match short-circuits fuzzy ranking so muscle memory never
drifts, plus a `/`-prefix mode that browses aliases only. Ours is only a query prefill.
Cheap, and nobody in the herdr ecosystem has it. But zero herdr discussions asked for it,
so it fails the demand bar on evidence. Inference only.

## No-signal searches, recorded so nobody repeats them

"sessionizer", "zoxide", "frecency" as literal terms: 0 discussion hits — people describe
the behaviour, never the word. herdr open *Issues* matching picker/fuzzy/switcher terms: 0,
confirming feature demand lives entirely in Discussions.
