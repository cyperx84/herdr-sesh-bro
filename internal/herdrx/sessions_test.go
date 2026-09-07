package herdrx

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// realSessionsJSON is a verbatim capture of `herdr session list --json` on a
// live herdr 0.8.2 with two sessions (docs/MULTI-SESSION.md). Tests built on
// a hand-rolled shape would keep passing if the wire format drifted in a way
// this package stopped decoding; this one can only pass if the real CLI's
// output still parses.
const realSessionsJSON = `{"sessions":[{"default":true,"name":"default","running":true,
  "session_dir":"/Users/x/.config/herdr",
  "socket_path":"/Users/x/.config/herdr/herdr.sock"},
 {"default":false,"name":"main","running":false,
  "session_dir":"/Users/x/.config/herdr/sessions/main",
  "socket_path":"/Users/x/.config/herdr/sessions/main/herdr.sock"}]}`

// cannedRunner returns a Runner whose calls must be exactly `session list
// --json` on the expected binary — a ListSessions that invoked anything else
// (say, an RPC-shaped flag or a stray glob) would pass its output through a
// fake that never recognizes the call, so the test fails on invocation, not
// later on decode.
func cannedRunner(wantBin string, out string) Runner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name != wantBin {
			return nil, errors.New("unexpected binary: " + name)
		}
		if strings.Join(args, " ") != "session list --json" {
			return nil, errors.New("unexpected args: " + strings.Join(args, " "))
		}
		return []byte(out), nil
	}
}

func TestListSessions_RealShape(t *testing.T) {
	sessions, err := ListSessions(context.Background(), "", cannedRunner("herdr", realSessionsJSON))
	if err != nil {
		t.Fatalf("ListSessions(real shape): %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}
	def := sessions[0]
	if def.Name != "default" || !def.Default || !def.Running {
		t.Fatalf("first session = %+v, want the default running one", def)
	}
	if def.SocketPath != "/Users/x/.config/herdr/herdr.sock" {
		t.Errorf("default socket = %q, want %q", def.SocketPath, "/Users/x/.config/herdr/herdr.sock")
	}
	// THE globbing trap, asserted so nobody "simplifies" enumeration into
	// walking sessions/ later (docs/MULTI-SESSION.md): the default session
	// lives directly under ~/.config/herdr, NOT in sessions/. If this
	// assertion ever fails, the fixture drifted from reality, not the code.
	sessionsSub := filepath.Join("sessions", "default")
	if strings.Contains(def.SessionDir, sessionsSub) {
		t.Errorf("default session_dir %q is under sessions/ — the real CLI puts the default session in ~/.config/herdr itself; a glob-based enumeration would miss it", def.SessionDir)
	}
	main := sessions[1]
	if main.Name != "main" || main.Default || main.Running {
		t.Fatalf("second session = %+v, want the stopped non-default one", main)
	}
}

func TestListSessions_ExplicitBinary(t *testing.T) {
	// herdrBin must reach the runner verbatim. If ListSessions hardcoded
	// "herdr" (or appended to it), a caller pointing at a pinned binary
	// would silently query whatever is on PATH instead.
	if _, err := ListSessions(context.Background(), "/opt/herdr/bin/herdr", cannedRunner("/opt/herdr/bin/herdr", realSessionsJSON)); err != nil {
		t.Fatalf("ListSessions with explicit binary: %v", err)
	}
}

func TestListSessions_Errors(t *testing.T) {
	// A runner failure or malformed JSON must surface as an error, not a
	// panic or a silently-empty list — a headless caller treats "cannot
	// enumerate" and "no sessions" very differently (fail loudly vs.
	// report not-found).
	cases := []struct {
		name string
		run  Runner
	}{
		{
			name: "runner fails",
			run: func(context.Context, string, ...string) ([]byte, error) {
				return nil, errors.New("exit status 1")
			},
		},
		{
			name: "malformed JSON",
			run: func(context.Context, string, ...string) ([]byte, error) {
				return []byte(`{"sessions":[{"name":`), nil
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sessions, err := ListSessions(context.Background(), "", tc.run)
			if err == nil {
				t.Fatalf("ListSessions(%s) returned sessions=%v with nil error, want an error", tc.name, sessions)
			}
			if !strings.HasPrefix(err.Error(), "herdrx: list sessions:") {
				t.Errorf("error %q lacks the herdrx: prefix herdrx.go's wrap style requires", err)
			}
		})
	}
}

func TestDefaultRunningSocket(t *testing.T) {
	defRunning := Session{Name: "default", Default: true, Running: true, SocketPath: "/d/herdr.sock"}
	defStopped := Session{Name: "default", Default: true, Running: false, SocketPath: "/d/herdr.sock"}
	mainRunning := Session{Name: "main", Running: true, SocketPath: "/s/main/herdr.sock"}
	altRunning := Session{Name: "alt", Running: true, SocketPath: "/s/alt/herdr.sock"}

	cases := []struct {
		name      string
		sessions  []Session
		wantSock  string
		wantFound bool
		// why documents what a failure of this case would MEAN, per house
		// style: each row is a way a headless command could end up dialled
		// into the wrong daemon — or into nothing when something existed.
		why string
	}{
		{
			name:      "default running",
			sessions:  []Session{defStopped, defRunning, mainRunning},
			wantSock:  "/d/herdr.sock",
			wantFound: true,
			why:       "the default session wins even when others run; a headless command must land in the session the user configured as default",
		},
		{
			name:      "default stopped, one other running",
			sessions:  []Session{defStopped, mainRunning},
			wantSock:  "/s/main/herdr.sock",
			wantFound: true,
			why:       "default not started today is the common one-machine case; refusing here would make the headless CLI useless whenever the user just runs a named session",
		},
		{
			name:      "several running, none default",
			sessions:  []Session{mainRunning, altRunning},
			wantFound: false,
			why:       "picking arbitrarily would silently attach a headless command to the wrong session; not-found forces the caller to ask the user instead of guessing",
		},
		{
			name:      "no sessions at all",
			sessions:  nil,
			wantFound: false,
			why:       "an empty list must report not-found, not error: the caller distinguishes 'nothing to dial' from 'could not enumerate'",
		},
		{
			name:      "default stopped, nothing else running",
			sessions:  []Session{defStopped},
			wantFound: false,
			why:       "the default session's socket is unlinked when stopped, so there is genuinely nothing to dial — reporting found would send the caller into an ENOENT dial",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sock, ok := DefaultRunningSocket(tc.sessions)
			if ok != tc.wantFound {
				t.Fatalf("DefaultRunningSocket(%s): found=%v, want %v — %s", tc.name, ok, tc.wantFound, tc.why)
			}
			if sock != tc.wantSock {
				t.Errorf("DefaultRunningSocket(%s): socket=%q, want %q — %s", tc.name, sock, tc.wantSock, tc.why)
			}
		})
	}
}
