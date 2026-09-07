package main

import (
	"fmt"

	"github.com/cyperx84/herdr-sesh-bro/docs"
)

// cmdSkill prints the agent-facing documentation verbatim.
//
// It serves docs/AGENTS.md itself, through a tiny embed package, so there is
// exactly one copy: a second one under cmd/ would go stale, and documentation
// that lies about the tool is worse than none. `herdr --skill` set the
// precedent for a CLI that explains itself to an agent, and
// TestSkillDocumentsEveryHeadlessCommand keeps the explanation honest by
// walking the command table against this file.

func cmdSkill(env *appEnv) int {
	fmt.Fprint(env.stdout, docs.AgentsMD)
	return 0
}
