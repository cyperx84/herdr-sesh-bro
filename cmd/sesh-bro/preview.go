// cmdPreview reproduces cmd_preview (sesh-bro:440-498, BEHAVIOUR.md §2.8):
// render the fzf preview pane for a workspace, agent, or directory row.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	herdr "github.com/cyperx84/herdr-api"

	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx"
	"github.com/cyperx84/herdr-sesh-bro/internal/render"
)

func cmdPreview(ctx context.Context, env *appEnv, args []string) int {
	// BEHAVIOUR.md §9 S9: same `set -u` unbound-variable death as connect,
	// same divergence note applies (exit 1, our own message, not bash's
	// source-line diagnostic).
	if len(args) < 2 {
		fmt.Fprintln(env.stderr, "sesh-bro: preview: missing TYPE/TARGET arguments")
		return 1
	}
	kind, target := args[0], args[1]
	client, openErr := openHerdr(env.getenv)

	switch kind {
	case "workspace":
		return previewWorkspace(ctx, env, client, openErr, target)
	case "agent":
		return previewAgent(ctx, env, client, openErr, target)
	case "dir":
		return previewDir(env, target)
	default:
		fmt.Fprint(env.stdout, render.PreviewUnknown())
		return 0
	}
}

// previewWorkspace reproduces sesh-bro:443-458.
//
// EXIT CODE, CORRECTING THE SECTION'S OWN SUMMARY CLAIM: BEHAVIOUR.md §2.8's
// header says preview "always exits 0 ... including when the target does
// not exist". That is true only for the explicit not-found `return 0`
// (verified below). For the SUCCESS path it is not: `pane="$(...)"` is a
// plain assignment under `set -euo pipefail`, so a failing `pane list
// --workspace` aborts the script immediately; and the final
// `[[ -n $pane ]] && "$HERDR" pane read ...` is the LAST statement of a
// bash FUNCTION — confirmed empirically (bash -c reproduction, not just the
// doc) that a function's own return status, even one earned inside an
// exempted &&/|| list, IS subject to `set -e` at the function's CALL SITE,
// even though `set -e` does not abort execution mid-function for that same
// list. So an empty pane list, or a failing `pane read`, both propagate as
// a non-zero script exit — the doc's own note on `preview agent` flags
// exactly this as "[UNVERIFIED]"; it holds for `preview workspace` too, by
// the identical mechanism, and is reproduced here (exit 1) rather than
// smoothed over into an unconditional 0.
func previewWorkspace(ctx context.Context, env *appEnv, client *herdrx.Client, openErr error, target string) int {
	ws, err := getWorkspace(ctx, client, openErr, target)
	if err != nil {
		fmt.Fprint(env.stdout, render.PreviewWorkspaceNotFound(target))
		return 0
	}
	// sesh-bro:452: `.result.workspace.label // ""` — NOT `list`'s
	// label-or-workspace_id fallback (WorkspaceRows). Preview shows a blank
	// label rather than falling back to the id; do not reuse WorkspaceRows'
	// logic here.
	fmt.Fprint(env.stdout, render.PreviewWorkspaceHeader(string(herdrx.NormalizeStatus(ws.Status)), ws.Label, target))

	// sesh-bro:455: `herdr pane list --workspace <id>` — fresh, UNCACHED;
	// this does not go through paneList()/the pane-cache file at all.
	panes, err := listPanesScoped(ctx, client, openErr, target)
	if err != nil {
		return 1
	}
	paneID, ok := herdrx.FocusedOrFirstPane(panes)
	if !ok {
		return 1
	}
	text, err := readPaneANSI(ctx, client, openErr, paneID)
	if err != nil {
		return 1
	}
	fmt.Fprint(env.stdout, text)
	return 0
}

// previewAgent reproduces sesh-bro:459-473. Same exit-code note as
// previewWorkspace applies to `agent read`'s failure (BEHAVIOUR.md §2.8's
// own "[UNVERIFIED]" flag on this exact point) — reproduced as exit 1.
func previewAgent(ctx context.Context, env *appEnv, client *herdrx.Client, openErr error, target string) int {
	ag, err := getAgent(ctx, client, openErr, target)
	if err != nil {
		fmt.Fprint(env.stdout, render.PreviewAgentNotFound(target))
		return 0
	}
	status := string(herdrx.NormalizeStatus(ag.Status))
	name := ag.Name
	if name == "" && ag.Agent != nil {
		name = *ag.Agent
	}
	fmt.Fprint(env.stdout, render.PreviewAgentHeader(status, name, ag.CWD))

	// Why herdr says that, when it can tell us. One extra RPC, and the preview
	// is the one place in the picker where that is affordable: it runs for the
	// highlighted row only, not for every row on every render, so this cannot
	// become the per-event RPC storm §10 spent 0.4.1 removing.
	//
	// Errors are swallowed on purpose. agent.explain arrived in herdr 0.9.0
	// and the plugin still supports 0.8.2, where the method does not exist; a
	// preview that failed, or printed a diagnostic about an unsupported
	// method, would be worse on that daemon than one that simply says less.
	if ex, err := client.ExplainAgent(ctx, target); err == nil {
		fmt.Fprint(env.stdout, render.PreviewAgentReason(ex.Summary()))
	}

	text, err := readAgentANSI(ctx, client, openErr, target)
	if err != nil {
		return 1
	}
	fmt.Fprint(env.stdout, text)
	return 0
}

// previewDir reproduces sesh-bro:474-495: a directory listing (eza, falling
// back to a bare `ls -la` reproduction) plus the first 40 lines of any
// case-insensitive `readme*` file in the directory (bat, falling back to a
// plain reproduction), in FILESYSTEM order for the README pick — not
// sorted, matching `find -maxdepth 1`.
func previewDir(env *appEnv, target string) int {
	fmt.Fprint(env.stdout, render.PreviewDirHeader(target))
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		fmt.Fprint(env.stdout, render.PreviewDirNotFound())
		return 0
	}
	writeDirListing(env.stdout, target)
	if readme, ok := findReadme(target); ok {
		fmt.Fprint(env.stdout, render.PreviewDirReadmeSeparator(readme))
		writeReadmeBody(env.stdout, filepath.Join(target, readme))
	}
	return 0
}

// listPanesScoped is `herdr pane list --workspace <id>`, uncached — the one
// place this port calls ListPanes with a non-empty workspace id, matching
// BEHAVIOUR.md §2.8's note that this bypasses the pane-cache file entirely.
func listPanesScoped(ctx context.Context, client *herdrx.Client, openErr error, workspaceID string) ([]herdr.Pane, error) {
	if openErr != nil {
		return nil, openErr
	}
	return client.ListPanes(ctx, workspaceID)
}

func readPaneANSI(ctx context.Context, client *herdrx.Client, openErr error, paneID string) (string, error) {
	if openErr != nil {
		return "", openErr
	}
	return client.ReadPaneVisibleANSI(ctx, paneID)
}

func readAgentANSI(ctx context.Context, client *herdrx.Client, openErr error, target string) (string, error) {
	if openErr != nil {
		return "", openErr
	}
	return client.ReadAgentVisibleANSI(ctx, target)
}

// writeDirListing reproduces sesh-bro:480-484 exactly: eza if on PATH, else
// the REAL `ls -la` — bash's own fallback execs the actual `ls` binary, not
// a reimplementation of it, and `ls` exists on every platform this port
// targets, so this shells out on both branches rather than hand-rolling a
// second, inevitably-divergent listing format (no total line, wrong date
// formatting, wrong column widths) for whichever branch happens to run on a
// machine without eza installed. Either branch's own failure is swallowed,
// matching bash's `2>/dev/null` on both.
func writeDirListing(w io.Writer, dir string) {
	if bin, err := exec.LookPath("eza"); err == nil {
		out, _ := exec.Command(bin, "-la", "--color=always", "--group-directories-first", "--no-user", "--time-style=relative", dir).Output()
		_, _ = w.Write(out)
		return
	}
	if bin, err := exec.LookPath("ls"); err == nil {
		out, _ := exec.Command(bin, "-la", dir).Output()
		_, _ = w.Write(out)
	}
}

// findReadme reproduces sesh-bro:486 (`find "$target" -maxdepth 1 -iname
// 'readme*' -type f | head -1`): the first case-insensitive `readme*`
// regular file in FILESYSTEM order — os.File.Readdirnames (not
// os.ReadDir, which sorts lexically) preserves that order. Returns the
// bare filename, not a path.
func findReadme(dir string) (string, bool) {
	f, err := os.Open(dir)
	if err != nil {
		return "", false
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		return "", false
	}
	for _, name := range names {
		if !strings.HasPrefix(strings.ToLower(name), "readme") {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.IsDir() {
			continue
		}
		return name, true
	}
	return "", false
}

// writeReadmeBody reproduces sesh-bro:489-492: bat if on PATH (plain style,
// no paging, first 40 lines, ANSI colour preserved); otherwise the first 40
// lines verbatim with no styling — bash's own `sed -n '1,40p'` fallback.
func writeReadmeBody(w io.Writer, path string) {
	if bin, err := exec.LookPath("bat"); err == nil {
		out, _ := exec.Command(bin, "--color=always", "--style=plain", "--paging=never", "--line-range", ":40", path).Output()
		_, _ = w.Write(out)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.SplitAfter(string(data), "\n")
	if len(lines) > 40 {
		lines = lines[:40]
	}
	for _, l := range lines {
		fmt.Fprint(w, l)
	}
}
