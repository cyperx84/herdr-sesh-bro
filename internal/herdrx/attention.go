package herdrx

import (
	"sort"

	herdr "github.com/cyperx84/herdr-api"
)

// AttentionSet is every agent that wants the human, most urgent first:
// blocked before done, and within each group the most recent state change
// first, then name.
//
// Blocked and done are herdr's two "has something for you that you have not
// seen" states — blocked means it recognised an approval or question UI, done
// is the idle state reached by unseen background work and stays done until you
// look at the tab. Working and idle are excluded because neither is waiting on
// you, and unknown is excluded because herdr could not classify it: an
// unclassifiable agent has not been shown to want anything, and teleporting
// the user to one on a keypress would make the key untrustworthy.
func AttentionSet(agents []Agent) []Agent {
	set := make([]Agent, 0, len(agents))
	for _, a := range agents {
		a.Status = NormalizeStatus(a.Status)
		if a.Status == herdr.StatusBlocked || a.Status == herdr.StatusDone {
			set = append(set, a)
		}
	}
	sort.SliceStable(set, func(i, j int) bool {
		if ri, rj := agentRank(set[i].Status), agentRank(set[j].Status); ri != rj {
			return ri < rj
		}
		if si, sj := set[i].StateChangeSeq, set[j].StateChangeSeq; si != sj {
			return si > sj
		}
		return set[i].Name < set[j].Name
	})
	return set
}

// NextAttention picks the agent a next/prev keypress should focus, given the
// pane the user is currently on. dir is +1 for next, -1 for previous.
//
// The cycle is stateless on purpose: it derives entirely from the live
// snapshot and the focused pane, with no cursor file and no daemon of its own.
// That means it stays correct when the user navigates by other means between
// presses — which a stored index silently would not — and it cannot go stale
// or need pruning. The cost is that the traversal order can shift under you as
// agents change state, which is the right trade for a key whose job is "take
// me to whoever needs me now", not "walk a fixed list".
//
// Landing on the pane you are already looking at would make the key feel
// broken, so the currently focused pane is skipped. When the focused pane is
// not in the set at all — the common case, since you press this from wherever
// you were working — the first entry (or last, going backwards) wins.
//
// remaining is how many OTHER agents still want you after this jump, for the
// toast's "+N more". ok is false only when nothing wants the human.
//
// Prior art: milkyskies/herdr-attention cycles statelessly and skips the
// focused agent; Akram012388/herdr-checkin's retired design argued for
// evicting on focus, which herdr does for us by flipping done to idle once a
// tab is seen.
func NextAttention(set []Agent, focusedPane string, dir int) (target Agent, remaining int, ok bool) {
	if len(set) == 0 {
		return Agent{}, 0, false
	}
	if dir >= 0 {
		dir = 1
	} else {
		dir = -1
	}

	idx := -1
	for i, a := range set {
		if a.PaneID == focusedPane {
			idx = i
			break
		}
	}

	var next int
	switch {
	case idx < 0 && dir == 1:
		next = 0
	case idx < 0:
		next = len(set) - 1
	default:
		next = ((idx+dir)%len(set) + len(set)) % len(set)
	}

	// One-element set whose only member is the focused pane: there is
	// genuinely nowhere else to go, and re-focusing where you already are is
	// indistinguishable from a dead key. Report it as "nothing to jump to"
	// so the caller can say so out loud.
	if set[next].PaneID == focusedPane {
		return Agent{}, 0, false
	}
	return set[next], len(set) - 1, true
}
