// cmdOpen reproduces cmd_open (sesh-bro:580-595, BEHAVIOUR.md §2.9): open
// the picker popup through the herdr plugin API. This is the ONE place this
// port still shells out to the `herdr` CLI (BEHAVIOUR.md §6 note #12 —
// plugin.pane.open has no herdr-api method) — and, matching bash's `exec`,
// it REPLACES this process: herdr's own stdout, stderr, and exit code
// become sesh-bro's, and nothing after the exec ever runs.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// openFlags is the fixed set of flags `open` accepts — note `--json` is
// deliberately absent: `list` accepts it, `open` does not (BEHAVIOUR.md
// §2.2.9: "open rejects it, exit 2").
var openFlags = map[string]bool{
	"--workspaces": true, "--agents": true, "--dirs": true,
	"--blocked": true, "--working": true, "--done": true, "--idle": true,
	"--hide-current": true,
}

func cmdOpen(env *appEnv, args []string) int {
	for _, a := range args {
		if !openFlags[a] {
			fmt.Fprintf(env.stderr, "sesh-bro open: unknown flag %s\n", a)
			return 2
		}
	}

	pluginID := env.getenv("HERDR_PLUGIN_ID")
	if pluginID == "" {
		pluginID = "sesh-bro"
	}

	argv := []string{env.herdrBin, "plugin", "pane", "open", "--plugin", pluginID, "--entrypoint", "picker"}
	if len(args) > 0 {
		// sesh-bro:592: `$*` — default IFS, single-space-joined — is what
		// "$*" un-quoted in a --env=KEY=$* argument actually produces.
		argv = append(argv, "--env", "SESH_BRO_ARGS="+strings.Join(args, " "))
	}

	bin, err := exec.LookPath(env.herdrBin)
	if err != nil {
		// bash: `exec "$HERDR" ...` with $HERDR unresolvable is a shell
		// "command not found" — the shell itself prints that and exits
		// 127. There is no sesh-bro-authored message for this in bash
		// either (open never calls require_herdr), so none is added here.
		fmt.Fprintf(env.stderr, "sesh-bro: %s: command not found\n", env.herdrBin)
		return 127
	}

	// syscall.Exec REPLACES this process image, exactly like bash's `exec`
	// (sesh-bro:591, :594): herdr's stdout/stderr/exit code become this
	// program's. os.Environ() is the real process environment — this
	// function always runs against the real one (unlike the rest of this
	// package, which threads env.getenv everywhere for testability): a
	// test that reached this line would replace the TEST BINARY's own
	// process image, which is not something any test in this package does
	// or should do. If Exec itself fails (should not, given LookPath just
	// succeeded), fall through to a plain error.
	err = syscall.Exec(bin, argv, os.Environ())
	fmt.Fprintln(env.stderr, err)
	return 1
}
