// `sessions` lists the herdr sessions on this machine.
//
// It exists because a driving agent has no other way to learn them: there is
// no session.list RPC — the entire session.* namespace is one method,
// session.snapshot — so enumeration means shelling out to the CLI, and the
// obvious shortcut of globbing ~/.config/herdr/sessions/* misses the DEFAULT
// session, whose session_dir is ~/.config/herdr itself (docs/MULTI-SESSION.md).
// An agent that globbed would look right on a multi-session machine and
// silently miss the one almost everybody uses.
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/cyperx84/herdr-sesh-bro/internal/output"
)

// cmdSessions reports every session herdr knows. Exit codes:
//
//	0 — at least one session
//	1 — herdr could not be asked
//	2 — usage error
//	3 — no sessions at all
func cmdSessions(ctx context.Context, env *appEnv, args []string) int {
	var asJSON, runningOnly bool
	for _, a := range args {
		switch a {
		case "--json":
			asJSON = true
		case "--running":
			runningOnly = true
		default:
			fmt.Fprintf(env.stderr, "sesh-bro: sessions: unknown flag %s\n", a)
			return output.ExitUsage
		}
	}

	sessions, err := listSessions(ctx, sessionRunner(env.getenv))
	if err != nil {
		fmt.Fprintln(env.stderr, err)
		if asJSON {
			_ = output.Emit(env.stdout, output.Failure("sessions", err))
		}
		return output.ExitFailure
	}

	self := selfSocket(ctx, env)
	results := make([]any, 0, len(sessions))
	for _, s := range sessions {
		if runningOnly && !s.Running {
			continue
		}
		// "current" is by SOCKET, not by name. The name is what a human calls
		// the session; the socket is what this process is actually talking to,
		// and they are the same thing only when nothing has gone wrong.
		current := self != "" && s.SocketPath == self
		results = append(results, map[string]any{
			"name":        s.Name,
			"running":     s.Running,
			"default":     s.Default,
			"current":     current,
			"socket_path": s.SocketPath,
		})
		if !asJSON {
			fmt.Fprintf(env.stdout, "%s\t%s\t%s\n", s.Name, runningWord(s.Running), strings.TrimSpace(sessionTags(s.Default, current)))
		}
	}

	if len(results) == 0 {
		if asJSON {
			_ = output.Emit(env.stdout, output.Empty("sessions"))
		}
		return output.ExitEmpty
	}
	if asJSON {
		_ = output.Emit(env.stdout, output.Success("sessions", results...))
	}
	return output.ExitOK
}

func runningWord(running bool) string {
	if running {
		return "running"
	}
	return "stopped"
}

func sessionTags(isDefault, current bool) string {
	var tags []string
	if isDefault {
		tags = append(tags, "default")
	}
	if current {
		tags = append(tags, "current")
	}
	return strings.Join(tags, ",")
}
