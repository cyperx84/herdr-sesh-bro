// The command table: one description of every subcommand, from which dispatch,
// the usage text, and the agent-facing documentation are all derived.
//
// Four things used to have to agree by hand — the dispatch switch, usage(),
// open.go's accepted-flag map, and the docs — and they did not. `counts` and
// `rows` reached dispatch before either doc mentioned them. That is the
// ordinary fate of a list maintained in four places, and the fix is to have
// one list and generate the rest.
//
// It matters more now than it did: 0.5.0 makes sesh-bro drivable by another
// coding agent, and an agent reading documentation that omits a command has no
// way to discover it. TestSkillDocumentsEveryHeadlessCommand walks this table
// against docs/AGENTS.md so the two cannot drift.
package main

import (
	"context"
	"sort"
	"strings"
)

// commandSpec is one subcommand.
type commandSpec struct {
	Name string
	// Args is the argument summary shown after the name in usage, e.g.
	// "TYPE TARGET". Empty for commands that take none.
	Args string
	// Summary is one line, lowercase, no trailing period.
	Summary string
	// Hidden keeps a command out of the usage text. Used for plumbing a human
	// never invokes: `rows` is called by fzf's reload binds, `record-event` by
	// herdr's event hooks. Hidden commands are still dispatched and still
	// tested; they are simply not advertised.
	Hidden bool
	// Headless marks a command another agent can reasonably drive: it takes no
	// interactive input, and its output is either machine-readable or trivially
	// parseable. These are the commands docs/AGENTS.md must document.
	Headless bool
	Run      func(ctx context.Context, env *appEnv, args []string) int
}

// commands is the single source of truth. Order is the order usage() prints:
// the surfaces a human touches first, then the machine-facing ones.
var commands = []commandSpec{
	{Name: "picker", Args: "[flags]", Summary: "open the fzf picker (default)",
		Run: cmdPicker},
	{Name: "list", Args: "[flags]", Summary: "print picker candidates (type, target, display)",
		Headless: true, Run: cmdList},
	{Name: "agents", Args: "[flags]", Summary: "list agents (list --agents, without the flag-order trap)",
		Headless: true, Run: cmdAgents},
	{Name: "counts", Args: "[flags]", Summary: "one line: how many agents are blocked/working/done/idle",
		Headless: true, Run: cmdCounts},
	{Name: "next", Summary: "focus the next agent needing attention (blocked, then done)",
		Headless: true, Run: func(ctx context.Context, env *appEnv, args []string) int {
			return cmdNext(ctx, env, args, 1)
		}},
	{Name: "prev", Summary: "same, backwards",
		Headless: true, Run: func(ctx context.Context, env *appEnv, args []string) int {
			return cmdNext(ctx, env, args, -1)
		}},
	{Name: "star", Args: "TARGET | --list", Summary: "pin an agent to the top of its status group (--toggle, --off)",
		Headless: true, Run: cmdStar},
	{Name: "prompt", Args: "TARGET... --text S", Summary: "send text to agents as if typed (--all-blocked, --from-file)",
		Headless: true, Run: cmdPrompt},
	{Name: "reply", Args: "TARGET [N] | --list", Summary: "send canned reply N to a blocked agent",
		Headless: true, Run: cmdReply},
	{Name: "wait", Args: "--target A [--until S] [--timeout D]", Summary: "block until an agent settles; exit 3 on timeout",
		Headless: true, Run: cmdWait},
	{Name: "read", Args: "TARGET [--source S]", Summary: "print what an agent's pane shows (--source recent-unwrapped for scrollback)",
		Headless: true, Run: cmdRead},
	{Name: "connect", Args: "TYPE TARGET", Summary: "focus a workspace/agent, or create a workspace for a dir",
		Headless: true, Run: cmdConnect},
	{Name: "close", Args: "TYPE TARGET", Summary: "close a workspace (workspace rows only; picker alt-x)",
		Headless: true, Run: cmdClose},
	{Name: "create", Args: "[PATH]", Summary: "create a workspace for a directory (default: current dir)",
		Headless: true, Run: cmdCreate},
	{Name: "last", Summary: "focus the previously-focused workspace",
		Headless: true, Run: func(ctx context.Context, env *appEnv, _ []string) int { return cmdLast(ctx, env) }},
	{Name: "root", Summary: "focus/create the workspace for the current git root",
		Headless: true, Run: func(ctx context.Context, env *appEnv, _ []string) int { return cmdRoot(ctx, env) }},
	{Name: "worktree", Args: "[URL]", Summary: "create/focus the workspace for a GitHub issue/PR",
		Headless: true, Run: cmdWorktree},
	{Name: "preview", Args: "TYPE TARGET", Summary: "render the preview used by the picker",
		Run: cmdPreview},
	{Name: "open", Args: "[flags]", Summary: "open (or close) the picker popup through the Herdr plugin API",
		Run: func(_ context.Context, env *appEnv, args []string) int { return cmdOpen(env, args) }},
	{Name: "startup", Summary: "validate deps, report fzf features, tidy state (manifest startup hook)",
		Run: func(ctx context.Context, env *appEnv, _ []string) int { return cmdStartup(ctx, env) }},

	// Plumbing. Dispatched and tested, never advertised.
	{Name: "rows", Hidden: true, Summary: "print a pre-rendered view file (picker reload binds)",
		Run: func(_ context.Context, env *appEnv, args []string) int { return cmdRows(env, args) }},
	{Name: "record-event", Hidden: true, Summary: "record a herdr event (manifest [[events]] hook)",
		Run: func(ctx context.Context, env *appEnv, _ []string) int { return cmdRecordEvent(ctx, env) }},
}

// lookupCommand finds a spec by name.
func lookupCommand(name string) (commandSpec, bool) {
	for _, c := range commands {
		if c.Name == name {
			return c, true
		}
	}
	return commandSpec{}, false
}

// headlessCommands returns the agent-drivable commands, sorted, for the docs
// coverage test and for anything that wants to enumerate them.
func headlessCommands() []string {
	var out []string
	for _, c := range commands {
		if c.Headless {
			out = append(out, c.Name)
		}
	}
	sort.Strings(out)
	return out
}

// commandLines renders the visible commands as usage text, aligned.
func commandLines() string {
	const indent = "  "
	// Two columns, wrapping the summary onto its own line when the name and
	// args are too wide to leave room — which is what "connect TYPE TARGET"
	// did by hand before this was generated.
	const col = 21

	var b strings.Builder
	for _, c := range commands {
		if c.Hidden {
			continue
		}
		left := c.Name
		if c.Args != "" {
			left += " " + c.Args
		}
		if len(left) < col-len(indent) {
			b.WriteString(indent + left + strings.Repeat(" ", col-len(indent)-len(left)) + c.Summary + "\n")
			continue
		}
		b.WriteString(indent + left + "\n")
		b.WriteString(indent + strings.Repeat(" ", col-len(indent)) + c.Summary + "\n")
	}
	return b.String()
}
