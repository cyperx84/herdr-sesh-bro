package fzfctl

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// serveFake stands in for fzf's --listen server and records what was posted.
func serveFake(t *testing.T, status int) (socket string, got *[]string) {
	t.Helper()
	// Not t.TempDir(): macOS caps a unix socket path at 104 bytes and Go's
	// per-test temp paths are long enough to exceed it.
	dir, err := os.MkdirTemp("", "fz")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	socket = filepath.Join(dir, "f.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	posts := []string{}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		posts = append(posts, string(buf))
		w.WriteHeader(status)
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return socket, &posts
}

func TestReloadPostsCatAndRefresh(t *testing.T) {
	socket, posts := serveFake(t, http.StatusOK)
	if err := New(socket).Reload(context.Background(), "/tmp/rows dir/all.tsv", false); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if len(*posts) != 1 {
		t.Fatalf("posts = %d, want 1", len(*posts))
	}
	got := (*posts)[0]
	// The path has a space in it, so unquoted it would become two arguments
	// and reload would cat the wrong thing.
	if got != `reload(cat '/tmp/rows dir/all.tsv')+refresh-preview` {
		t.Errorf("posted %q", got)
	}
}

// When the tracked row is gone, the cursor must be sent to the top: --track
// otherwise blocks the UI waiting for an item that will never arrive.
func TestReloadFirstAppendsFirstAction(t *testing.T) {
	socket, posts := serveFake(t, http.StatusOK)
	if err := New(socket).Reload(context.Background(), "/tmp/all.tsv", true); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if !strings.HasSuffix((*posts)[0], "+first") {
		t.Errorf("posted %q, want a trailing +first", (*posts)[0])
	}
}

// A picker the user has already closed is the normal end of life, and the
// caller distinguishes it only by "this returned an error".
func TestPostToMissingSocketErrors(t *testing.T) {
	if err := New("/nonexistent/nope.sock").Post(context.Background(), "reload(true)"); err == nil {
		t.Error("Post to a dead socket returned nil, want an error")
	}
}

func TestPostSurfacesNon2xx(t *testing.T) {
	socket, _ := serveFake(t, http.StatusBadRequest)
	err := New(socket).Post(context.Background(), "bogus(")
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Errorf("err = %v, want one naming the 400", err)
	}
}

// The cursor is tracked by the row's target field, not by the whole line: the
// display field carries a status age that ticks, and a row whose badge went
// from 9m to 10m is the same row.
func TestStateCurrentTarget(t *testing.T) {
	for name, tc := range map[string]struct{ text, want string }{
		"normal row":   {"agent\tw1:p2\t● claude · thing · 9m", "w1:p2"},
		"header row":   {"header\t-\t● 2 blocked", "-"},
		"no fields":    {"garbage", ""},
		"empty cursor": {"", ""},
	} {
		var s State
		s.Current.Text = tc.text
		if got := s.CurrentTarget(); got != tc.want {
			t.Errorf("%s: CurrentTarget() = %q, want %q", name, got, tc.want)
		}
	}
}

func TestQueryDecodesState(t *testing.T) {
	dir, err := os.MkdirTemp("", "fq")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "f.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"matchCount":3,"totalCount":9,"current":{"text":"agent\tw1:p2\trow"}}`))
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	st, err := New(socket).Query(context.Background())
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if st.MatchCount != 3 || st.TotalCount != 9 || st.CurrentTarget() != "w1:p2" {
		t.Errorf("decoded %+v, target %q", st, st.CurrentTarget())
	}
}

// A picker the user already closed is the normal case, and must be an ordinary
// error the caller can shrug at rather than anything louder.
func TestQueryOnDeadSocketErrors(t *testing.T) {
	if _, err := New("/nonexistent/nope.sock").Query(context.Background()); err == nil {
		t.Error("Query on a dead socket returned nil error")
	}
}
