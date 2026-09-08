package main

import (
	"strings"
	"testing"
)

// TestRefuseForeignAllowsLocalTargets: the guard sits at the top of commands
// whose targets are almost always local, so the common path must be silent.
func TestRefuseForeignAllowsLocalTargets(t *testing.T) {
	for _, local := range []string{"w1:p1", "builder", "", "/tmp"} {
		if err := refuseForeign("prompt", local); err != nil {
			t.Errorf("refuseForeign(%q) = %v, want nil", local, err)
		}
	}
}

// TestRefuseForeignNamesTheSessionAndTheWayOut. The error is the entire
// interface here: a user or a driving agent that only learns "no" will try
// again, and the thing they should do instead is open a terminal there.
func TestRefuseForeignNamesTheSessionAndTheWayOut(t *testing.T) {
	err := refuseForeign("prompt", "builder@work")
	if err == nil {
		t.Fatal("want a refusal")
	}
	msg := err.Error()
	for _, want := range []string{"prompt", "builder@work", `"work"`, "connect session work"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
}
