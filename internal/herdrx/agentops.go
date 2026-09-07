package herdrx

import (
	"context"
	"fmt"

	herdr "github.com/cyperx84/herdr-api"
)

// PromptAgent sends text to target as if typed and submitted.
//
// The obvious assumption — that a blocked agent cannot be prompted — is
// wrong, and acting on it would send a maintainer down a long detour.
// agent.prompt sends text as if typed, and its wait set explicitly includes
// blocked, so prompting a blocked agent is exactly the intended use case and
// herdr will not refuse. What herdr CAN report is AgentPromptStalledCode:
// when the target was not already "working", herdr expects to see a lifecycle
// change within 5s and fails with agent_prompt_stalled if it does not. Any
// caller that needs to distinguish a stalled prompt from other errors must
// use herdr.IsAgentPromptStalled(err); never match the error text directly.
func (c *Client) PromptAgent(ctx context.Context, target, text string) (herdr.Agent, error) {
	a, err := c.c.AgentPrompt(ctx, target, text, nil)
	if err != nil {
		return herdr.Agent{}, fmt.Errorf("herdrx: prompt agent %s: %w", target, err)
	}
	return a, nil
}

// SendKeys sends raw key sequences to target's pane, bypassing the
// type-and-submit semantics of AgentPrompt. Use it for control characters
// and interactive prompts (e.g. a permission dialog) that AgentPrompt's
// model does not fit.
func (c *Client) SendKeys(ctx context.Context, target string, keys []string) error {
	if err := c.c.AgentSendKeys(ctx, target, keys); err != nil {
		return fmt.Errorf("herdrx: send keys to %s: %w", target, err)
	}
	return nil
}
