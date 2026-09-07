// `read` is sesh-bro's "what did that agent actually say?" surface.
//
// It returns the text an agent's pane currently shows, ANSI intact, so a
// human piping it keeps colour and a driving agent can read recent output
// without trying to parse a live TUI. The valuable source is
// recent-unwrapped: scrollback with terminal line-wrapping undone, which is
// the right shape for logs and transcripts.
package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	herdr "github.com/cyperx84/herdr-api"

	"github.com/cyperx84/herdr-sesh-bro/internal/external"
)

// keepANSI pins strip_ansi=false for every read this command makes. The
// server default is true, so a nil pointer or a plain bool with omitempty
// would silently strip the colour a human piping this wants — the same
// trap herdrx.ReadAgentVisibleANSI documents (BEHAVIOUR.md §6).
var keepANSI = false

var sourceNames = map[string]herdr.ReadSource{
	"visible":          herdr.ReadSourceVisible,
	"recent":           herdr.ReadSourceRecent,
	"recent-unwrapped": herdr.ReadSourceRecentUnwrapped,
}

type readFlags struct {
	target string
	source herdr.ReadSource
	lines  *uint32
	asJSON bool
}

func parseReadFlags(args []string) (readFlags, error) {
	f := readFlags{source: herdr.ReadSourceVisible}
	var positional []string

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json":
			f.asJSON = true
		case strings.HasPrefix(a, "--source="):
			if err := parseSourceFlag(&f, strings.TrimPrefix(a, "--source=")); err != nil {
				return f, err
			}
		case a == "--source":
			if i+1 >= len(args) {
				return f, fmt.Errorf("sesh-bro: read: --source requires a value")
			}
			i++
			if err := parseSourceFlag(&f, args[i]); err != nil {
				return f, err
			}
		case strings.HasPrefix(a, "--lines="):
			if err := parseLinesFlag(&f, strings.TrimPrefix(a, "--lines=")); err != nil {
				return f, err
			}
		case a == "--lines":
			if i+1 >= len(args) {
				return f, fmt.Errorf("sesh-bro: read: --lines requires a value")
			}
			i++
			if err := parseLinesFlag(&f, args[i]); err != nil {
				return f, err
			}
		case strings.HasPrefix(a, "-"):
			return f, fmt.Errorf("sesh-bro: read: unknown flag: %s", a)
		default:
			positional = append(positional, a)
		}
	}

	if len(positional) != 1 {
		return f, fmt.Errorf("sesh-bro: read: target required")
	}
	f.target = positional[0]
	return f, nil
}

func parseSourceFlag(f *readFlags, value string) error {
	src, ok := sourceNames[value]
	if !ok {
		return fmt.Errorf("sesh-bro: read: unknown source: %s", value)
	}
	f.source = src
	return nil
}

func parseLinesFlag(f *readFlags, value string) error {
	n, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return fmt.Errorf("sesh-bro: read: invalid --lines: %w", err)
	}
	lines := uint32(n)
	f.lines = &lines
	return nil
}

// cmdRead prints the current text of an agent's pane. Exit codes:
//
//	0 — text read successfully
//	1 — runtime failure (daemon down, herdr rejected the call)
//	2 — usage error
func cmdRead(ctx context.Context, env *appEnv, args []string) int {
	flags, err := parseReadFlags(args)
	if err != nil {
		writeReadOutput(env, flags.asJSON, "", err.Error())
		return 2
	}

	client, openErr := openHerdr(env.getenv)
	if err := external.CheckListDeps(ctx, env.herdrBin, aliverFor(client, openErr)); err != nil {
		writeReadOutput(env, flags.asJSON, "", err.Error())
		return 1
	}

	res, err := client.Raw().AgentRead(ctx, herdr.AgentReadParams{
		Target:    flags.target,
		Source:    flags.source,
		Format:    herdr.ReadFormatANSI,
		Lines:     flags.lines,
		StripANSI: &keepANSI,
	})
	if err != nil {
		writeReadOutput(env, flags.asJSON, "", err.Error())
		return 1
	}

	writeReadOutput(env, flags.asJSON, res.Text, "")
	return 0
}

func writeReadOutput(env *appEnv, asJSON bool, text, errMsg string) {
	if asJSON {
		results := []map[string]any{{"text": text}}
		writeHeadlessJSON(env.stdout, errMsg == "", "read", results, errMsg)
		return
	}
	if errMsg != "" {
		fmt.Fprintln(env.stderr, errMsg)
		return
	}
	fmt.Fprint(env.stdout, text)
}

// writeHeadlessJSON is defined in wait.go; this file shares the same
// machine-readable envelope.
