// `counts` is sesh-bro's zero-keypress surface: one line saying how many
// agents are in each state, cheap enough to run on a timer.
//
// It exists because the picker, however fast, still costs a keystroke and a
// context switch, and the question it usually answers is not "which session"
// but "does anything need me at all". That question deserves an answer you
// never have to ask for. herdr 0.8.2 added right-aligned tab-bar entries that
// asynchronously re-run a command, so the answer can simply live in the tab
// bar; the same line drives a SketchyBar item or a shell prompt.
//
// Prior art: wjarka/herdr-ghostty-tab-title puts the same counts in a Ghostty
// tab title, ordered by attention priority. The idea is theirs; this is the
// same idea served from the binary that already knows the answer.
package main

import (
	"context"
	"encoding/json"
	"fmt"

	herdr "github.com/cyperx84/herdr-api"

	"github.com/cyperx84/herdr-sesh-bro/internal/external"
	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx"
	"github.com/cyperx84/herdr-sesh-bro/internal/render"
)

type countsFlags struct {
	ansi        bool
	asJSON      bool
	includeZero bool
}

func parseCountsFlags(args []string) (countsFlags, error) {
	var f countsFlags
	for _, a := range args {
		switch a {
		case "--ansi":
			f.ansi = true
		case "--json":
			f.asJSON = true
		case "--all":
			f.includeZero = true
		default:
			return f, fmt.Errorf("sesh-bro: counts: unknown flag: %s", a)
		}
	}
	return f, nil
}

func cmdCounts(ctx context.Context, env *appEnv, args []string) int {
	flags, err := parseCountsFlags(args)
	if err != nil {
		fmt.Fprintln(env.stderr, err)
		return 2
	}

	client, openErr := openHerdr(env.getenv)
	// Same dependency gate as `list`: a missing binary or an unreachable
	// daemon is an error, not an empty line. A tab bar that silently blanks
	// when herdr dies is worse than one that shows nothing at all, because
	// "no agents" and "no herdr" look identical.
	if err := external.CheckListDeps(ctx, env.herdrBin, aliverFor(client, openErr)); err != nil {
		fmt.Fprintln(env.stderr, err)
		return 1
	}
	snap, err := loadSnapshot(ctx, client, openErr)
	if err != nil {
		fmt.Fprintln(env.stderr, err)
		return 1
	}

	counts := herdrx.CountByStatus(snap.Agents)
	if flags.asJSON {
		// Every status is present in JSON, including zeros: a consumer
		// indexing the object should not have to distinguish absent from
		// zero, which is the opposite of what the human-facing line wants.
		out := make(map[string]int, len(herdrx.StatusOrder))
		for _, s := range herdrx.StatusOrder {
			out[string(s)] = counts[s]
		}
		enc := json.NewEncoder(env.stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		if err := enc.Encode(out); err != nil {
			fmt.Fprintln(env.stderr, err)
			return 1
		}
		return 0
	}

	style := render.CountsPlain
	if flags.ansi {
		style = render.CountsANSI
	}
	fmt.Fprintln(env.stdout, render.CountsLine(statusCounts(counts), statusOrder(), style, flags.includeZero))
	return 0
}

// statusCounts and statusOrder re-key herdrx's typed map for render, which
// deliberately knows nothing about herdr-api's types — the same separation
// StatusColor already keeps.
func statusCounts(in map[herdr.AgentStatus]int) map[string]int {
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[string(k)] = v
	}
	return out
}

func statusOrder() []string {
	out := make([]string, len(herdrx.StatusOrder))
	for i, s := range herdrx.StatusOrder {
		out[i] = string(s)
	}
	return out
}
