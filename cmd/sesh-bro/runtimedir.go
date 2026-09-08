package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// runtimeDir returns a private scratch directory for one running picker,
// creating it.
//
// The path is deliberately short. It holds the fzf --listen socket, and macOS
// caps a unix socket path at 104 bytes — long enough that a nested temp
// directory silently breaks the listener rather than reporting anything
// useful. It lives under $TMPDIR (not $HOME) because it is per-process
// scratch that must not outlive a reboot, and it is per-uid because /tmp is
// shared.
//
// Each picker gets its own pid-named subdirectory so two open pickers, or a
// picker opened while a stale one leaked, never fight over the same files.
func runtimeDir(getenv func(string) string, pid int) (string, error) {
	base := getenv("TMPDIR")
	if base == "" {
		base = "/tmp"
	}
	dir := filepath.Join(base, fmt.Sprintf("sesh-bro-%d", os.Getuid()), fmt.Sprintf("p%d", pid))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("sesh-bro: create runtime dir: %w", err)
	}
	return dir, nil
}

// listenSocketPath is the fzf --listen socket inside a runtime directory.
// The ".sock" suffix is required, not cosmetic: fzf parses an argument
// without it as a PORT and opens a TCP listener instead.
func listenSocketPath(dir string) string { return filepath.Join(dir, "fzf.sock") }

// rowsFile is the pre-rendered row file for one view.
func rowsFile(dir, view string) string { return filepath.Join(dir, view+".tsv") }

// viewMarkerPath is where the picker records which view is on screen, so the
// pushing side knows which rows to re-send.
func viewMarkerPath(dir string) string { return filepath.Join(dir, "view") }

// readView returns the view currently displayed, defaulting to "all" when the
// marker is missing or unrecognised — the state the picker opens in.
func readView(dir string) string {
	b, err := os.ReadFile(viewMarkerPath(dir))
	if err != nil {
		return "all"
	}
	view := string(b)
	for _, known := range pickerViews {
		if view == known {
			return view
		}
	}
	return "all"
}

// pickerViews are the row files kept in sync, one per filter key plus the
// default. They exist as files so a filter keypress costs a file read rather
// than a process start and a daemon round trip.
var pickerViews = []string{"all", "workspaces", "agents", "blocked", "dirs", "worktrees", "issues"}

// viewFlags maps a view name to the `list` flags that produce it.
func viewFlags(view string) []string {
	switch view {
	case "workspaces":
		return []string{"--workspaces"}
	case "agents":
		return []string{"--agents"}
	case "blocked":
		return []string{"--blocked"}
	case "dirs":
		return []string{"--dirs"}
	case "worktrees":
		return []string{"--worktrees"}
	case "issues":
		return []string{"--issues"}
	default:
		return nil
	}
}

// pruneStaleRuntimeDirs removes runtime directories left behind by pickers
// that are no longer running.
//
// The normal exit path removes its own directory, but a picker killed by
// SIGKILL — or by herdr tearing the popup down — never gets there, and nothing
// else ever looked. Each leak is small, and that is exactly why it would have
// gone unnoticed indefinitely while $TMPDIR filled with sockets and row files.
//
// Liveness is `kill(pid, 0)`: it asks the kernel whether the process exists
// without touching it. The mtime guard exists because pids are recycled — a
// directory whose pid has been reissued to something unrelated would otherwise
// look alive forever — and because a picker that has just started may not have
// written anything yet, so a young directory is never removed even if its pid
// reads dead.
func pruneStaleRuntimeDirs(getenv func(string) string, self int) {
	base := getenv("TMPDIR")
	if base == "" {
		base = "/tmp"
	}
	root := filepath.Join(base, fmt.Sprintf("sesh-bro-%d", os.Getuid()))
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "p") {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimPrefix(e.Name(), "p"))
		if err != nil || pid == self {
			continue
		}
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) < staleRuntimeAge {
			continue
		}
		if err := syscall.Kill(pid, 0); err == nil {
			continue // still running
		}
		_ = os.RemoveAll(filepath.Join(root, e.Name()))
	}
}

// staleRuntimeAge is how long a directory must have been untouched before a
// dead pid is believed. Generous, because the cost of waiting is a few
// kilobytes and the cost of being wrong is deleting a live picker's files.
const staleRuntimeAge = time.Minute
