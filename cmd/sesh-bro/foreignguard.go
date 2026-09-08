package main

import (
	"fmt"

	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx"
)

// refuseForeign rejects a target that names another herdr session.
//
// Every command that acts on a target dials ONE socket — the local session's —
// and no herdr API call takes a session parameter. So a foreign target passed
// to `prompt`, `close`, `star`, `read` or `explain` does not fail: it silently
// acts on the local session instead, either missing entirely or, far worse,
// hitting a same-named agent here. herdr's own documentation says ids and
// agent names are scoped to one server and two of them can both hold `w1:p1`
// or an agent called `reviewer`.
//
// "It would succeed, change state the user cannot see, and report success" is
// the failure this one function exists to make impossible. It is a line at the
// top of each command rather than a design constraint precisely because the
// actions were built first: the refusal is cheap to add everywhere and
// expensive to remember anywhere.
//
// Exit 2, usage, not exit 1: the caller asked for something that cannot be
// done, rather than something that failed. A driving agent should stop asking,
// not retry.
func refuseForeign(command, target string) error {
	_, session, ok := herdrx.SplitForeignTarget(target)
	if !ok {
		return nil
	}
	return fmt.Errorf(
		"sesh-bro: %s: %s is in session %q, and no herdr API call takes a session — "+
			"open a terminal there with `sesh-bro connect session %s` instead",
		command, target, session, session)
}
