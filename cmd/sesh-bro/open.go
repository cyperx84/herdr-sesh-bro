// cmdOpen opens the picker popup through the herdr plugin API — the ONE place
// this port still shells out to the `herdr` CLI, because plugin.pane.open has
// no herdr-api method (BEHAVIOUR.md §6 note #12).
//
// Since 0.4.0 it also TOGGLES (BEHAVIOUR.md §10.7). Pressing the same chord
// again used to hit herdr's "popup already open" error and leave the popup
// sitting there, so opening and closing were different gestures for something
// the user thinks of as one. Now a second open closes it.
//
// That costs the `exec` semantics bash had: this process must survive the
// child in order to react to its failure, so herdr runs as a subprocess whose
// output and exit code are forwarded rather than inherited (§10.7).
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
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

	// Run herdr as a child rather than replacing this process, so its failure
	// is observable. Output is captured and forwarded verbatim, and the exit
	// code is passed through, so every case except the toggle below behaves
	// exactly as the previous `exec` did from the caller's point of view.
	cmd := exec.Command(bin, argv[1:]...)
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	if runErr != nil && isPopupAlreadyOpen(stdout.String()+stderr.String()) {
		// The popup is open and the user pressed the chord again: close it.
		// Do NOT forward herdr's error — from the user's side this is a
		// successful toggle, not a failed open.
		client, openErr := openHerdr(env.getenv)
		if openErr != nil {
			fmt.Fprint(env.stderr, stderr.String())
			return 1
		}
		if err := client.ClosePopup(context.Background()); err != nil {
			// popup_not_open is a race, not a fault: something closed it
			// between herdr's refusal and this call, which is the state the
			// user was asking for anyway.
			if !isPopupNotOpen(err.Error()) {
				fmt.Fprintln(env.stderr, err)
				return 1
			}
		}
		return 0
	}

	fmt.Fprint(env.stdout, stdout.String())
	fmt.Fprint(env.stderr, stderr.String())
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return exitErr.ExitCode()
		}
		return 1
	}
	return 0
}

// isPopupAlreadyOpen recognises herdr's refusal to stack a second popup.
//
// Matching on the message rather than a code because plugin.pane.open reports
// this as plugin_pane_open_failed — the same code it uses for unrelated
// failures — so the code alone would turn any open error into a close.
func isPopupAlreadyOpen(out string) bool {
	return strings.Contains(out, "popup already open")
}

// isPopupNotOpen recognises popup.close's complaint that there was nothing to
// close, which is a race rather than a fault.
func isPopupNotOpen(msg string) bool {
	return strings.Contains(msg, "popup_not_open")
}
