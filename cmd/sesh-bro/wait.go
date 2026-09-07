// `wait` is sesh-bro's "is it done yet?" surface for a driving agent.
//
// Rather than polling agent.get in a loop, it asks herdr to block
// server-side until the named agent reaches a wanted status. That matters
// for composability: a script can run `sesh-bro wait --target foo` and the
// process exit code alone tells it whether foo settled (0), is still going
// (3, timed out), or could not be asked at all (1/2).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	herdr "github.com/cyperx84/herdr-api"

	"github.com/cyperx84/herdr-sesh-bro/internal/external"
)

// settleSet is the default --until value: the statuses that mean "this
// agent has finished its current turn and a caller can safely proceed".
// It matches herdr-api's own doc comment for AgentPromptWaitOptions and
// AgentWait's server-side default.
var settleSet = []herdr.AgentStatus{
	herdr.StatusIdle,
	herdr.StatusDone,
	herdr.StatusBlocked,
}

// validStatuses maps the status names the CLI accepts to herdr-api's
// typed constants. Waiting for "unknown" is allowed because the wire API
// allows it, even though it is almost never what a caller wants.
var validStatuses = map[string]herdr.AgentStatus{
	"idle":    herdr.StatusIdle,
	"working": herdr.StatusWorking,
	"blocked": herdr.StatusBlocked,
	"done":    herdr.StatusDone,
	"unknown": herdr.StatusUnknown,
}

type waitFlags struct {
	target  string
	until   []herdr.AgentStatus
	timeout time.Duration
	asJSON  bool
}

func parseWaitFlags(args []string) (waitFlags, error) {
	f := waitFlags{timeout: 60 * time.Second}
	var positional []string

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json":
			f.asJSON = true
		case strings.HasPrefix(a, "--target="):
			f.target = strings.TrimPrefix(a, "--target=")
		case a == "--target":
			if i+1 >= len(args) {
				return f, fmt.Errorf("sesh-bro: wait: --target requires a value")
			}
			i++
			f.target = args[i]
		case strings.HasPrefix(a, "--until="):
			if err := parseUntilFlag(&f, strings.TrimPrefix(a, "--until=")); err != nil {
				return f, err
			}
		case a == "--until":
			if i+1 >= len(args) {
				return f, fmt.Errorf("sesh-bro: wait: --until requires a value")
			}
			i++
			if err := parseUntilFlag(&f, args[i]); err != nil {
				return f, err
			}
		case strings.HasPrefix(a, "--timeout="):
			d, err := time.ParseDuration(strings.TrimPrefix(a, "--timeout="))
			if err != nil {
				return f, fmt.Errorf("sesh-bro: wait: invalid timeout: %w", err)
			}
			f.timeout = d
		case a == "--timeout":
			if i+1 >= len(args) {
				return f, fmt.Errorf("sesh-bro: wait: --timeout requires a value")
			}
			i++
			d, err := time.ParseDuration(args[i])
			if err != nil {
				return f, fmt.Errorf("sesh-bro: wait: invalid timeout: %w", err)
			}
			f.timeout = d
		case strings.HasPrefix(a, "-"):
			return f, fmt.Errorf("sesh-bro: wait: unknown flag: %s", a)
		default:
			positional = append(positional, a)
		}
	}

	if f.target == "" {
		return f, fmt.Errorf("sesh-bro: wait: --target is required")
	}
	if len(f.until) == 0 {
		f.until = append([]herdr.AgentStatus(nil), settleSet...)
	}
	if len(positional) > 0 {
		return f, fmt.Errorf("sesh-bro: wait: takes no positional arguments")
	}
	return f, nil
}

func parseUntilFlag(f *waitFlags, value string) error {
	parts := strings.Split(value, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		s, ok := validStatuses[p]
		if !ok {
			return fmt.Errorf("sesh-bro: wait: unknown status: %s", p)
		}
		f.until = append(f.until, s)
	}
	return nil
}

// cmdWait blocks until the target agent reaches one of the wanted statuses
// and reports the result. Exit codes are the contract:
//
//	0 — agent reached a wanted status
//	1 — runtime failure (daemon down, herdr rejected the call)
//	2 — usage error
//	3 — timeout: the agent did not reach any wanted status in time
func cmdWait(ctx context.Context, env *appEnv, args []string) int {
	flags, err := parseWaitFlags(args)
	if err != nil {
		writeWaitOutput(env, flags.asJSON, false, herdr.Agent{}, err.Error())
		return 2
	}

	client, openErr := openHerdr(env.getenv)
	if err := external.CheckListDeps(ctx, env.herdrBin, aliverFor(client, openErr)); err != nil {
		writeWaitOutput(env, flags.asJSON, false, herdr.Agent{}, err.Error())
		return 1
	}

	timeoutMs := uint64(flags.timeout.Milliseconds())
	agent, err := client.Raw().AgentWait(ctx, flags.target, flags.until, &timeoutMs)
	if err != nil {
		// A timeout is how herdr reports the ONLY interesting negative answer
		// this command has, and it reports it as an error rather than by
		// returning the unsettled agent. Verified against a live daemon:
		// waiting on an idle agent for "blocked" yields
		// `herdr: timeout: timed out waiting for agent status`.
		//
		// Treating that as a failure would defeat the whole point of the
		// command — "did it settle within 60s" is a question whose negative
		// answer is information, not a fault, and a caller that cannot tell a
		// timeout from an unreachable daemon has to parse the message to find
		// out. Hence exit 3 (output.ExitEmpty).
		if isWaitTimeout(err) {
			if flags.asJSON {
				writeWaitOutput(env, true, false, herdr.Agent{}, "")
			} else {
				fmt.Fprintf(env.stderr, "sesh-bro: wait: timed out after %s\n", flags.timeout)
			}
			return 3
		}
		writeWaitOutput(env, flags.asJSON, false, herdr.Agent{}, err.Error())
		return 1
	}

	if !statusWanted(agent.Status, flags.until) {
		// Belt and braces. herdr signals a timeout with an error (handled
		// above), but a server that instead returned the unsettled agent
		// would otherwise be reported as success — the caller would read
		// exit 0 and believe the agent had settled when it had not, which is
		// a worse failure than a spurious exit 3.
		if flags.asJSON {
			writeWaitOutput(env, true, false, agent, "")
		} else {
			fmt.Fprintf(env.stderr, "sesh-bro: wait: timed out after %s with status %s\n", flags.timeout, agent.Status)
		}
		return 3
	}

	writeWaitOutput(env, flags.asJSON, true, agent, "")
	return 0
}

func statusWanted(status herdr.AgentStatus, until []herdr.AgentStatus) bool {
	for _, s := range until {
		if status == s {
			return true
		}
	}
	return false
}

// writeWaitOutput prints either the JSON envelope or the human line. The
// JSON shape is intentionally uniform so a driving agent can parse the same
// object for success, timeout, and failure.
func writeWaitOutput(env *appEnv, asJSON, ok bool, agent herdr.Agent, errMsg string) {
	if asJSON {
		var results []map[string]any
		if agent.PaneID != "" {
			results = []map[string]any{{
				"status":  string(agent.Status),
				"pane_id": agent.PaneID,
			}}
		}
		writeHeadlessJSON(env.stdout, ok, "wait", results, errMsg)
		return
	}
	if errMsg != "" {
		fmt.Fprintln(env.stderr, errMsg)
		return
	}
	if !ok {
		// Plain timeout already printed its message before this helper was
		// called; reaching here with !ok and no errMsg should not happen.
		return
	}
	fmt.Fprintf(env.stdout, "%s %s\n", agent.Status, agent.PaneID)
}

// headlessJSON is the shared machine-readable envelope for `wait` and
// `read`: one object, one line, always valid JSON.
type headlessJSON struct {
	OK      bool             `json:"ok"`
	Command string           `json:"command"`
	Results []map[string]any `json:"results"`
	Error   *string          `json:"error"`
}

func writeHeadlessJSON(out io.Writer, ok bool, command string, results []map[string]any, errMsg string) {
	if results == nil {
		results = []map[string]any{}
	}
	var errStr *string
	if errMsg != "" {
		errStr = &errMsg
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(headlessJSON{
		OK:      ok,
		Command: command,
		Results: results,
		Error:   errStr,
	})
}

// isWaitTimeout reports whether an agent.wait error is herdr's timeout.
//
// Matching on the API error code rather than the message: the code is the
// stable identifier, and the human text around it is free to change. The
// message check is a fallback for a daemon that reports the same condition
// without a code — a wrong answer here costs an exit-3-versus-1 distinction,
// not correctness, so leaning permissive is right.
func isWaitTimeout(err error) bool {
	var apiErr *herdr.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code == "timeout"
	}
	return strings.Contains(err.Error(), "timed out waiting")
}
