package herdrx

import (
	"context"
	"fmt"
)

// Explain is herdr's answer to "why do you say this agent is blocked?".
//
// herdr classifies an agent's state by running a prioritised rule set against
// what the pane is showing — an OSC title, a prompt box, a permission dialog —
// and agent.explain reports which rule won. That turns a one-word status into
// something actionable: `blocked` alone says an agent wants you, while
// `blocked · bash_permission_prompt` says it is waiting on a command approval,
// and `blocked · mcp_elicitation_prompt` says something quite different is
// being asked. Same badge, different answer, different urgency.
//
// Only the winning rule is kept. herdr evaluates around sixteen per call and
// reports every one of them with its evidence; that is a debugging surface for
// the detection manifests themselves, not information about this agent, and
// carrying it would make an explain response an order of magnitude larger than
// the row it annotates.
type Explain struct {
	// Agent is the harness herdr recognised (claude, codex, pi…).
	Agent string
	// State is the status the winning rule produced.
	//
	// It does NOT always match the status on the row, and the mismatch is
	// systematic rather than a race. The detection rules only ever produce
	// working, blocked, idle or unknown — verified against a live 0.9.0
	// daemon, where sixteen evaluated rules carried exactly those four. `done`
	// is not a rule outcome at all: herdr derives it above this layer, as
	// "idle with work whose tab you have not looked at yet". So every done row
	// will have a rule that says idle, and the two are both correct in their
	// own vocabulary.
	//
	// That is why Summary omits it. A `done` row annotated "why: idle" reads
	// as a contradiction and teaches the user to distrust the line.
	State string
	// RuleID names the rule that won, e.g. "bash_permission_prompt". This is
	// the field that carries the whole value of the call.
	RuleID string
	// Region is the part of the screen the rule matched against, e.g.
	// "prompt_box_body" or "osc_title". Useful when the rule id alone is
	// ambiguous about where herdr was looking.
	Region string
	// Warning and FallbackReason are herdr's own notes about a classification
	// it is not confident in. Both are usually empty; when they are not, they
	// are the most important thing in the response.
	Warning        string
	FallbackReason string
	// ManifestVersion identifies the detection ruleset that produced this.
	// Rule ids are only meaningful relative to a manifest, and manifests
	// update themselves from a remote — so a rule id quoted without one is a
	// claim that may not reproduce next week.
	ManifestVersion string
}

// explainResult mirrors the agent.explain wire shape, narrowed to the fields
// Explain keeps. Unexported so the wire layout can change without widening
// this package's API.
type explainResult struct {
	Explain struct {
		Agent           string `json:"agent"`
		State           string `json:"state"`
		Warning         string `json:"warning"`
		FallbackReason  string `json:"fallback_reason"`
		ManifestVersion string `json:"manifest_version"`
		MatchedRule     *struct {
			ID     string `json:"id"`
			Region string `json:"region"`
		} `json:"matched_rule"`
	} `json:"explain"`
}

// ExplainAgent asks herdr why an agent is in the state it is in.
//
// agent.explain is new in herdr 0.9.0 (protocol 22) and absent from the 0.8.2
// baseline this plugin still supports, so a daemon that does not know the
// method returns an error here rather than an empty answer. Callers treat that
// as "no explanation available" and carry on: an explanation enriches a row,
// it never gates one.
func (c *Client) ExplainAgent(ctx context.Context, target string) (Explain, error) {
	var res explainResult
	params := map[string]any{"target": target}
	if err := c.c.Call(ctx, "agent.explain", params, &res); err != nil {
		return Explain{}, fmt.Errorf("herdrx: explain %s: %w", target, err)
	}
	e := Explain{
		Agent:           res.Explain.Agent,
		State:           res.Explain.State,
		Warning:         res.Explain.Warning,
		FallbackReason:  res.Explain.FallbackReason,
		ManifestVersion: res.Explain.ManifestVersion,
	}
	if res.Explain.MatchedRule != nil {
		e.RuleID = res.Explain.MatchedRule.ID
		e.Region = res.Explain.MatchedRule.Region
	}
	return e, nil
}

// Summary is the one-line form for a display that ALREADY shows the status:
// the rule that decided it, and nothing else.
//
// State is deliberately absent — see its doc comment. The row's badge is the
// status; this line's job is the part the badge cannot say, which is which of
// several quite different situations produced it.
//
// Empty when herdr matched no rule, so a caller can print it unconditionally
// and get silence rather than a line that says nothing.
func (e Explain) Summary() string {
	return e.RuleID
}
