// `reply` sends one of a few canned answers to a blocked agent.
//
// It is `prompt` with the typing removed. The thing that actually costs a
// human time in a room full of agents is not composing an answer — most
// answers are "yes" — it is noticing that something is asking, finding it, and
// getting a word into it. `prompt` solves the last step for a driving agent;
// `reply` solves it for a person, in one keypress from the picker.
//
// Two deliberate restrictions, both of which make an accidental keypress
// harmless rather than expensive:
//
//   - It refuses any agent that is not BLOCKED. A canned "yes" is an answer to
//     a question, and an agent that is not blocked is not asking one — sending
//     it text mid-turn injects a stray line into whatever it is doing. The
//     refusal is exit 3, the "nothing to do here" code, because a picker bind
//     that fires on the wrong row should be a no-op and not an error.
//   - There is no --all-blocked. `prompt` has one because a driving agent
//     genuinely wants to unblock a fleet with one call; this is the key a
//     human presses without reading carefully, and answering every waiting
//     agent at once from muscle memory is the approve-all behaviour this
//     project refuses on purpose (docs/FEATURE-DEMAND.md).
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	herdr "github.com/cyperx84/herdr-api"

	"github.com/cyperx84/herdr-sesh-bro/internal/external"
	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx"
	"github.com/cyperx84/herdr-sesh-bro/internal/output"
)

// defaultReplies are the answers a fresh install has. They are the three that
// actually get typed at a blocked agent: approve, decline, and "keep going"
// for one that stopped to check rather than to ask.
var defaultReplies = []string{"yes", "no", "continue"}

// repliesPath is the editable canned-reply list, one reply per line, beside
// the other state files.
//
// A file rather than an environment variable, and it is the separator that
// decides it: every env-var list in this project splits on a colon or a comma,
// and a canned reply is free text that may well contain both ("run: make
// test"). One reply per line has no such ambiguity, survives spaces and
// punctuation untouched, and is the shape a person can edit without consulting
// documentation.
func repliesPath(getenv func(string) string) string {
	return filepath.Join(filepath.Dir(statePath(getenv)), "replies.txt")
}

// loadReplies reads the canned replies, falling back to the built-in list.
//
// Blank lines and # comments are skipped so the file can explain itself. A
// missing or unreadable file degrades to the defaults rather than to nothing:
// a reply key that silently stopped working would be diagnosed as a broken
// keybind, and the defaults are always a sensible answer to a blocked agent.
func loadReplies(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return defaultReplies
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		return defaultReplies
	}
	return out
}

type replyFlags struct {
	target string
	index  int
	list   bool
	asJSON bool
}

func parseReplyFlags(args []string) (replyFlags, error) {
	f := replyFlags{index: 1}
	var positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json":
			f.asJSON = true
		case a == "--list":
			f.list = true
		case strings.HasPrefix(a, "-"):
			return f, fmt.Errorf("sesh-bro: reply: unknown flag: %s", a)
		default:
			positional = append(positional, a)
		}
	}
	if f.list {
		return f, nil
	}
	if len(positional) == 0 {
		return f, fmt.Errorf("sesh-bro: reply: a target is required")
	}
	f.target = positional[0]
	if len(positional) > 1 {
		n, err := strconv.Atoi(positional[1])
		if err != nil || n < 1 {
			return f, fmt.Errorf("sesh-bro: reply: reply number must be 1 or greater: %s", positional[1])
		}
		f.index = n
	}
	if len(positional) > 2 {
		return f, fmt.Errorf("sesh-bro: reply: takes a target and an optional reply number")
	}
	return f, nil
}

// cmdReply sends canned reply N to a blocked agent. Exit codes:
//
//	0 — the reply reached the agent
//	1 — runtime failure
//	2 — usage error
//	3 — the target is not a blocked agent, or there is no reply N
func cmdReply(ctx context.Context, env *appEnv, args []string) int {
	flags, err := parseReplyFlags(args)
	if err != nil {
		fmt.Fprintln(env.stderr, err)
		if flags.asJSON {
			_ = output.Emit(env.stdout, output.Failure("reply", err))
		}
		return output.ExitUsage
	}

	replies := loadReplies(repliesPath(env.getenv))
	if flags.list {
		results := make([]any, 0, len(replies))
		for i, r := range replies {
			results = append(results, map[string]any{"n": i + 1, "text": r})
			if !flags.asJSON {
				fmt.Fprintf(env.stdout, "%d\t%s\n", i+1, r)
			}
		}
		if flags.asJSON {
			_ = output.Emit(env.stdout, output.Success("reply", results...))
		}
		return output.ExitOK
	}
	if flags.index > len(replies) {
		fmt.Fprintf(env.stderr, "sesh-bro: reply: no reply %d (%d configured)\n", flags.index, len(replies))
		if flags.asJSON {
			_ = output.Emit(env.stdout, output.Empty("reply"))
		}
		return output.ExitEmpty
	}
	text := replies[flags.index-1]

	client, openErr := openHerdr(env.getenv)
	if err := external.CheckListDeps(ctx, env.herdrBin, aliverFor(client, openErr)); err != nil {
		fmt.Fprintln(env.stderr, err)
		if flags.asJSON {
			_ = output.Emit(env.stdout, output.Failure("reply", err))
		}
		return output.ExitFailure
	}

	// Resolve through the snapshot rather than prompting blind: the blocked
	// check IS the safety property here, and it cannot be made after the text
	// has already been delivered.
	snap, err := loadSnapshot(ctx, client, openErr)
	if err != nil {
		fmt.Fprintln(env.stderr, err)
		if flags.asJSON {
			_ = output.Emit(env.stdout, output.Failure("reply", err))
		}
		return output.ExitFailure
	}
	var agent herdrx.Agent
	found := false
	for _, a := range snap.Agents {
		if a.Name == flags.target || a.PaneID == flags.target {
			agent, found = a, true
			break
		}
	}
	if !found {
		// Not an agent at all — a picker bind that fired on a workspace or a
		// directory row. Nothing to answer, and nothing broken.
		fmt.Fprintf(env.stderr, "sesh-bro: reply: no agent %q\n", flags.target)
		if flags.asJSON {
			_ = output.Emit(env.stdout, output.Empty("reply"))
		}
		return output.ExitEmpty
	}
	if herdrx.NormalizeStatus(agent.Status) != herdr.StatusBlocked {
		fmt.Fprintf(env.stderr, "sesh-bro: reply: %s is %s, not blocked — nothing is being asked\n",
			flags.target, agent.Status)
		if flags.asJSON {
			_ = output.Emit(env.stdout, output.Empty("reply"))
		}
		return output.ExitEmpty
	}

	prompted, err := client.PromptAgent(ctx, flags.target, text)
	if err != nil {
		fmt.Fprintln(env.stderr, err)
		if flags.asJSON {
			_ = output.Emit(env.stdout, output.Failure("reply", err))
		}
		return output.ExitFailure
	}
	if flags.asJSON {
		_ = output.Emit(env.stdout, output.Success("reply", map[string]any{
			"target":  flags.target,
			"pane_id": prompted.PaneID,
			"text":    text,
			"status":  string(prompted.Status),
		}))
	}
	return output.ExitOK
}
