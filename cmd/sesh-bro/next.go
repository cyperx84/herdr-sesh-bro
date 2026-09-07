// `next` and `prev` are sesh-bro's one-keypress surface: go straight to the
// agent that wants you, with no picker in between.
//
// herdr ships next_agent/previous_agent, but they cycle in *panel* order, so
// they only visit agents by urgency if you also set agent_panel_sort =
// "priority" — and that reorders the panel, which breaks the stable positions
// focus_agent's 1..9 indexes rely on. herdr discussion #2761 states the
// tension exactly: "stable numbers or attention-ordered cycling — I can have
// either, not both." A plugin command has no such constraint, because it
// reorders nothing: it reads the live snapshot, picks, and focuses.
//
// Demand: herdr discussions #682 (6 upvotes) and #778 (3), four distinct
// askers, still open. Prior art credited in herdrx.NextAttention.
package main

import (
	"context"
	"fmt"

	"github.com/cyperx84/herdr-sesh-bro/internal/external"
	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx"
	"github.com/cyperx84/herdr-sesh-bro/internal/output"
)

// cmdNext focuses the next (dir +1) or previous (dir -1) agent needing
// attention.
func cmdNext(ctx context.Context, env *appEnv, args []string, dir int) int {
	var asJSON, dryRun bool
	for _, a := range args {
		switch a {
		case "--json":
			asJSON = true
		case "--dry-run":
			// Report where it WOULD go without going. A driving agent
			// surveying the queue must be able to ask without moving the
			// human's focus, and focusing a `done` agent marks it seen —
			// so a survey that focused would quietly destroy the signal it
			// was surveying.
			dryRun = true
		default:
			fmt.Fprintf(env.stderr, "sesh-bro: %s: unknown flag %s\n", dirName(dir), a)
			return 2
		}
	}

	client, openErr := openHerdr(env.getenv)
	if err := external.CheckListDeps(ctx, env.herdrBin, aliverFor(client, openErr)); err != nil {
		fmt.Fprintln(env.stderr, err)
		return 1
	}
	snap, err := loadSnapshot(ctx, client, openErr)
	if err != nil {
		fmt.Fprintln(env.stderr, err)
		return 1
	}

	set := herdrx.AttentionSet(snap.Agents)
	target, remaining, ok := herdrx.NextAttention(set, snap.FocusedPane(), dir)
	if !ok {
		// Nothing is waiting. For a human on a keybinding that is a success
		// worth announcing — silence is indistinguishable from a broken bind.
		// For a caller it is output.ExitEmpty: the command ran correctly and
		// the answer is "nobody", which is information rather than a fault.
		if asJSON {
			_ = output.Emit(env.stdout, output.Empty(dirName(dir)))
		} else {
			announce(ctx, env, client, openErr, "nothing needs you", "")
		}
		return output.ExitEmpty
	}

	if dryRun {
		if asJSON {
			_ = output.Emit(env.stdout, output.Success(dirName(dir), map[string]any{
				"pane_id":   target.PaneID,
				"status":    string(target.Status),
				"remaining": remaining,
			}))
		} else {
			fmt.Fprintf(env.stdout, "%s\t%s\n", target.PaneID, target.Status)
		}
		return output.ExitOK
	}

	title, body := attentionMessage(target, remaining)
	// Toast BEFORE focusing. herdr suppresses a notification aimed at the tab
	// you are already looking at, and focusing makes the target exactly that
	// tab — so toasting afterwards reliably shows nothing.
	announce(ctx, env, client, openErr, title, body)

	// Focus by pane id, never by name: most agents are unnamed, and agent.focus
	// switches workspace, tab and pane in one call.
	if err := focusAgent(ctx, client, openErr, target.PaneID); err != nil {
		fmt.Fprintf(env.stderr, "sesh-bro: failed to focus agent %s\n", target.PaneID)
		if asJSON {
			_ = output.Emit(env.stdout, output.Failure(dirName(dir), err))
		}
		return output.ExitFailure
	}
	if asJSON {
		_ = output.Emit(env.stdout, output.Success(dirName(dir), map[string]any{
			"pane_id":   target.PaneID,
			"status":    string(target.Status),
			"remaining": remaining,
		}))
	}
	return output.ExitOK
}

// attentionMessage is the toast a jump announces: what you are being taken to,
// and how much else is still waiting.
func attentionMessage(a herdrx.Agent, remaining int) (title, body string) {
	title = fmt.Sprintf("→ %s %s", agentDisplayName(a), a.Status)
	if remaining > 0 {
		title += fmt.Sprintf(" (+%d more)", remaining)
	}
	kind := "?"
	if k := a.Agent.Agent; k != nil && *k != "" {
		kind = *k
	}
	detail := a.TerminalTitleStripped
	if detail == "" {
		detail = a.CWD
	}
	return title, kind + " · " + detail
}

// agentDisplayName is the agent's name, else its kind, else its pane id —
// matching how a picker row labels the same agent.
func agentDisplayName(a herdrx.Agent) string {
	if a.Name != "" {
		return a.Name
	}
	if k := a.Agent.Agent; k != nil && *k != "" {
		return *k
	}
	return a.PaneID
}

// announce toasts, and falls back to stdout when herdr will not show it.
//
// A keybinding's stdout is not on screen, but it IS captured in
// `herdr plugin log list`, so writing there turns an invisible suppressed
// toast into something diagnosable rather than lost. Toast failure is never
// fatal: the jump itself is the point.
func announce(ctx context.Context, env *appEnv, client *herdrx.Client, openErr error, title, body string) {
	if openErr == nil {
		if res, err := client.ShowToast(ctx, title, body); err == nil && res.Shown {
			return
		}
	}
	if body == "" {
		fmt.Fprintln(env.stdout, title)
		return
	}
	fmt.Fprintf(env.stdout, "%s — %s\n", title, body)
}

func dirName(dir int) string {
	if dir < 0 {
		return "prev"
	}
	return "next"
}
