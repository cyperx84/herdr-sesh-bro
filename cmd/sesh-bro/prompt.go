// `prompt` is sesh-bro's "send text to agents from the command line"
// surface: the thing a blocked-agent picker cannot do by itself is answer
// the agent.
//
// It targets one or more agents by name or pane id, sends the text as if
// typed and submitted, and reports per-target whether the prompt reached
// its pane. It never aborts a batch on one failure: one unreachable agent
// must not deny the others their prompt.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	herdr "github.com/cyperx84/herdr-api"

	"github.com/cyperx84/herdr-sesh-bro/internal/external"
	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx"
	"github.com/cyperx84/herdr-sesh-bro/internal/output"
)

type promptFlags struct {
	targets    []string
	text       string
	allBlocked bool
	fromFile   string
	wait       bool
	asJSON     bool
}

func parsePromptFlags(args []string) (promptFlags, error) {
	var f promptFlags
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json":
			f.asJSON = true
		case a == "--wait":
			f.wait = true
		case a == "--all-blocked":
			f.allBlocked = true
		case strings.HasPrefix(a, "--text="):
			f.text = strings.TrimPrefix(a, "--text=")
		case a == "--text":
			if i+1 >= len(args) {
				return f, fmt.Errorf("sesh-bro: prompt: --text requires a value")
			}
			i++
			f.text = args[i]
		case strings.HasPrefix(a, "--from-file="):
			f.fromFile = strings.TrimPrefix(a, "--from-file=")
		case a == "--from-file":
			if i+1 >= len(args) {
				return f, fmt.Errorf("sesh-bro: prompt: --from-file requires a value")
			}
			i++
			f.fromFile = args[i]
		case strings.HasPrefix(a, "-"):
			return f, fmt.Errorf("sesh-bro: prompt: unknown flag: %s", a)
		default:
			f.targets = append(f.targets, a)
		}
	}
	return f, nil
}

// cmdPrompt sends text to one or more agents. Exit codes are the contract:
//
//	0 — all prompts reached their targets
//	1 — at least one prompt failed (the others still ran)
//	2 — usage error
//	3 — nothing matched the requested targets (e.g. --all-blocked with no blocked agent)
func cmdPrompt(ctx context.Context, env *appEnv, args []string) int {
	flags, err := parsePromptFlags(args)
	if err != nil {
		writePromptOutput(env, flags.asJSON, false, nil, err.Error())
		return 2
	}

	if flags.text == "-" {
		b, err := io.ReadAll(env.stdin)
		if err != nil {
			writePromptOutput(env, flags.asJSON, false, nil, fmt.Sprintf("sesh-bro: prompt: read stdin: %v", err))
			return 1
		}
		// Trim a single trailing newline if present; stdin reading is a
		// convenience for piping prose, and a trailing newline is almost
		// always shell redirection noise, not part of the prompt.
		flags.text = strings.TrimSuffix(string(b), "\n")
		flags.text = strings.TrimSuffix(flags.text, "\r\n")
	}
	if flags.text == "" {
		writePromptOutput(env, flags.asJSON, false, nil, "sesh-bro: prompt: --text is required and must not be empty")
		return 2
	}

	client, openErr := openHerdr(env.getenv)
	if err := external.CheckListDeps(ctx, env.herdrBin, aliverFor(client, openErr)); err != nil {
		writePromptOutput(env, flags.asJSON, false, nil, err.Error())
		return 1
	}

	var targets []string
	switch {
	case flags.allBlocked:
		snap, err := loadSnapshot(ctx, client, openErr)
		if err != nil {
			writePromptOutput(env, flags.asJSON, false, nil, err.Error())
			return 1
		}
		targets = blockedTargets(snap.Agents)
		if len(targets) == 0 {
			writePromptOutput(env, flags.asJSON, true, nil, "")
			return 3
		}
	case flags.fromFile != "":
		rows, skipped, err := readPromptRowsFile(flags.fromFile)
		if err != nil {
			writePromptOutput(env, flags.asJSON, false, nil, err.Error())
			return 1
		}
		for _, sk := range skipped {
			fmt.Fprintf(env.stderr, "sesh-bro: prompt: skipped non-agent row: %s\n", sk)
		}
		targets = rows
		if len(targets) == 0 {
			writePromptOutput(env, flags.asJSON, true, nil, "")
			return 3
		}
	default:
		if len(flags.targets) == 0 {
			writePromptOutput(env, flags.asJSON, false, nil, "sesh-bro: prompt: target(s) required (or --all-blocked or --from-file)")
			return 2
		}
		targets = flags.targets
	}

	results := make([]map[string]any, 0, len(targets))
	allOK := true
	for _, target := range targets {
		var agent herdr.Agent
		var err error
		if flags.wait {
			// The herdrx wrapper deliberately exposes the simple
			// send-and-return prompt; --wait is the command-level choice to
			// block until the agent settles, so it goes through the raw
			// herdr-api client with the server-default settle set.
			agent, err = client.Raw().AgentPrompt(ctx, target, flags.text, &herdr.AgentPromptWaitOptions{})
		} else {
			agent, err = client.PromptAgent(ctx, target, flags.text)
		}
		if err != nil {
			allOK = false
			results = append(results, map[string]any{
				"target":  target,
				"pane_id": "",
				"ok":      false,
				"error":   err.Error(),
			})
			continue
		}
		results = append(results, map[string]any{
			"target":  target,
			"pane_id": agent.PaneID,
			"ok":      true,
			"status":  string(agent.Status),
		})
	}

	if allOK {
		writePromptOutput(env, flags.asJSON, true, results, "")
		return 0
	}
	writePromptOutput(env, flags.asJSON, false, results, "one or more prompts failed")
	return 1
}

// blockedTargets returns every agent that is currently blocked, in the
// attention-set order (newest state change first). The target is the agent's
// name if it has one, otherwise its pane id.
func blockedTargets(agents []herdrx.Agent) []string {
	set := herdrx.AttentionSet(agents)
	var out []string
	for _, a := range set {
		if a.Status != herdr.StatusBlocked {
			continue
		}
		out = append(out, agentPromptTarget(a))
	}
	return out
}

func agentPromptTarget(a herdrx.Agent) string {
	if a.Name != "" {
		return a.Name
	}
	return a.PaneID
}

// readPromptRowsFile reads a TSV file of the shape `sesh-bro list` emits.
// It returns the target field of AGENT rows only; every other row type is
// returned in skipped so the caller can report it rather than dropping it
// silently. The header row is chrome and is skipped without reporting.
func readPromptRowsFile(path string) ([]string, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("sesh-bro: prompt: open %s: %w", path, err)
	}
	defer f.Close()

	var targets, skipped []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			skipped = append(skipped, line)
			continue
		}
		kind := parts[0]
		target := parts[1]
		if kind == "agent" {
			targets = append(targets, target)
		} else if kind == "header" {
			// counts/header row is chrome, not a selectable candidate.
			continue
		} else {
			skipped = append(skipped, fmt.Sprintf("%s %s", kind, target))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, nil, fmt.Errorf("sesh-bro: prompt: read %s: %w", path, err)
	}
	return targets, skipped, nil
}

func writePromptOutput(env *appEnv, asJSON, ok bool, results []map[string]any, errMsg string) {
	if asJSON {
		var envp output.Envelope
		switch {
		case errMsg != "":
			envp = output.Failure("prompt", errors.New(errMsg), resultsToAny(results)...)
		case ok && len(results) == 0:
			envp = output.Empty("prompt")
		case ok:
			envp = output.Success("prompt", resultsToAny(results)...)
		default:
			envp = output.Failure("prompt", errors.New("one or more prompts failed"), resultsToAny(results)...)
		}
		_ = output.Emit(env.stdout, envp)
		return
	}
	if errMsg != "" {
		fmt.Fprintln(env.stderr, errMsg)
		return
	}
	for _, r := range results {
		target := r["target"].(string)
		if r["ok"].(bool) {
			fmt.Fprintf(env.stdout, "prompted %s (%s)\n", target, r["pane_id"])
		} else {
			fmt.Fprintf(env.stderr, "sesh-bro: prompt %s failed: %s\n", target, r["error"])
		}
	}
}

func resultsToAny(results []map[string]any) []any {
	out := make([]any, len(results))
	for i, r := range results {
		out[i] = r
	}
	return out
}
