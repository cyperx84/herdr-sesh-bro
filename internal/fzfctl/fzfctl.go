// Package fzfctl talks to a running fzf's --listen server.
//
// fzf 0.66 can serve an HTTP endpoint on a unix socket and apply any action
// string posted to it — `reload(...)`, `change-header(...)`, `refresh-preview`
// — to the live UI. That is what lets sesh-bro update an OPEN picker the
// instant herdr reports a state change, instead of polling on a timer or
// making the user press a key. The alternative designs both cost something
// this one does not: a timer burns CPU to discover that nothing happened, and
// a keypress is the friction the whole feature exists to remove.
package fzfctl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Client posts actions to one fzf listen socket.
type Client struct {
	socket string
	http   *http.Client
}

// New returns a Client for the fzf serving socket.
//
// Keep-alives are disabled deliberately. fzf's server is a minimal
// implementation rather than a general HTTP server, and a pooled connection
// held open against a process the user can kill at any moment buys nothing:
// these requests are rare, tiny, and always local.
func New(socket string) *Client {
	return &Client{
		socket: socket,
		http: &http.Client{
			Timeout: 2 * time.Second,
			Transport: &http.Transport{
				DisableKeepAlives: true,
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", socket)
				},
			},
		},
	}
}

// Post applies one action string to the running picker.
//
// A closed socket is the normal end of a picker's life — the user pressed
// escape while an update was in flight — so callers treat any error here as
// "the picker is gone", not as a fault to report.
func (c *Client) Post(ctx context.Context, actions string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://fzf/", strings.NewReader(actions))
	if err != nil {
		return fmt.Errorf("fzfctl: build request: %w", err)
	}
	req.ContentLength = int64(len(actions))
	req.Header.Set("Content-Type", "text/plain")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("fzfctl: post to %s: %w", c.socket, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("fzfctl: fzf returned %s: %s", resp.Status, bytes.TrimSpace(body))
	}
	return nil
}

// Reload replaces the picker's list with the contents of a rows file and
// refreshes the preview so it describes whatever the cursor ended up on.
//
// `cat` rather than sending the rows in the request body because fzf's reload
// takes a COMMAND, not data; the file is already written, and passing a path
// keeps the pushed payload a constant handful of bytes no matter how many
// agents exist.
//
// first controls what happens when the row the cursor was tracking is gone
// from the new list. --track blocks the UI until it finds the tracked item, so
// a vanished target would leave the prompt dimmed until the stream ended;
// appending `first` moves the cursor to the top instead, which is both
// responsive and the right place to be when the thing you were looking at
// stopped existing.
func (c *Client) Reload(ctx context.Context, rowsFile string, first bool) error {
	action := fmt.Sprintf("reload(cat %s)+refresh-preview", shellQuote(rowsFile))
	if first {
		action += "+first"
	}
	return c.Post(ctx, action)
}

// shellQuote wraps a path for the shell fzf runs reload commands through,
// using the same POSIX single-quote escape the picker package applies to its
// own argv strings.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// State is the slice of fzf's reported state this package needs.
type State struct {
	Current struct {
		Text string `json:"text"`
	} `json:"current"`
	MatchCount int `json:"matchCount"`
	TotalCount int `json:"totalCount"`
}

// CurrentTarget returns the TSV target field (field 2) of the row the cursor
// is on, or "" when there is no cursor or the line has no fields.
//
// Field 2 rather than the whole line because that is the identity --id-nth
// tracks, and the display field carries volatile text — a status badge ticking
// from 9m to 10m would otherwise look like a different row.
func (s State) CurrentTarget() string {
	parts := strings.Split(s.Current.Text, "\t")
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

// Query asks a running fzf for its current state.
func (c *Client) Query(ctx context.Context) (State, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://fzf/?limit=1", nil)
	if err != nil {
		return State{}, fmt.Errorf("fzfctl: build request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return State{}, fmt.Errorf("fzfctl: query %s: %w", c.socket, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return State{}, fmt.Errorf("fzfctl: read state: %w", err)
	}
	var st State
	if err := json.Unmarshal(body, &st); err != nil {
		return State{}, fmt.Errorf("fzfctl: decode state: %w", err)
	}
	return st, nil
}
