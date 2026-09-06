// Package herdrtest is a tests-only fake herdr daemon: a unix-socket server
// speaking the exact wire protocol github.com/cyperx84/herdr-api's client
// speaks, so command-level tests can drive real RPC paths without a live
// daemon.
//
// The repo never had this. The old bash harness's tests/mock-herdr faked the
// herdr CLI, which cannot drive the Go binary — it dials the socket directly
// — so that harness was deleted (0.4.0) and this replaces it. Protocol
// fidelity is not improvised here: herdr-api's client.go (writeRequest,
// newScanner, Call, Subscribe, normalizeEventKind) is the source of truth,
// and this server is built to satisfy that client, not a paraphrase of its
// docs. The strongest pin on that fidelity is that herdrtest's own tests
// drive the server with herdr-api's REAL client, not a hand-rolled one.
//
// Load-bearing protocol facts (all from client.go, established by experiment
// against herdr 0.8.0 upstream):
//
//   - Newline-delimited JSON; requests {"id","method","params"} where params
//     is never omitted (writeRequest substitutes {} for nil).
//   - Ordinary methods are ONE REQUEST PER CONNECTION: the client dials
//     fresh per Call, so the server writes exactly one response —
//     {"id":…,"result":…} or {"id":…,"error":{"code","message"}} — then
//     closes.
//   - events.subscribe is the exception: it acks (id-matched response) and
//     then HOLDS the connection open, streaming {"event":"…","data":…}
//     frames (no id) until the client disconnects.
//   - The client's scanner tolerates lines up to 16 MiB (session.snapshot is
//     ~18 KB and grows), so the server reads with the same headroom.
package herdrtest

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"testing"

	herdr "github.com/cyperx84/herdr-api"
)

// Handler answers one request. params is the raw request params object; the
// returned value is marshalled into the response's "result" field. A
// non-nil error becomes an error frame {"code","message"} — use APIError to
// control the code, or any error to get code "test_error".
type Handler func(params json.RawMessage) (any, error)

// APIError is a Handler's way of naming a specific error code, matching
// herdr-api's client.APIError shape (that client branches on Code).
type APIError struct {
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Call is one recorded request for a method, as Calls exposes it.
type Call struct {
	Params json.RawMessage
}

// request is the wire shape writeRequest produces; params is RawMessage so
// handlers see exactly what the client sent.
type request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// frame is the union the server writes: exactly one of a response (id +
// result/error) or an event (event + data). Events carry no id — that is
// what distinguishes them from responses in the client's reader.
type frame struct {
	ID     string          `json:"id,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *errBody        `json:"error,omitempty"`
	Event  string          `json:"event,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// errBody is herdr-api's APIError wire shape.
type errBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Server is the fake daemon. Construct with Start, which wires cleanup.
type Server struct {
	path string

	mu        sync.Mutex
	handlers  map[string]Handler
	calls     map[string][]Call
	subs      []herdr.Subscription
	subConns  map[net.Conn]struct{}
	listener  net.Listener
	closeOnce sync.Once
}

// Start launches the fake server and registers t.Cleanup to shut it down.
//
// The socket lives under os.MkdirTemp("", "hx") — deliberately NOT
// t.TempDir(): macOS caps unix socket paths at 104 bytes (sun_path), and
// t.TempDir()'s paths ($TMPDIR + TestXXX/random…) are long enough to blow
// that cap and make every dial fail with "invalid argument" in a way that
// looks like a server bug. "hx" keeps the prefix short; the dir is removed
// by cleanup.
func Start(t testing.TB) *Server {
	t.Helper()
	dir, err := os.MkdirTemp("", "hx")
	if err != nil {
		t.Fatalf("herdrtest: temp dir: %v", err)
	}
	s := &Server{
		path:     dir + "/herdr.sock",
		handlers: map[string]Handler{},
		calls:    map[string][]Call{},
		subConns: map[net.Conn]struct{}{},
	}
	ln, err := net.Listen("unix", s.path)
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("herdrtest: listen: %v", err)
	}
	s.listener = ln
	go s.accept()

	t.Cleanup(func() { s.Close() })
	return s
}

// Path is the socket path to hand a herdr-api client (herdr.New(s.Path())),
// or to inject as HERDR_SOCKET_PATH through the command layer's getenv seam.
func (s *Server) Path() string { return s.path }

// Handle registers fn as the answer to method. Registering the same method
// twice replaces the first handler — a test that wants call-count semantics
// wraps its own counter rather than the server guessing.
func (s *Server) Handle(method string, fn Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = fn
}

// Calls returns the requests recorded for method, oldest first, so a test
// can assert the params a command actually sent. Only presence and order are
// the server's to guarantee; params are the raw bytes the client wrote.
func (s *Server) Calls(method string) []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Call, len(s.calls[method]))
	copy(out, s.calls[method])
	return out
}

// Subscriptions returns every subscription every events.subscribe request
// asked for, in arrival order — for asserting what a command subscribed to.
func (s *Server) Subscriptions() []herdr.Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]herdr.Subscription, len(s.subs))
	copy(out, s.subs)
	return out
}

// Emit pushes one {"event","data"} frame to every open subscription
// connection. The kind spelling is passed through verbatim — herdr-api's
// client normalizes dotted spellings itself (normalizeEventKind), so a test
// can exercise either spelling and assert the spelling the CONSUMER sees.
// Blocking writes are avoided by dropping-when-full rather than inventing
// flow control a test never needs (and which the real server lacks: its
// reader drops rather than backpressures — client.go's Stream.read notes
// the same about the client side).
func (s *Server) Emit(kind string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		// Marshal of a test-controlled value; a failure is a test bug, and
		// panicking surfaces it at the Emit call site instead of as a
		// mysterious empty event on the client side.
		panic(fmt.Sprintf("herdrtest: Emit: marshal data: %v", err))
	}
	f := frame{Event: kind, Data: payload}
	s.mu.Lock()
	conns := make([]net.Conn, 0, len(s.subConns))
	for c := range s.subConns {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	for _, c := range conns {
		b, _ := json.Marshal(f)
		if _, err := c.Write(append(b, '\n')); err != nil {
			// Dead subscriber: drop it from the set on the next Emit's
			// sweep rather than distinguishing errors here — a test that
			// closed its stream expects silence, not a server error.
			s.mu.Lock()
			delete(s.subConns, c)
			s.mu.Unlock()
		}
	}
}

// Close stops the listener and closes every subscription connection.
// Safe to call more than once (Start's cleanup may race a test's own Close).
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		s.listener.Close()
		s.mu.Lock()
		for c := range s.subConns {
			c.Close()
		}
		s.mu.Unlock()
		os.RemoveAll(s.pathDir())
	})
}

func (s *Server) pathDir() string {
	// The socket file itself is removed by the listener close on most
	// platforms; the containing dir is this package's own mess to clean.
	return s.path[:len(s.path)-len("/herdr.sock")]
}

func (s *Server) accept() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.serve(conn)
	}
}

// serve answers one connection. For every method except events.subscribe
// this is exactly one request → one response → close, because herdr-api's
// Call dials a fresh connection per request and would fail a second
// response with a broken pipe. events.subscribe instead acks and keeps the
// connection open for Emit's frames (client.go's Subscribe doc) — so it
// takes ownership of the connection (and its closing) away from this
// function's defer; the conn lives on in subConns until Close.
func (s *Server) serve(conn net.Conn) {
	// 16 MiB line cap, mirroring herdr-api's newScanner: session.snapshot
	// is already ~18 KB and grows with the session, and a fixture that
	// crosses the default 64 KB would otherwise fail only against big
	// tests.
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	if !sc.Scan() {
		conn.Close()
		return
	}
	var req request
	if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
		// The client only ever sends valid JSON; garbage here is a test
		// bug, and a loud error frame beats silent nothing.
		s.writeFrame(conn, frame{ID: req.ID, Error: &errBody{Code: "bad_request", Message: err.Error()}})
		conn.Close()
		return
	}

	s.mu.Lock()
	s.calls[req.Method] = append(s.calls[req.Method], Call{Params: req.Params})
	handler, ok := s.handlers[req.Method]
	s.mu.Unlock()

	if req.Method == "events.subscribe" {
		s.serveSubscribe(conn, req)
		return // conn ownership transferred: serveSubscribe/subConns close it
	}

	defer conn.Close()

	if !ok {
		// Unhandled method: fail LOUDLY, never succeed silently — the whole
		// point of this server is that a test exercises real call paths, so
		// a missing handler means the test mis-models the wire traffic and
		// should fail with the method name in the message, not pass green
		// on an empty result.
		s.writeFrame(conn, frame{ID: req.ID, Error: &errBody{
			Code:    "test_unhandled",
			Message: fmt.Sprintf("herdrtest: no handler registered for %q", req.Method),
		}})
		return
	}
	result, err := handler(req.Params)
	if err != nil {
		// Structured errors keep their code and pass their message verbatim
		// (the wire frame is {"code","message"} — herdr-api's APIError the
		// caller branches on); anything else is a test bug and gets the
		// generic test_error code, still with the message as written.
		code := "test_error"
		message := err.Error()
		var api *APIError
		if errors.As(err, &api) {
			code = api.Code
			message = api.Message
		}
		s.writeFrame(conn, frame{ID: req.ID, Error: &errBody{Code: code, Message: message}})
		return
	}
	payload, mErr := json.Marshal(result)
	if mErr != nil {
		s.writeFrame(conn, frame{ID: req.ID, Error: &errBody{Code: "test_error", Message: mErr.Error()}})
		return
	}
	s.writeFrame(conn, frame{ID: req.ID, Result: payload})
}

// serveSubscribe acks an events.subscribe request (id-matched response —
// client.go's Subscribe consumes the ack before streaming) and registers
// the connection for Emit.
func (s *Server) serveSubscribe(conn net.Conn, req request) {
	var params struct {
		Subscriptions []herdr.Subscription `json:"subscriptions"`
	}
	_ = json.Unmarshal(req.Params, &params)

	s.mu.Lock()
	s.subs = append(s.subs, params.Subscriptions...)
	s.subConns[conn] = struct{}{}
	s.mu.Unlock()

	// The ack shape mirrors what client.go documents from the real server
	// (a {"type":"subscription_started"} result); Subscribe only checks the
	// id and error fields, so the payload is conventional.
	s.writeFrame(conn, frame{ID: req.ID, Result: json.RawMessage(`{"type":"subscription_started"}`)})
	// Hold the connection open: the goroutine that got here owns nothing
	// further to read (the real client never sends a second request on a
	// subscribe connection), so serve returns and the conn lives on in
	// subConns, receiving Emit's frames, until either side closes it.
}

func (s *Server) writeFrame(conn net.Conn, f frame) {
	// json.Encoder appends the newline the protocol needs — same primitive
	// herdr-api's writeRequest uses on the client side.
	_ = json.NewEncoder(conn).Encode(f)
}
