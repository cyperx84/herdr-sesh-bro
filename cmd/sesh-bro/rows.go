// `rows` prints a pre-rendered view file. It is internal plumbing for the
// picker's reload binds, not something a user runs.
//
// It exists because of an invariant that is easy to break by accident: fzf is
// told `--header-lines=1`, so EVERY stream it loads must begin with a header
// row. The row files always do. The alternative — having a bind re-execute
// `list` — only does when that bind remembers to pass `--header`, and three of
// them did not, which silently promoted the first real candidate into an
// unselectable header (BEHAVIOUR.md §10.6).
//
// Routing every reload through one command that reads the marker and cats the
// matching file removes the class of bug rather than the three instances:
// there is now one place that decides what a reload emits, and it cannot
// forget its own header. It also fixes a second defect in the same binds —
// close and create used to reload the default all-sources view regardless of
// which filter was active, and left the view marker untouched, so the next
// live push silently switched the list back to whatever the marker still said.
//
// This runs on every filter keypress, so it opens no socket, loads no config,
// and does nothing but read two files.
package main

import (
	"fmt"
	"io"
	"os"
)

func cmdRows(env *appEnv, args []string) int {
	dir := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dir":
			if i+1 >= len(args) {
				fmt.Fprintln(env.stderr, "sesh-bro rows: --dir needs a path")
				return 2
			}
			dir = args[i+1]
			i++
		default:
			fmt.Fprintf(env.stderr, "sesh-bro rows: unknown flag %s\n", args[i])
			return 2
		}
	}
	if dir == "" {
		fmt.Fprintln(env.stderr, "sesh-bro rows: --dir is required")
		return 2
	}

	f, err := os.Open(rowsFile(dir, readView(dir)))
	if err != nil {
		// An absent file means the renderer has not written this view yet.
		// Emitting nothing is right: fzf shows an empty list for a moment and
		// the next push fills it. Failing would tear down the picker over a
		// race with its own first render.
		return 0
	}
	defer f.Close()
	if _, err := io.Copy(env.stdout, f); err != nil {
		fmt.Fprintln(env.stderr, err)
		return 1
	}
	return 0
}
