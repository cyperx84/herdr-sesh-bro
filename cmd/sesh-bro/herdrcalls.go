// Thin wrappers around herdrx.Client's calls for the six commands that never
// check daemon reachability up front (connect, create, preview, last, root,
// worktree — BEHAVIOUR.md §7.2). Each wrapper takes the (client, openErr)
// pair openHerdr produced: when openErr is non-nil the wrapper fails
// immediately with that same error, reproducing what bash gets for free —
// `"$HERDR" workspace focus …` with $HERDR unresolvable simply fails with a
// shell "command not found", which the surrounding `|| { echo "...";
// return 1; }` catches exactly like any other failure of that call. No
// distinct "herdr not found" message exists at these call sites in bash, and
// none is introduced here.
package main

import (
	"context"

	herdr "github.com/cyperx84/herdr-api"

	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx"
)

func focusWorkspace(ctx context.Context, client *herdrx.Client, openErr error, id string) error {
	if openErr != nil {
		return openErr
	}
	return client.FocusWorkspace(ctx, id)
}

func focusAgent(ctx context.Context, client *herdrx.Client, openErr error, target string) error {
	if openErr != nil {
		return openErr
	}
	return client.FocusAgent(ctx, target)
}

func createWorkspace(ctx context.Context, client *herdrx.Client, openErr error, cwd, label string) error {
	if openErr != nil {
		return openErr
	}
	_, err := client.CreateWorkspace(ctx, cwd, label)
	return err
}

func listWorkspaces(ctx context.Context, client *herdrx.Client, openErr error) ([]herdr.Workspace, error) {
	if openErr != nil {
		return nil, openErr
	}
	return client.ListWorkspaces(ctx)
}

func getWorkspace(ctx context.Context, client *herdrx.Client, openErr error, id string) (herdr.Workspace, error) {
	if openErr != nil {
		return herdr.Workspace{}, openErr
	}
	return client.GetWorkspace(ctx, id)
}

func listAgents(ctx context.Context, client *herdrx.Client, openErr error) ([]herdr.Agent, error) {
	if openErr != nil {
		return nil, openErr
	}
	return client.ListAgents(ctx)
}

func getAgent(ctx context.Context, client *herdrx.Client, openErr error, target string) (herdr.Agent, error) {
	if openErr != nil {
		return herdr.Agent{}, openErr
	}
	return client.GetAgent(ctx, target)
}
