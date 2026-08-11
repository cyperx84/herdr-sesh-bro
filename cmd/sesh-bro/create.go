// cmdCreate reproduces cmd_create (sesh-bro:324-355, BEHAVIOUR.md §2.4):
// create a focused workspace for a directory, defaulting the path through a
// four-tier fallback (argument, plugin context JSON, an interactive prompt,
// $PWD) and bash's ~-expansion (including its S10 macOS bug, reproduced not
// fixed).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/cyperx84/herdr-sesh-bro/internal/external"
	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx"
)

func cmdCreate(ctx context.Context, env *appEnv, args []string) int {
	path := resolveCreatePath(env, args)
	// sesh-bro:337: unreachable in bash too — the $PWD fallback always
	// yields a non-empty string. Kept for parity with the documented exit
	// path (BEHAVIOUR.md §2.4 point 5).
	if path == "" {
		fmt.Fprintln(env.stderr, "sesh-bro create: no path")
		return 1
	}
	path = expandTilde(path, env.getenv)

	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		fmt.Fprintf(env.stderr, "sesh-bro create: not a directory: %s\n", path)
		return 1
	}
	external.ZoxideAdd(ctx, path)
	label := herdrx.Basename(path)

	client, openErr := openHerdr()
	// sesh-bro:353: `"$HERDR" workspace create ... --focus >/dev/null` —
	// note **stdout only** is discarded here; herdr's stderr passes
	// through IN ADDITION to sesh-bro's own message (BEHAVIOUR.md §2.4).
	if err := createWorkspace(ctx, client, openErr, path, label); err != nil {
		fmt.Fprintf(env.stderr, "sesh-bro: failed to create workspace for %s\n", path)
		fmt.Fprintln(env.stderr, err)
		return 1
	}
	return 0
}

// resolveCreatePath reproduces sesh-bro:325-336's four-tier path resolution,
// in order: the argument, HERDR_PLUGIN_CONTEXT_JSON's TOP-LEVEL
// focused_pane_cwd/workspace_cwd (deliberately NOT the recursive descent
// current_workspace_id uses — BEHAVIOUR.md §2.4 point 2 calls this out
// explicitly), an interactive tty prompt, then $PWD.
func resolveCreatePath(env *appEnv, args []string) string {
	if len(args) > 0 && args[0] != "" {
		return args[0]
	}
	if cwd, ok := contextCWD(env.getenv("HERDR_PLUGIN_CONTEXT_JSON")); ok {
		return cwd
	}
	if isTTY(env.stdin) {
		if p, ok := promptPath(env); ok {
			return p
		}
	}
	return resolvePWD(env)
}

// jqTruthy converts a decoded JSON value the way jq's `//` picks a STRING
// result: null and `false` are falsy (no match, per jq's own truthy/falsy
// rule) and fall through to the next `//` clause; an explicit empty string
// counts as present and is kept, matching jq's own semantics exactly.
//
// SIMPLIFICATION, DELIBERATE: real jq's `//` would also treat a truthy
// NON-string (a number, `true`, an object) as "found" and let `-r` attempt
// to stringify it. contextCWD only ever wants a genuine path string —
// herdr's focused_pane_cwd/workspace_cwd are documented as strings and
// never anything else — so a truthy non-string here is treated as "no
// usable value" (ok=false) rather than reproducing jq's stringification of
// an object or boolean into something that could never be a valid path
// anyway. This is unreachable via any input herdr actually sends.
func jqTruthy(v any) (string, bool) {
	if v == nil {
		return "", false
	}
	if b, ok := v.(bool); ok && !b {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// contextCWD reproduces sesh-bro:330 (BEHAVIOUR.md §2.4 point 2):
// `jq -r '.focused_pane_cwd // .workspace_cwd // empty'` against a TOP-LEVEL
// object — not the recursive `..` descent CurrentWorkspaceID performs for
// $HERDR_PLUGIN_CONTEXT_JSON elsewhere (§1.7). An explicit empty string for
// either key is KEPT by jq's `//` (only null/false fall through), matching
// jqTruthy above.
func contextCWD(contextJSON string) (string, bool) {
	if contextJSON == "" {
		return "", false
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(contextJSON), &m); err != nil {
		return "", false
	}
	if s, ok := jqTruthy(m["focused_pane_cwd"]); ok {
		return s, true
	}
	return jqTruthy(m["workspace_cwd"])
}

// isTTY reproduces bash's `[[ -t 0 ]]` (sesh-bro:332): stdin is a character
// device. Only meaningful when env.stdin is *os.File (a real fd) — a
// non-file Reader (as tests inject) is never a tty.
func isTTY(stdin io.Reader) bool {
	f, ok := stdin.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// promptPath reproduces sesh-bro:333 (`read -r -p "Path: " -e -i "$PWD"
// path`) as far as Go's standard library allows.
//
// DIVERGENCE FROM BASH, DELIBERATE: bash's `-e -i "$PWD"` is a readline
// prompt PRE-FILLED with $PWD — pressing Enter immediately accepts $PWD
// unedited, and the user can also arrow-edit it in place. Reproducing a
// prefilled, editable line needs raw-mode terminal control (à la
// golang.org/x/term), which is out of scope for this port to add as a new
// dependency for one interactive nicety — see picker.waitForKeypress's own
// doc comment for the same class of deviation. This prints the same "Path:
// " prompt and reads one line; an EMPTY line (just Enter, matching the
// no-edit case) resolves to $PWD, matching what a no-edit Enter would have
// submitted in bash. A non-empty line REPLACES $PWD outright rather than
// editing it in place — functionally equivalent for "type a full path and
// hit enter", the common case, but not for in-place editing of the
// prefilled text.
func promptPath(env *appEnv) (string, bool) {
	// `read -p` displays its prompt on STDERR (bash manual: "-p prompt:
	// display prompt on standard error"), not stdout — an interactive
	// `create` must not write "Path: " into anything a caller might be
	// capturing from stdout.
	fmt.Fprint(env.stderr, "Path: ")
	reader := bufio.NewReader(env.stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", false // sesh-bro:333: `read ... || path="$PWD"`
	}
	line = strings.TrimRight(line, "\n")
	if line == "" {
		if pwd := resolvePWD(env); pwd != "" {
			return pwd, true
		}
		return "", false
	}
	return line, true
}

// expandTilde reproduces sesh-bro:339-347 (BEHAVIOUR.md §2.4, §9 S10):
//
//   - "~/..." (including bare "~/"): $HOME + the rest.
//   - "~" or "~user" or "~user/...": resolve user's home via getent(1) if
//     it exists on PATH; bash's own fallback (`eval "printf '%s'
//     \"\$$user\""`, a shell-variable lookup that is empirically always
//     empty for a real username — S10) is not reproduced, since there is no
//     Go equivalent of "evaluate a variable named after arbitrary user
//     input" that isn't itself a footgun, and per S10 it never resolves to
//     anything on any platform this port targets anyway. On macOS, getent
//     does not exist (S10's own finding), so this branch is a no-op there,
//     identically to bash: the path is left untouched, exactly like S10
//     documents. `${path#*/}` requires a literal '/' to strip through — a
//     bare "~alice" with no slash is concatenated onto home AS-IS
//     ("$home/~alice"), reproducing S10's second bug rather than fixing it.
//   - anything else: unchanged.
func expandTilde(path string, getenv func(string) string) string {
	if strings.HasPrefix(path, "~/") {
		return getenv("HOME") + "/" + strings.TrimPrefix(path, "~/")
	}
	if !strings.HasPrefix(path, "~") {
		return path
	}
	rest := path[1:]
	user := rest
	slash := strings.IndexByte(rest, '/')
	if slash >= 0 {
		user = rest[:slash]
	}
	home, ok := getentHome(user)
	if !ok || home == "" {
		return path
	}
	if slash < 0 {
		return home + "/" + path // S10: "$home/${path}" — path, not rest.
	}
	return home + "/" + rest[slash+1:]
}

// getentHome shells out to `getent passwd <user>` (present on Linux, absent
// on macOS — the exact platform split S10 documents) and returns field 6
// (the home directory), matching `getent passwd "$user" | cut -d: -f6`.
func getentHome(user string) (string, bool) {
	if user == "" {
		return "", false
	}
	bin, err := exec.LookPath("getent")
	if err != nil {
		return "", false
	}
	out, err := exec.Command(bin, "passwd", user).Output()
	if err != nil {
		return "", false
	}
	fields := strings.Split(strings.TrimRight(string(out), "\n"), ":")
	if len(fields) < 6 || fields[5] == "" {
		return "", false
	}
	return fields[5], true
}
