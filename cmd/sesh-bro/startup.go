// cmdStartup is the manifest's [[startup]] hook (BEHAVIOUR.md §2.1): validate
// deps, report ok. Any arguments are silently ignored (the bash never read
// $1/$2 here either).
//
// It used to also clear a stale pane cache. That cache was removed in 0.4.0
// when session.snapshot replaced the four reads it existed to soften
// (BEHAVIOUR.md §10.1).
package main

import (
	"context"
	"fmt"

	"github.com/cyperx84/herdr-sesh-bro/internal/external"
)

func cmdStartup(ctx context.Context, env *appEnv) int {
	client, openErr := openHerdr(env.getenv)
	if err := external.CheckDeps(ctx, env.herdrBin, aliverFor(client, openErr)); err != nil {
		fmt.Fprintln(env.stderr, err)
		return 1
	}
	fmt.Fprintln(env.stdout, "sesh-bro: deps ok")
	return 0
}
