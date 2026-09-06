// cmdPicker reproduces cmd_picker (sesh-bro:501-576, BEHAVIOUR.md §2.10,
// §3): assemble the picker's argument list, generate its row stream, and
// hand both to internal/picker's fzf integration.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/cyperx84/herdr-sesh-bro/internal/config"
	"github.com/cyperx84/herdr-sesh-bro/internal/external"
	"github.com/cyperx84/herdr-sesh-bro/internal/picker"
)

func cmdPicker(ctx context.Context, env *appEnv, args []string) int {
	cfg := config.Load(env.getenv)

	// sesh-bro:503: `local picker_args=(${SESH_BRO_ARGS:-} "$@")` — word-split
	// on IFS whitespace and PREPENDED to argv. strings.Fields reproduces the
	// whitespace-splitting half; bash's unquoted expansion here ALSO
	// undergoes pathname expansion (glob), which is not reproduced (same
	// class of divergence as internal/herdrx's blacklistMatches, §9 S6) —
	// harmless in practice because SESH_BRO_ARGS is only ever written by
	// `open` (open.go) as a space-joined run of "--flag" tokens, none of
	// which are glob metacharacters.
	pickerArgs := append(strings.Fields(env.getenv("SESH_BRO_ARGS")), args...)

	// sesh-bro:505-508: --hide-current anywhere in the COMBINED list sets
	// hide_flag, which is spliced into the reload binds ONLY — the initial
	// row stream below still gets the full argument list as-is.
	hideCurrent := false
	for _, a := range pickerArgs {
		if a == "--hide-current" {
			hideCurrent = true
		}
	}

	// sesh-bro:510-517: the configured default filter applies ONLY when the
	// combined list is completely empty — not merely lacking a source flag.
	if len(pickerArgs) == 0 {
		if flag, ok := cfg.DefaultFilterFlag(); ok {
			pickerArgs = append(pickerArgs, flag)
		}
	}

	// sesh-bro:519-520: picker's OWN dependency gate is herdr + fzf only —
	// not jq (this port has none), not the daemon (that's `list`'s job,
	// and its failure is handled below per S16, not here).
	if err := external.RequireHerdr(env.herdrBin); err != nil {
		fmt.Fprintln(env.stderr, err)
		return 1
	}
	if _, err := exec.LookPath("fzf"); err != nil {
		fmt.Fprintln(env.stderr, "sesh-bro: fzf is required")
		return 1
	}

	previewEnabled, err := cfg.PreviewEnabled()
	if err != nil {
		fmt.Fprintln(env.stderr, err)
		return 1
	}

	// Row generation: bash's `"$SELF" list "${picker_args[@]}" | fzf` runs
	// `list` as a concurrent subprocess piping into fzf — fzf's popup opens
	// immediately and rows stream in as `list` produces them. Reproduce that
	// concurrency with an io.Pipe + goroutine (rather than buffering the
	// whole row set before ever invoking fzf) so a slow daemon doesn't
	// delay the popup opening.
	//
	// S16 (BEHAVIOUR.md §9): if listOutput fails — daemon down, an invalid
	// flag arriving via $SESH_BRO_ARGS (never validated by cmd_picker
	// itself, sesh-bro:510-511's own comment), anything — bash's
	// `selection="$(... | fzf ...)" || true` swallows it: fzf simply sees
	// an empty (or truncated) stream and the picker still runs to
	// completion, exiting 0 unless a REAL selection's connect then fails.
	// Print the error, close the pipe, and let fzf open on whatever rows
	// (possibly zero) made it through before the failure.
	pr, pw := io.Pipe()
	go func() {
		if err := listOutput(ctx, env, append(pickerArgs, "--header"), pw); err != nil {
			fmt.Fprintln(env.stderr, err)
		}
		pw.Close()
	}()

	// What this fzf can do decides how live the picker is. Everything absent
	// degrades to the 0.3.0 behaviour rather than failing — see picker.Features.
	feats := picker.Detect(func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).Output()
	})

	opts := picker.Options{
		SelfPath:       env.self,
		Rows:           pr,
		HideCurrent:    hideCurrent,
		PreviewEnabled: previewEnabled,
		PreviewWidth:   cfg.PreviewWidth,
		Aliases:        cfg.Aliases,
		Keys:           cfg.Keys(),
		Stderr:         env.stderr,
		Fzf:            feats,
		// The counts line rides in as the first ROW rather than in fzf's
		// --header flag, because --header is fixed for the process's lifetime
		// while a header line is replaced by every reload — so the counts stay
		// live for free, on the mechanism already updating the list.
		HeaderLines: true,
	}

	// The live layer is strictly additive: if any part of it cannot be set up
	// — an old fzf, an unwritable TMPDIR, a daemon that will not take a
	// subscription — the picker still opens and still works, just as a
	// snapshot of the moment you pressed the key.
	if feats.Live() {
		if dir, err := runtimeDir(env.getenv, os.Getpid()); err == nil {
			defer os.RemoveAll(dir)
			opts.RowsDir = dir
			opts.ListenSocket = listenSocketPath(dir)
			stop := startLiveUpdates(ctx, env, cfg, dir, hideCurrent)
			defer stop()
		}
	}
	connector := func(kind, target string) error {
		client, openErr := openHerdr(env.getenv)
		return connect(ctx, env, client, openErr, kind, target)
	}

	if err := picker.Run(opts, connector); err != nil {
		return 1
	}
	return 0
}
