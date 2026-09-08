# sesh-bro for agents

sesh-bro is a picker for [herdr](https://herdr.io), a terminal multiplexer that
runs coding agents in panes. Every picker capability is also a command, so you
can ask it what is happening and act on it without a human at a keyboard.

**sesh-bro never acts on its own.** It does not steal focus, does not open
itself, and does not nag. Everything below happens because you asked.

## The two things to know first

**Exit codes carry the answer.** Branch on them; do not parse messages.

| Code | Meaning |
|---|---|
| 0 | It worked |
| 1 | Runtime failure — daemon unreachable, herdr rejected the call |
| 2 | Your mistake — unknown flag, missing argument, bad status name. Retrying unchanged will not help |
| 3 | It worked and the answer is **nothing** — no agent needs attention, the wait timed out |

Code 3 is the one that matters. Without it you cannot tell a quiet queue from
a broken daemon without reading English.

**Use `--jsonl`, not `--json`, for rows.** `--json` emits concatenated
pretty-printed objects, which `json.loads` will not accept in one call; it is
that shape for backward compatibility and is not going to change. `--jsonl`
gives one compact object per line:

```sh
sesh-bro agents --blocked --jsonl | while read -r line; do ...; done
```

Action commands (`wait`, `next`, `prev`) use `--json` and emit exactly one
object, on one line, valid even on failure:

```json
{"ok":true,"command":"wait","results":[{"pane_id":"w1:p2","status":"done"}],"error":null}
```

`results` is always an array, never null. `error` is null on success.

## You do not need to be inside herdr

sesh-bro finds the running session itself, so these work from any shell. If
several sessions are running and none is marked default it refuses rather than
guessing, because every herdr call is scoped to the socket it dialled and
attaching to the wrong one is undetectable downstream.

## Commands

### Find out what is happening

```sh
sesh-bro counts --json          # {"blocked":2,"working":1,"done":0,"idle":5,"unknown":0}
sesh-bro agents --blocked --jsonl
sesh-bro agents --done --jsonl
sesh-bro list --jsonl           # workspaces, agents, directories, worktrees
```

`agents` exists because `list --agents --blocked --jsonl` has a flag-order trap:
a status flag clears earlier source flags, so `--blocked --agents` and
`--agents --blocked` differ, and the wrong order returns a plausible wrong
answer rather than an error. `agents` handles that for you.

Agent statuses are herdr's: `blocked` (waiting on a human — an approval or a
question), `working`, `done` (finished work nobody has looked at yet), `idle`
(finished and seen), `unknown` (herdr could not classify it — **not** proof of
completion).

Blocked and done are the two that want a human. Blocked, done and idle rows
carry how long they have been that way, e.g. `claude · Fix the parser · 9m`.

### Why is it blocked?

```sh
sesh-bro explain builder         # e.g. "blocked · bash_permission_prompt"
sesh-bro explain builder --json
```

A `blocked` status says an agent wants a human. It does not say whether it is
waiting on a bash command approval, an MCP elicitation, or a question inside a
dynamic workflow. Those are three different asks with three different
urgencies, and they render identically. herdr already knows which, because that
is how it decided: it runs a prioritised rule set against what the pane shows,
and one rule wins. `explain` reports the winner.

Branch on the `rule` field, not on the human line. `region` says which part of
the screen matched, and `manifest_version` identifies the ruleset — rule names
are only meaningful relative to one, and herdr updates its manifests from a
remote.

Two things to know before you rely on it:

- The rules only ever produce `working`, `blocked`, `idle` or `unknown`. There
  is no `done` rule: herdr derives `done` above this layer as "idle with work
  you have not looked at". So a `done` agent explains as `idle`, and both are
  right.
- `agent.explain` arrived in herdr 0.9.0. Against an older daemon this exits 1
  with the method error rather than pretending there is nothing to say, so you
  can tell "this herdr cannot answer" from "herdr looked and found nothing",
  which exits 3.

### Watch and listen

```sh
sesh-bro wait --target builder --until blocked,done --timeout 300s --json
sesh-bro read builder --source recent-unwrapped
```

`wait` is a real server-side block, not a poll. Exit 3 means it timed out —
that is an answer, not a fault.

`read` defaults to the visible viewport. Use `--source recent-unwrapped` when
you want what the agent actually said rather than what currently fits on
screen; it typically returns twice as much.

### Act

```sh
sesh-bro next                   # focus the agent that most needs a human
sesh-bro next --dry-run --json  # ...or just say who that is, without moving
sesh-bro prev
sesh-bro connect agent builder  # focus a specific agent
sesh-bro connect dir /path      # focus or create a workspace there
sesh-bro create /path
sesh-bro close workspace w3     # irreversible
sesh-bro last                   # back to the previous workspace
sesh-bro root                   # workspace for the current git root
```

`next` exits 3 when nothing needs attention. It skips the pane you are already
on, so calling it repeatedly drains the queue rather than sticking.

Use `--dry-run` when surveying: it reports the agent it would pick without
focusing anything. That distinction matters — see the note about `done` below.

Focusing a `done` agent marks it seen and it becomes `idle`. If you are
surveying rather than intervening, use `read` — looking does not clear the
marker, focusing does.

`close` cannot be undone. It closes the workspace and every agent in it.

### Answer an agent that is waiting on you

```sh
sesh-bro prompt builder --text "yes, go ahead"   # send text as if typed
sesh-bro prompt --all-blocked --text "continue"  # every blocked agent at once
sesh-bro prompt builder --text - < answer.txt    # read the text from stdin
sesh-bro prompt builder --text "run the tests" --wait
```

`prompt` is how you unblock an agent without taking its terminal. The text is
delivered as if a human typed it and pressed enter, so it answers an approval
prompt or a question exactly the way that agent expects.

With several targets it reports one result per target and never abandons the
batch because one failed: an agent whose pane has gone still leaves the others
prompted. `--json` gives you that per-target result to branch on.

`--wait` blocks until the agent settles again, which turns "send and hope" into
"send and know". Without it the command returns as soon as the text is
delivered. herdr reports a `agent_prompt_stalled` condition when an agent takes
the text but nothing in its lifecycle moves within five seconds; that is
surfaced as a failure for that target, not silently swallowed.

There is deliberately no approve-everything flag. Answering one agent you are
looking at is a different act from a standing policy that says yes to every
approval prompt on the machine, which would defeat the confirmations those
agents implement on purpose.

### Canned replies

```sh
sesh-bro reply builder        # send canned reply 1 ("yes" by default)
sesh-bro reply builder 2      # ...reply 2
sesh-bro reply --list         # what the canned replies are
```

`reply` is `prompt` with the typing removed, and it is bound to a key in the
picker so a human can answer a waiting agent without leaving the list.

It refuses any agent that is not blocked, exiting 3. A canned answer is an
answer to a question, and an agent that is not blocked is not asking one —
sending it text mid-turn injects a stray line into whatever it is doing. That
refusal is what makes a one-keypress reply safe to sit beside the filter keys.

The replies live one per line in `replies.txt` beside the state file, with `#`
comments allowed. A file rather than an environment variable because a canned
reply is free text that may contain the colons and commas every list variable
here splits on.

There is no `--all-blocked` for `reply`, though `prompt` has one. Answering
every waiting agent at once from muscle memory is the approve-all behaviour
this project refuses on purpose.

### Pin the agents you care about

```sh
sesh-bro star builder --toggle   # pin, or unpin if already pinned
sesh-bro star builder            # pin
sesh-bro star builder --off      # unpin
sesh-bro star --list --json      # what is pinned
```

A star floats an agent to the top of its own status group, not to the top of
the list. A pinned idle agent leads the idle ones and still sits below every
blocked agent, so pinning can never bury something that actually needs a human.

An agent with a name is pinned by that name, so the pin survives its pane being
destroyed and recreated. An agent with no name is pinned by pane id, and that
pin therefore lasts only as long as the pane. sesh-bro will not name an agent on
your behalf to make a pin durable.

### Work on a GitHub issue

```sh
sesh-bro worktree https://github.com/owner/repo/issues/409
```

Creates a real `git worktree` on a branch named for the issue, opens it as a
workspace, and labels it with the issue title (resolved via `gh`, cached for a
day). Called again for the same issue it focuses the existing workspace rather
than making a second one.

If the target is not a checkout of that repository, or the worktree cannot be
created, it degrades to a plain workspace at the same location and says so on
stderr rather than failing — you get somewhere to work either way.

## A worked example

Find whoever is blocked, read their question, answer it by hand:

```sh
if sesh-bro agents --blocked --jsonl | head -1 | grep -q .; then
  target=$(sesh-bro agents --blocked --jsonl | head -1 | jq -r .target)
  sesh-bro read "$target" --source recent-unwrapped
fi
```

Wait for a long job, then report:

```sh
sesh-bro wait --target builder --until idle,done --timeout 600s --json
case $? in
  0) echo "settled" ;;
  3) echo "still going after 10 minutes" ;;
  *) echo "something is wrong with herdr" ;;
esac
```

## Other sessions

```sh
sesh-bro sessions --json          # every session on this machine
sesh-bro sessions --running
sesh-bro list --all-sessions --jsonl
sesh-bro counts --all-sessions
```

A herdr session is a daemon. N sessions are N server processes with N sockets,
and **no herdr API call takes a session parameter** — verified again against
protocol 22, where the only `session_id` in the whole schema is
`agent_session_id`, a coding agent's own identity. So every call lands on
whichever socket it dialled.

That makes other sessions **readable and not writable**. `--all-sessions` adds
one `session` row per other running session and one `ragent` row per agent
inside it, with targets composed as `<id>@<session>`.

Every mutating command refuses a composed target with **exit 2**, naming the
session and what to do instead. That includes `prompt`, `reply`, `star`,
`close`, and also `read` and `explain` — they dial the local socket, so a
foreign target would not fail, it would silently act on a same-named agent
here. herdr's ids are scoped to one server and two sessions can both hold
`w1:p1` or an agent called `reviewer`.

The one thing that does work is leaving:

```sh
sesh-bro connect session work
```

That opens a terminal running `herdr session attach work`, via
`SESH_BRO_ATTACH_CMD` (a shell command containing `{session}`). macOS has a
default; every other platform must set the variable, because terminal
emulators vary too much for a guess to be right.

Foreign rows are polled, not subscribed — `SESH_BRO_FOREIGN_INTERVAL`, default
`5s`, `0` to disable. It is the only poll in the project, and
`internal/live`'s package comment says why.

## What sesh-bro will not do

- **Act on another herdr session.** It can list other sessions read-only, but
  cross-session focus is structurally impossible: no herdr API method takes a
  session parameter, and the TUI client owns the terminal. See
  `docs/MULTI-SESSION.md`.
- **Answer prompts for you.** There is no approve-all, deliberately. Blanket
  auto-yes bypasses the security confirmations agents raise on purpose.
- **Steal focus.** Nothing here runs unless you run it.
