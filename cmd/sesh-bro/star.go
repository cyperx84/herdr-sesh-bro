// `star` pins an agent so it leads its group in the picker.
//
// Within its group, deliberately: see herdrx.WithStars for why a pin must not
// outrank a blocked agent.
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx"
	"github.com/cyperx84/herdr-sesh-bro/internal/output"
	"github.com/cyperx84/herdr-sesh-bro/internal/stars"
)

func cmdStar(ctx context.Context, env *appEnv, args []string) int {
	var target string
	var asJSON, toggle, unstar, list bool
	for _, a := range args {
		switch a {
		case "--json":
			asJSON = true
		case "--toggle":
			toggle = true
		case "--off":
			unstar = true
		case "--list":
			list = true
		default:
			if len(a) > 1 && a[0] == '-' {
				fmt.Fprintf(env.stderr, "sesh-bro: star: unknown flag %s\n", a)
				return output.ExitUsage
			}
			target = a
		}
	}

	path := starsPath(env.getenv)

	if list {
		current := starsLoad(path)
		if len(current.Stars) == 0 {
			if asJSON {
				_ = output.Emit(env.stdout, output.Empty("star"))
			}
			return output.ExitEmpty
		}
		results := make([]any, 0, len(current.Stars))
		for _, st := range current.Stars {
			results = append(results, map[string]any{"kind": st.Kind, "key": st.Key})
			if !asJSON {
				fmt.Fprintf(env.stdout, "%s\t%s\n", st.Kind, st.Key)
			}
		}
		if asJSON {
			_ = output.Emit(env.stdout, output.Success("star", results...))
		}
		return output.ExitOK
	}

	if target == "" {
		fmt.Fprintln(env.stderr, "sesh-bro: star: needs an agent name or pane id (or --list)")
		return output.ExitUsage
	}

	// Resolve the target through the live session so the stored identity
	// matches what the row layer will look up. Starring by a name the picker
	// renders as a pane id (or the reverse) stores a key nothing ever matches,
	// and the pin then silently does nothing.
	kind, key := "agent", target
	client, openErr := openHerdr(env.getenv)
	if snap, err := loadSnapshot(ctx, client, openErr); err == nil {
		found := false
		for _, a := range snap.Agents {
			if a.Name == target || a.PaneID == target {
				kind, key = herdrx.StarKey(a)
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintf(env.stderr, "sesh-bro: star: no agent %q\n", target)
			return output.ExitFailure
		}
	}

	var starredNow bool
	err := starsUpdate(path, func(s *stars.Stars) {
		switch {
		case unstar:
			if s.Has(kind, key) {
				s.Toggle(kind, key, time.Now())
			}
			starredNow = false
		case toggle:
			starredNow = s.Toggle(kind, key, time.Now())
		default:
			if !s.Has(kind, key) {
				s.Toggle(kind, key, time.Now())
			}
			starredNow = true
		}
	})
	if err != nil {
		fmt.Fprintln(env.stderr, err)
		if asJSON {
			_ = output.Emit(env.stdout, output.Failure("star", err))
		}
		return output.ExitFailure
	}

	if asJSON {
		_ = output.Emit(env.stdout, output.Success("star", map[string]any{
			"kind": kind, "key": key, "starred": starredNow,
		}))
	}
	return output.ExitOK
}
