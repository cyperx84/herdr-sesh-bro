package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx"
	"github.com/cyperx84/herdr-sesh-bro/internal/herdrx/herdrtest"
)

// Without discovery, every headless invocation fails before it does anything:
// herdr only sets HERDR_SOCKET_PATH inside panes it spawned, so an agent
// running sesh-bro from an ordinary shell got an error naming a variable it
// had never heard of.
func TestOpenHerdrFallsBackToTheDefaultSession(t *testing.T) {
	s := herdrtest.Start(t)
	orig := listSessions
	listSessions = func(context.Context, herdrx.Runner) ([]herdrx.Session, error) {
		return []herdrx.Session{
			{Name: "default", Default: true, Running: true, SocketPath: s.Path()},
			{Name: "other", Running: false, SocketPath: "/nonexistent/other.sock"},
		}, nil
	}
	t.Cleanup(func() { listSessions = orig })

	s.Handle("workspace.list", func(json.RawMessage) (any, error) {
		return map[string]any{"workspaces": []map[string]any{}}, nil
	})

	client, err := openHerdr(fakeEnv(nil)) // no HERDR_SOCKET_PATH at all
	if err != nil {
		t.Fatalf("openHerdr with no env socket = %v, want the discovered one", err)
	}
	if !client.Alive(context.Background()) {
		t.Error("discovered client cannot reach the fake daemon")
	}
}

// An explicit socket always wins: a command run inside a herdr pane must talk
// to ITS session, never to whichever one discovery would have picked.
func TestOpenHerdrPrefersTheEnvironmentSocket(t *testing.T) {
	envSock := herdrtest.Start(t)
	other := herdrtest.Start(t)
	orig := listSessions
	listSessions = func(context.Context, herdrx.Runner) ([]herdrx.Session, error) {
		return []herdrx.Session{{Name: "default", Default: true, Running: true, SocketPath: other.Path()}}, nil
	}
	t.Cleanup(func() { listSessions = orig })

	envSock.Handle("workspace.list", func(json.RawMessage) (any, error) {
		return map[string]any{"workspaces": []map[string]any{}}, nil
	})

	client, err := openHerdr(fakeEnv(map[string]string{"HERDR_SOCKET_PATH": envSock.Path()}))
	if err != nil {
		t.Fatalf("openHerdr = %v", err)
	}
	if !client.Alive(context.Background()) {
		t.Error("client did not use the socket from the environment")
	}
	if len(other.CallOrder()) != 0 {
		t.Errorf("discovery was consulted despite an explicit socket: %v", other.CallOrder())
	}
}

// Discovery failing is not an error of its own — the caller still gets
// herdr-api's message naming the missing variable, which is the most useful
// thing to say when there is genuinely no daemon.
func TestOpenHerdrReportsTheOriginalErrorWhenDiscoveryFails(t *testing.T) {
	orig := listSessions
	listSessions = func(context.Context, herdrx.Runner) ([]herdrx.Session, error) {
		return nil, context.DeadlineExceeded
	}
	t.Cleanup(func() { listSessions = orig })

	if _, err := openHerdr(fakeEnv(nil)); err == nil {
		t.Fatal("openHerdr with no socket and failed discovery returned nil error")
	} else if strings.Contains(err.Error(), "DeadlineExceeded") {
		t.Errorf("leaked the discovery error instead of the useful one: %v", err)
	}
}
