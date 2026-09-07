// `agents` is `list --agents` with a shorter name.
//
// It exists purely for the agent-facing surface. The full incantation for "show
// me every blocked agent as parseable JSON" is
// `sesh-bro list --agents --blocked --jsonl`, which is four flags in an order
// that matters (BEHAVIOUR.md §9 S3: a status flag clears earlier source flags,
// so `--blocked --agents` and `--agents --blocked` are not the same command).
// That is a lot of surface to get right for the most obvious question anyone
// would ask this tool, and getting it subtly wrong yields a plausible-looking
// wrong answer rather than an error.
//
// So: one name, status flags that mean what they look like, and no ordering
// trap. It delegates to the same code path, so there is no second
// implementation to keep in step.
package main

import (
	"context"
	"fmt"
)

func cmdAgents(ctx context.Context, env *appEnv, args []string) int {
	// --agents first, so a status flag arriving later cannot clear it. This is
	// the S3 ordering trap being handled once, here, instead of by every
	// caller.
	forwarded := append([]string{"--agents"}, args...)
	for _, a := range args {
		switch a {
		case "--workspaces", "--dirs", "--worktrees":
			// Refuse rather than silently producing something other than
			// agents: the command's name is a promise about what comes back.
			fmt.Fprintf(env.stderr, "sesh-bro agents: %s contradicts this command; use `list` for mixed sources\n", a)
			return 2
		}
	}
	return cmdList(ctx, env, forwarded)
}
