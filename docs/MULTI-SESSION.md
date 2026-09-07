# Can sesh-bro show every herdr session? — findings (2026-09-06)

Primary research against a live herdr **0.8.2** on macOS, prompted by wanting
"show me every session's agents, not just this one". The short answer shaped
the whole design, so it is written down here rather than left in a commit
message.

**Cross-session focus is structurally impossible. Foreign sessions can be read,
never acted on.**

## A session is a daemon

`N` sessions means `N` `herdr server` processes, each with its own API socket,
its own client socket and its own `session.json`. Confirmed by `ps` and by a
stopped session's own log recording its pid and socket path before exiting.

`herdr session list --json` is the only correct enumeration:

```json
{"name":"default","default":true,"running":true,
 "session_dir":"/Users/…/.config/herdr",
 "socket_path":"/Users/…/.config/herdr/herdr.sock"}
```

Five fields, no pid, no counts, no last-attached time. It is **CLI-only** —
there is no `session.list` RPC; the entire `session.*` namespace over the
socket is one method, `session.snapshot`.

> **Gotcha worth the whole paragraph:** the default session's `session_dir` is
> `~/.config/herdr` *itself*, not `sessions/default/`. Globbing
> `~/.config/herdr/sessions/*` enumerates every session except the one almost
> everybody is using.

## Why focus cannot cross

Every request type in the 0.8.2 schema was checked for a session, host or
socket parameter. **There is none.** `workspace.focus`, `tab.focus`,
`pane.focus`, `agent.focus` and `plugin.pane.open` are all implicitly scoped to
the daemon you dialled.

The client side is worse, not better. Attaching is a foreground TUI process
owning a tty; a plugin running inside session A cannot reach session A's
client, let alone retarget it — `client.window_title.set` is the only `client.*`
method that exists. Moving the user to session B would mean that client exiting
and another starting on the same tty, which nothing in the API can ask for.

`herdr --remote <ssh-target>` is client-side too, and hits the identical wall.

## What *does* work: reading

Sockets have **no auth beyond unix file permissions** (`srw-------`, owned by
the user). Any same-user process can dial another running session's
`socket_path` and call `session.snapshot`, getting its full agent list with
live statuses, titles and cwds. There is no handshake and no capability
negotiation.

A **stopped** session has no socket file at all — the daemon unlinks it on a
clean exit, so a dial gets `ENOENT` rather than hanging. Prefer the `running`
flag from `session list` over probing. Its `<session_dir>/session.json`
(version 3) still holds workspaces, tabs, pane cwds and remembered agent names,
but no live status: it is a last-saved layout, not a view.

## What everyone else does

`thanhdat77/herdr-navigator` (130★) is the most complete picker in the
ecosystem. It lists sessions *as sessions* and hands off via herdr's own
`--remote TARGET --handoff` flow; its "remote" source is a hand-configured list
of ssh targets, not discovery. Decisively: **its agent source is
`herdr agent list`, i.e. the current session only.** Nobody aggregates agents
across sessions, because the API does not allow it.

## What sesh-bro will do

1. Enumerate via `herdr session list --json`, never by globbing.
2. For each *running* foreign session, dial its socket and `session.snapshot`.
   A session that will not answer contributes no rows rather than failing the
   render.
3. Show those rows **read-only**. Never call focus, prompt or send-keys on a
   foreign socket: it would succeed, change that session's internal state, and
   the user would see nothing happen. A silent action on an invisible session
   is a worse bug than a missing feature.
4. Enter on a foreign row spawns a new terminal running
   `herdr session attach <name>`, via a configurable command template. That is
   outside herdr's model and platform-specific, and it is the only thing that
   actually lands the user in session B.
5. Refresh foreign rows on a slow ticker rather than subscribing. Foreign
   sessions emit events to their own sockets, and following them properly means
   one global subscription per session plus one per-pane subscription per
   foreign agent — `O(sessions × panes)` held connections for a secondary view.
   This is the **one deliberate exception** to the project's push-not-poll
   rule, and `internal/live`'s package comment says so rather than leaving its
   absolute phrasing to quietly become false.

## Method

`herdr api schema --json` is 255 KB and contains raw control characters — `jq`
fails on it. Parse with Python's `json.loads(..., strict=False)`.
