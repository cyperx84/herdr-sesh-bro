package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A picker killed rather than closed never runs its own cleanup, so something
// has to sweep. The guards matter as much as the sweep: pids get recycled, and
// deleting a live picker's socket and row files would break the session the
// user is actually looking at.
func TestPruneStaleRuntimeDirs(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, fmt.Sprintf("sesh-bro-%d", os.Getuid()))
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}

	old := time.Now().Add(-2 * time.Minute)
	mk := func(name string, mtime time.Time) string {
		d := filepath.Join(root, name)
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(d, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		return d
	}

	self := os.Getpid()
	dead := mk("p999999", old)                 // pid cannot exist; old enough to believe
	alive := mk(fmt.Sprintf("p%d", self), old) // this test process
	young := mk("p999998", time.Now())         // dead pid but too fresh to trust
	notOurs := mk("scratch", old)              // not a pNNN directory at all

	pruneStaleRuntimeDirs(fakeEnv(map[string]string{"TMPDIR": tmp}), self)

	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Error("a dead picker's directory survived the sweep")
	}
	for name, d := range map[string]string{
		"the running picker's own directory": alive,
		"a directory too young to judge":     young,
		"an unrelated directory":             notOurs,
	} {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("sweep removed %s: %v", name, err)
		}
	}
}

// A missing root is the normal case on a machine that has never opened the
// picker, and must not be an error.
func TestPruneStaleRuntimeDirsNoRoot(t *testing.T) {
	pruneStaleRuntimeDirs(fakeEnv(map[string]string{"TMPDIR": t.TempDir()}), os.Getpid())
}
