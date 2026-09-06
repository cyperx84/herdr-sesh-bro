package herdrx

import (
	"testing"

	herdr "github.com/cyperx84/herdr-api"
)

func att(name, pane string, status herdr.AgentStatus, seq uint64) Agent {
	return Agent{
		Agent:          herdr.Agent{Name: name, PaneID: pane, Status: status},
		StateChangeSeq: seq,
	}
}

// Only blocked and done want you. Working is busy, idle is finished-and-seen,
// and unknown means herdr could not classify it — teleporting to one of those
// on a keypress would make the key untrustworthy.
func TestAttentionSetSelectsOnlyBlockedAndDone(t *testing.T) {
	in := []Agent{
		att("w", "p1", herdr.StatusWorking, 1),
		att("b", "p2", herdr.StatusBlocked, 2),
		att("i", "p3", herdr.StatusIdle, 3),
		att("d", "p4", herdr.StatusDone, 4),
		att("u", "p5", herdr.StatusUnknown, 5),
		att("e", "p6", "", 6),
	}
	got := AttentionSet(in)
	if len(got) != 2 {
		t.Fatalf("set = %d agents, want 2 (%v)", len(got), names(got))
	}
	if got[0].Name != "b" || got[1].Name != "d" {
		t.Errorf("set = %v, want [b d] — blocked before done", names(got))
	}
}

// Blocked outranks done regardless of recency, and within a rank the newest
// change leads.
func TestAttentionSetOrdering(t *testing.T) {
	in := []Agent{
		att("old-blocked", "p1", herdr.StatusBlocked, 1),
		att("new-done", "p2", herdr.StatusDone, 99),
		att("new-blocked", "p3", herdr.StatusBlocked, 50),
	}
	got := names(AttentionSet(in))
	want := []string{"new-blocked", "old-blocked", "new-done"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestNextAttentionEmptySet(t *testing.T) {
	if _, _, ok := NextAttention(nil, "p1", 1); ok {
		t.Error("ok = true for an empty set, want false")
	}
}

// The common case: you press the key from wherever you were working, which is
// not an agent that wants you. Forwards lands on the most urgent, backwards on
// the least.
func TestNextAttentionFocusOutsideSet(t *testing.T) {
	set := AttentionSet([]Agent{
		att("a", "p1", herdr.StatusBlocked, 30),
		att("b", "p2", herdr.StatusBlocked, 20),
		att("c", "p3", herdr.StatusBlocked, 10),
	})
	got, remaining, ok := NextAttention(set, "elsewhere", 1)
	if !ok || got.Name != "a" {
		t.Errorf("next = %q ok=%v, want a", got.Name, ok)
	}
	if remaining != 2 {
		t.Errorf("remaining = %d, want 2", remaining)
	}
	got, _, ok = NextAttention(set, "elsewhere", -1)
	if !ok || got.Name != "c" {
		t.Errorf("prev = %q ok=%v, want c", got.Name, ok)
	}
}

// Pressing it repeatedly walks the set and wraps, and never lands on the pane
// you are already looking at — a key that re-focuses the current pane is
// indistinguishable from a dead key.
func TestNextAttentionCyclesAndWraps(t *testing.T) {
	set := AttentionSet([]Agent{
		att("a", "p1", herdr.StatusBlocked, 30),
		att("b", "p2", herdr.StatusBlocked, 20),
		att("c", "p3", herdr.StatusBlocked, 10),
	})
	for _, tc := range []struct{ from, want string }{
		{"p1", "b"}, {"p2", "c"}, {"p3", "a"},
	} {
		got, _, ok := NextAttention(set, tc.from, 1)
		if !ok || got.Name != tc.want {
			t.Errorf("next from %s = %q ok=%v, want %q", tc.from, got.Name, ok, tc.want)
		}
	}
	for _, tc := range []struct{ from, want string }{
		{"p1", "c"}, {"p2", "a"}, {"p3", "b"},
	} {
		got, _, ok := NextAttention(set, tc.from, -1)
		if !ok || got.Name != tc.want {
			t.Errorf("prev from %s = %q ok=%v, want %q", tc.from, got.Name, ok, tc.want)
		}
	}
}

// One agent wants you and you are already on it: there is nowhere to jump, and
// saying so is better than silently re-focusing the same pane.
func TestNextAttentionSoleMemberIsFocused(t *testing.T) {
	set := AttentionSet([]Agent{att("only", "p1", herdr.StatusBlocked, 1)})
	if _, _, ok := NextAttention(set, "p1", 1); ok {
		t.Error("ok = true when the only attention agent is already focused, want false")
	}
}

func names(as []Agent) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = a.Name
	}
	return out
}
