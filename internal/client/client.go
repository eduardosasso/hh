// Package client talks to the hh daemon over its unix socket.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/eduardosasso/hh/internal/home"
	"github.com/eduardosasso/hh/internal/route"
)

// base is a placeholder origin; the transport always dials the socket.
const (
	base    = "http://hh"
	timeout = 5 * time.Second
)

// ErrDown means no daemon answers on the socket.
var ErrDown = errors.New("daemon not running")

// ConflictError is returned when another wrapper owns the host.
type ConflictError struct{ route.Conflict }

func (e *ConflictError) Error() string { return e.Conflict.Error }

// Health is the daemon status.
type Health struct {
	PID int `json:"pid"`
}

// Client is a daemon connection.
type Client struct{ http *http.Client }

// New returns a client bound to the socket in ~/.hh.
func New() (*Client, error) {
	sock, err := home.Socket()
	if err != nil {
		return nil, err
	}
	t := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer

			return d.DialContext(ctx, "unix", sock)
		},
	}

	return &Client{http: &http.Client{Transport: t, Timeout: timeout}}, nil
}

// Health checks the daemon. It returns ErrDown when nothing listens.
func (c *Client) Health() (Health, error) {
	var h Health

	return h, c.do(http.MethodGet, "/health", nil, &h)
}

// Routes lists live routes.
func (c *Client) Routes() ([]route.Route, error) {
	var rs []route.Route

	return rs, c.do(http.MethodGet, "/routes", nil, &rs)
}

// Register adds r. A taken host returns *ConflictError.
func (c *Client) Register(r route.Route) error {
	return c.do(http.MethodPost, "/routes", r, nil)
}

// Deregister removes host if pid still owns it.
func (c *Client) Deregister(host string, pid int) error {
	return c.do(http.MethodDelete, fmt.Sprintf("/routes/%s?pid=%d", url.PathEscape(host), pid), nil, nil)
}

func (c *Client) do(method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode %s %s: %w", method, path, err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, base+path, body)
	if err != nil {
		return fmt.Errorf("build %s %s: %w", method, path, err)
	}
	res, err := c.http.Do(req)
	if err != nil {
		var op *net.OpError
		if errors.As(err, &op) && op.Op == "dial" {
			return ErrDown
		}

		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("read %s %s: %w", method, path, err)
	}
	if res.StatusCode == http.StatusConflict {
		var cf route.Conflict
		if err := json.Unmarshal(raw, &cf); err != nil {
			return fmt.Errorf("decode conflict: %w", err)
		}

		return &ConflictError{cf}
	}
	if res.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("%s %s: %s: %s", method, path, res.Status, bytes.TrimSpace(raw))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode %s %s: %w", method, path, err)
	}

	return nil
}
