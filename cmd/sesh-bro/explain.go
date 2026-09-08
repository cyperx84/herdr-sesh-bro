// `explain` answers the question a status badge cannot: why does herdr think
// this agent is blocked?
//
// A `blocked` row says an agent wants a human. It does not say whether it is
// waiting on a bash command approval, an MCP elicitation, or a question inside
// a dynamic workflow — three quite different asks with three different
// urgencies, all rendered identically. herdr already knows, because that is
// how it decided: it runs a prioritised rule set against what the pane shows
// and one rule wins. This surfaces the winner.
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/cyperx84/herdr-sesh-bro/internal/external"
	"github.com/cyperx84/herdr-sesh-bro/internal/output"
)

// cmdExplain reports why an agent is in the state it is in. Exit codes:
//
//	0 — an explanation was returned
//	1 — runtime failure, including a daemon too old to know the method
//	2 — usage error
//	3 — herdr answered but matched no rule, so it has nothing to say
func cmdExplain(ctx context.Context, env *appEnv, args []string) int {
	var target string
	var asJSON bool
	for _, a := range args {
		switch {
		case a == "--json":
			asJSON = true
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(env.stderr, "sesh-bro: explain: unknown flag: %s\n", a)
			return output.ExitUsage
		default:
			if target != "" {
				fmt.Fprintln(env.stderr, "sesh-bro: explain: takes one target")
				return output.ExitUsage
			}
			target = a
		}
	}
	if target == "" {
		fmt.Fprintln(env.stderr, "sesh-bro: explain: needs an agent name or pane id")
		return output.ExitUsage
	}

	if err := refuseForeign("explain", target); err != nil {
		fmt.Fprintln(env.stderr, err)
		if asJSON {
			_ = output.Emit(env.stdout, output.Failure("explain", err))
		}
		return output.ExitUsage
	}

	client, openErr := openHerdr(env.getenv)
	if err := external.CheckListDeps(ctx, env.herdrBin, aliverFor(client, openErr)); err != nil {
		fmt.Fprintln(env.stderr, err)
		if asJSON {
			_ = output.Emit(env.stdout, output.Failure("explain", err))
		}
		return output.ExitFailure
	}

	// A daemon older than 0.9.0 does not know agent.explain and says so. That
	// is a runtime failure rather than an empty answer: "this herdr cannot
	// tell you" and "herdr looked and found nothing" are different facts, and
	// a caller that conflated them would keep asking a daemon that will never
	// answer.
	ex, err := client.ExplainAgent(ctx, target)
	if err != nil {
		fmt.Fprintln(env.stderr, err)
		if asJSON {
			_ = output.Emit(env.stdout, output.Failure("explain", err))
		}
		return output.ExitFailure
	}
	if ex.RuleID == "" {
		fmt.Fprintf(env.stderr, "sesh-bro: explain: herdr matched no rule for %s\n", target)
		if asJSON {
			_ = output.Emit(env.stdout, output.Empty("explain"))
		}
		return output.ExitEmpty
	}

	if asJSON {
		res := map[string]any{
			"target":           target,
			"agent":            ex.Agent,
			"state":            ex.State,
			"rule":             ex.RuleID,
			"region":           ex.Region,
			"manifest_version": ex.ManifestVersion,
		}
		// Only present when herdr is unsure. Always emitting them would train
		// a reader to skip the two fields that matter most when they appear.
		if ex.Warning != "" {
			res["warning"] = ex.Warning
		}
		if ex.FallbackReason != "" {
			res["fallback_reason"] = ex.FallbackReason
		}
		_ = output.Emit(env.stdout, output.Success("explain", res))
		return output.ExitOK
	}

	// The human form carries the state too, unlike the preview's line: someone
	// who ran this command asked about one agent and has no badge in front of
	// them, so the state is information rather than a contradiction. See
	// herdrx.Explain.State for why the two differ on `done` rows.
	fmt.Fprintf(env.stdout, "%s · %s\n", ex.State, ex.RuleID)
	if ex.Warning != "" {
		fmt.Fprintf(env.stdout, "warning: %s\n", ex.Warning)
	}
	if ex.FallbackReason != "" {
		fmt.Fprintf(env.stdout, "fallback: %s\n", ex.FallbackReason)
	}
	return output.ExitOK
}
