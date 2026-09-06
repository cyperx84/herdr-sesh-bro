// cmdStartup reproduces cmd_startup (sesh-bro:121-125, BEHAVIOUR.md §2.1):
// the manifest's [[startup]] hook — validate deps, clear the stale pane
// cache, report ok. Any arguments are silently ignored (bash never reads
// $1/$2 here either).
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
