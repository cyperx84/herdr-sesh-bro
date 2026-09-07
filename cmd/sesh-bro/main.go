// Command sesh-bro is the Go port of the bash sesh-bro script: a sesh-style
// fuzzy picker for herdr workspaces, agents, and zoxide directories.
//
// This file owns dispatch (main → run, mirroring sesh-bro:638-658's `main()`)
// and the two pieces of process-wide startup bash performs before any
// subcommand runs (BEHAVIOUR.md §1): resolving the binary's own path (§1.2)
// and reading VERSION from the manifest next to it (§1.3). Every subcommand's
// actual behaviour lives in its own file (list.go, connect.go, ...) — see
// each file's doc comment for the exact bash lines and BEHAVIOUR.md section
// it reproduces.
//
// Every exported-from-bash message and exit code in this package is cited
// against BEHAVIOUR.md; where this port cannot reproduce bash's exact
// mechanism (a source-line diagnostic from `set -u`, a raw-mode single
// keypress, readline's prefilled prompt), the comment at that call site says
// so and names the divergence — see also this task's final report.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// run is main's testable body: every dependency main() would otherwise pull
// from the process (args, streams, environment) is a parameter, so tests can
// drive the full dispatch table without touching os.Args/os.Stdin/os.Getenv.
// The return value is the process exit code — never called with os.Exit
// itself, so a deferred cleanup in any subcommand still runs.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	self := resolveSelf()
	version := resolveVersion(self, os.ReadFile)

	// sesh-bro:638-640: no argument defaults to "picker"; the command word,
	// if present, is consumed and never seen again by the subcommand's own
	// flag handling (there is no global flag parsing before it, so
	// `sesh-bro --workspaces` is an UNKNOWN COMMAND, not `list --workspaces`
	// — BEHAVIOUR.md §2.0: "commands are matched exactly").
	cmd := "picker"
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}

	herdrBin := getenv("HERDR_BIN_PATH")
	if herdrBin == "" {
		herdrBin = "herdr"
	}

	ctx := context.Background()
	env := &appEnv{
		getenv:   getenv,
		herdrBin: herdrBin,
		self:     self,
		version:  version,
		stdin:    stdin,
		stdout:   stdout,
		stderr:   stderr,
	}

	switch cmd {
	case "picker":
		return cmdPicker(ctx, env, args)
	case "list":
		return cmdList(ctx, env, args)
	case "counts":
		return cmdCounts(ctx, env, args)
	case "rows":
		return cmdRows(env, args)
	case "record-event":
		return cmdRecordEvent(ctx, env)
	case "next":
		return cmdNext(ctx, env, args, 1)
	case "prev":
		return cmdNext(ctx, env, args, -1)
	case "connect":
		return cmdConnect(ctx, env, args)
	case "close":
		// docs/COMPETITIVE-DEMAND.md #1 — no bash counterpart; see
		// close.go's doc comment for why this sits next to connect.
		return cmdClose(ctx, env, args)
	case "create":
		return cmdCreate(ctx, env, args)
	case "preview":
		return cmdPreview(ctx, env, args)
	case "open":
		return cmdOpen(env, args)
	case "startup":
		return cmdStartup(ctx, env)
	case "last":
		return cmdLast(ctx, env)
	case "root":
		return cmdRoot(ctx, env)
	case "worktree":
		return cmdWorktree(ctx, env, args)
	case "-h", "--help", "help":
		// BEHAVIOUR.md §2.0: usage on STDOUT, exit 0.
		fmt.Fprint(stdout, usage(version))
		return 0
	case "-v", "--version":
		fmt.Fprintf(stdout, "sesh-bro %s\n", version)
		return 0
	default:
		// BEHAVIOUR.md §2.0: unknown command → message on stderr, then the
		// FULL usage text also on stderr (not stdout), exit 2.
		fmt.Fprintf(stderr, "sesh-bro: unknown command %s\n", cmd)
		fmt.Fprint(stderr, usage(version))
		return 2
	}
}

// appEnv is every piece of process context a subcommand needs, resolved
// once by run and threaded through explicitly — the Go equivalent of bash's
// process-wide globals ($HERDR, $SELF, $VERSION, and the ambient
// stdin/stdout/stderr/environment every function closes over for free).
type appEnv struct {
	getenv   func(string) string
	herdrBin string // resolved $HERDR_BIN_PATH, or "herdr" (sesh-bro:6)
	self     string // resolved $SELF (sesh-bro:12-19), "" if unresolvable
	version  string // resolved $VERSION (sesh-bro:21-25)
	stdin    io.Reader
	stdout   io.Writer
	stderr   io.Writer
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv))
}

// resolveSelf reproduces bash's $SELF (sesh-bro:12-19, BEHAVIOUR.md §1.2):
// the script's own real path, symlinks followed, so a `herdr plugin install`
// symlink onto PATH still resolves to the real install directory next to
// herdr-plugin.toml.
//
// DIVERGENCE FROM BASH, DELIBERATE: bash's four-tier fallback
// (readlink -f → realpath → cd+pwd → command -v sesh-bro) exists because
// bash's $0 can be a relative path, a bare command name found via PATH, or
// simply wrong if the script was sourced oddly. Go's os.Executable() has no
// equivalent ambiguity — it asks the OS directly for the running binary's
// path and is reliable on every platform this port targets — so tiers 3-4
// (relevant only when $0 itself cannot be resolved or read) have no
// counterpart here and are not reproduced; there is no known case where
// os.Executable() fails but bash's $0 resolution would have succeeded.
// filepath.EvalSymlinks is tier 1-2's direct equivalent (readlink -f/
// realpath both fully resolve symlinks; EvalSymlinks does the same).
func resolveSelf() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		// exe itself is unusable for EvalSymlinks (e.g. deleted mid-run) —
		// fall back to the unresolved path rather than "", which would
		// disable version resolution and the picker's self-reinvocation
		// entirely for no reason.
		return exe
	}
	return real
}

// manifestVersionRe is bash's `sed -n 's/^version = "\(.*\)"/\1/p'`
// (sesh-bro:88, BEHAVIOUR.md §1.3) as a Go regexp: anchored at the start of
// a line so `min_herdr_version = "0.8.0"` never matches, greedy up to the
// LAST `"` on the matching line (irrelevant in practice — the manifest's
// version line has exactly one trailing quote).
var manifestVersionRe = regexp.MustCompile(`(?m)^version = "(.*)"`)

// resolveVersion reproduces bash's VERSION resolution (sesh-bro:87-89,
// BEHAVIOUR.md §1.3): read herdr-plugin.toml from next to the binary, take
// the FIRST `^version = "..."` match, fall back to "0.1.0" if the manifest
// is missing or unparseable. This is deliberately a runtime read, not a
// build-time constant — the manifest stays the single source of truth so
// the two can never drift, exactly as BEHAVIOUR.md §1.3 requires of the
// port.
//
// The installed binary lands at <plugin-root>/bin/sesh-bro (herdr-plugin.toml
// §build); the bash script instead lived at <plugin-root>/sesh-bro, directly
// beside the manifest. So unlike bash's single dirname(SELF) lookup, this
// probes two locations: dir(self)/herdr-plugin.toml (self beside the
// manifest, matching a `go build -o sesh-bro` run from the repo root during
// development) and dir(self)/../herdr-plugin.toml (self one directory below
// it, matching the installed bin/ layout) — first hit wins.
func resolveVersion(self string, readFile func(string) ([]byte, error)) string {
	const fallback = "0.1.0"
	if self == "" {
		return fallback
	}
	dir := filepath.Dir(self)
	for _, candidate := range []string{
		filepath.Join(dir, "herdr-plugin.toml"),
		filepath.Join(dir, "..", "herdr-plugin.toml"),
	} {
		data, err := readFile(candidate)
		if err != nil {
			continue
		}
		if m := manifestVersionRe.FindSubmatch(data); m != nil {
			return string(m[1])
		}
	}
	return fallback
}

// resolvePWD reproduces bash's $PWD: the shell's own idea of the current
// directory, which is a shell variable independent of (though normally in
// sync with) the process's actual working directory — used by `root`
// (sesh-bro:374, `git -C "$PWD"`) and `create`'s final fallback
// (sesh-bro:335). getenv("PWD") first, then os.Getwd() if that is unset —
// covers both "invoked from an interactive shell" (PWD is always set there)
// and "invoked with a bare environment" (PWD absent, cwd still resolvable).
func resolvePWD(env *appEnv) string {
	if pwd := env.getenv("PWD"); pwd != "" {
		return pwd
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

// usage is bash's usage() (sesh-bro:598-636, BEHAVIOUR.md §2.0), verbatim
// except for $VERSION interpolation on the first line. Printed to stdout for
// -h/--help/help (exit 0) and to stderr for an unknown command (exit 2) —
// callers choose the stream, this function only builds the text.
func usage(version string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "sesh-bro %s — sesh-style fuzzy session picker for Herdr\n\n", version)
	b.WriteString(`usage: sesh-bro <command> [flags]

commands:
  picker [flags]     open the fzf picker (default)
  list   [flags]     print picker candidates (type, target, display)
  next               focus the next agent needing attention (blocked, then done)
  prev               same, backwards
  counts [flags]     one line: how many agents are blocked/working/done/idle
                     (--ansi colour, --json, --all to include zeros)
  connect TYPE TARGET
                     focus a workspace/agent, or create a workspace for a dir
  close TYPE TARGET  close a workspace (workspace rows only; picker alt-x)
  create [PATH]      create a workspace for a directory (default: current dir)
  preview TYPE TARGET
                     render the preview used by the picker
  open   [flags]     open the picker popup through the Herdr plugin API
  startup            validate deps + clear stale cache (manifest startup hook)
  last               focus the previously-focused workspace
  root               focus/create the workspace for the current git root
  worktree [URL]     create/focus the workspace for a GitHub issue/PR
  -h, --help         show this help
  -v, --version      print the version

flags:
  --workspaces       only Herdr workspaces
  --agents           only agents (across all workspaces)
  --dirs             only zoxide directories
  --blocked          only blocked agents (needs attention)
  --working          only working agents
  --done             only done agents
  --idle             only idle agents
  --hide-current     hide the current workspace and its agents
  --json             machine-readable list output

environment:
  HERDR_BIN_PATH     the herdr binary to use (default: herdr on PATH)
  SESH_BRO_*         config overrides (preview_width, hide_current, dir_sources,
                     attention_first, default_filter, blacklist, icons, keys, aliases...)
`)
	return b.String()
}
