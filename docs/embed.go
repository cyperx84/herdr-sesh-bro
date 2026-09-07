// Package docs embeds the documentation files the binary serves at runtime.
//
// It exists so `sesh-bro --skill` can print the SAME file a human reads in the
// repository, rather than a copy. go:embed cannot reach outside its own
// package directory, so the alternatives were a duplicate under cmd/ or a
// generate step — both of which create a second artefact that can go stale,
// which is precisely the failure this documentation exists to avoid. A tiny
// package alongside the docs is the cheapest way to keep exactly one file.
package docs

import _ "embed"

// AgentsMD is docs/AGENTS.md: how to drive sesh-bro from another program.
//
//go:embed AGENTS.md
var AgentsMD string
