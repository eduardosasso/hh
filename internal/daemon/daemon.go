// Package daemon runs the embedded Caddy proxy and the hh control socket.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	_ "github.com/caddyserver/caddy/v2/modules/standard"
	"golang.org/x/term"

	"github.com/eduardosasso/hh/internal/client"
	"github.com/eduardosasso/hh/internal/home"
	"github.com/eduardosasso/hh/internal/name"
	"github.com/eduardosasso/hh/internal/route"
)

const (
	// Sweep is how often routes with a dead pid are dropped.
	Sweep = 5 * time.Second

	socketMode = 0o600
	maxBody    = 1 << 16
	shutdown   = 5 * time.Second
)

type daemon struct {
	mu       sync.Mutex
	routes   table
	state    string
	settings Settings
	log      *slog.Logger
}

// Run serves until ctx is done.
func Run(ctx context.Context) error {
	d, closeLog, err := setup()
	if err != nil {
		return err
	}
	defer closeLog()

	if err := d.run(ctx); err != nil {
		d.log.Error("daemon stopped", "err", err)

		return err
	}
	d.log.Info("daemon stopped")

	return nil
}

func setup() (*daemon, func(), error) {
	f, err := home.OpenLog()
	if err != nil {
		return nil, nil, err
	}
	var w io.Writer = f
	if term.IsTerminal(int(os.Stderr.Fd())) {
		w = io.MultiWriter(f, os.Stderr)
	}
	closeLog := func() {
		if err := f.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "hh: close log:", err)
		}
	}
	d := &daemon{log: slog.New(slog.NewTextHandler(w, nil)).With("component", "hh")}
	d.settings.Log = f.Name()
	if d.settings.Access, err = home.Access(); err != nil {
		return nil, closeLog, err
	}
	if d.settings.Storage, err = home.Storage(); err != nil {
		return nil, closeLog, err
	}
	if d.settings.HTTPS, err = home.HTTPS(); err != nil {
		return nil, closeLog, err
	}
	if d.settings.HTTP, err = home.HTTP(); err != nil {
		return nil, closeLog, err
	}
	if d.state, err = home.State(); err != nil {
		return nil, closeLog, err
	}

	return d, closeLog, nil
}

func (d *daemon) run(ctx context.Context) error {
	ln, err := d.listen()
	if err != nil {
		return err
	}
	if d.routes, err = load(d.state); err != nil {
		return errors.Join(err, ln.Close())
	}
	d.mu.Lock()
	d.drop(d.routes.dead(), "pid gone at startup")
	err = d.apply()
	d.mu.Unlock()
	if err != nil {
		return errors.Join(err, ln.Close())
	}
	defer d.stopCaddy()

	srv := &http.Server{Handler: d.mux(), ReadHeaderTimeout: shutdown}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	d.log.Info("daemon started", "pid", os.Getpid(), "https", d.settings.HTTPS, "http", d.settings.HTTP, "routes", len(d.routes))

	tick := time.NewTicker(Sweep)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			sctx, cancel := context.WithTimeout(context.Background(), shutdown)
			defer cancel()

			return srv.Shutdown(sctx)
		case err := <-served:
			return fmt.Errorf("control socket: %w", err)
		case <-tick.C:
			d.sweep()
		}
	}
}

func (d *daemon) stopCaddy() {
	if err := caddy.Stop(); err != nil {
		d.log.Error("stop caddy", "err", err)
	}
}

// listen claims the socket, refusing if another daemon answers on it.
func (d *daemon) listen() (net.Listener, error) {
	sock, err := home.Socket()
	if err != nil {
		return nil, err
	}
	c, err := client.New()
	if err != nil {
		return nil, err
	}
	h, err := c.Health()
	if err == nil {
		return nil, fmt.Errorf("daemon already running (pid %d)", h.PID)
	}
	if !errors.Is(err, client.ErrDown) {
		return nil, fmt.Errorf("probe existing daemon: %w", err)
	}
	if err := os.Remove(sock); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("remove stale socket: %w", err)
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", sock, err)
	}
	if err := os.Chmod(sock, socketMode); err != nil {
		return nil, errors.Join(fmt.Errorf("chmod socket: %w", err), ln.Close())
	}

	return ln, nil
}

// apply loads the current table into Caddy. Callers hold d.mu.
func (d *daemon) apply() error {
	cfg, err := Config(d.routes.list(), d.settings)
	if err != nil {
		return err
	}
	if err := caddy.Load(cfg, false); err != nil {
		return fmt.Errorf("load caddy config: %w", err)
	}

	return nil
}

// commit persists and applies the table, restoring prev on failure. Callers hold d.mu.
func (d *daemon) commit(prev table) error {
	err := d.apply()
	if err == nil {
		err = d.routes.save(d.state)
	}
	if err == nil {
		return nil
	}
	d.routes = prev
	if rerr := d.apply(); rerr != nil {
		d.log.Error("restore previous config", "err", rerr)
	}

	return err
}

// drop removes hosts without committing. Callers hold d.mu.
func (d *daemon) drop(hosts []string, why string) {
	for _, h := range hosts {
		d.log.Info("route removed", "host", h, "pid", d.routes[h].PID, "reason", why)
		delete(d.routes, h)
	}
}

func (d *daemon) sweep() {
	d.mu.Lock()
	defer d.mu.Unlock()
	gone := d.routes.dead()
	if len(gone) == 0 {
		return
	}
	prev := maps.Clone(d.routes)
	d.drop(gone, "pid gone")
	if err := d.commit(prev); err != nil {
		d.log.Error("sweep", "err", err)
	}
}

func (d *daemon) mux() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		d.reply(w, http.StatusOK, client.Health{PID: os.Getpid()})
	})
	m.HandleFunc("GET /routes", func(w http.ResponseWriter, _ *http.Request) {
		d.mu.Lock()
		rs := d.routes.list()
		d.mu.Unlock()
		d.reply(w, http.StatusOK, rs)
	})
	m.HandleFunc("POST /routes", d.register)
	m.HandleFunc("DELETE /routes/{host}", d.deregister)

	return m
}

func (d *daemon) register(w http.ResponseWriter, req *http.Request) {
	var r route.Route
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, maxBody)).Decode(&r); err != nil {
		d.fail(w, http.StatusBadRequest, fmt.Errorf("decode route: %w", err))

		return
	}
	if err := validate(r); err != nil {
		d.fail(w, http.StatusBadRequest, err)

		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	cur, ok := d.routes[r.Host]
	if ok && cur.PID != r.PID && alive(cur.PID) {
		d.log.Info("route refused", "host", r.Host, "pid", r.PID, "owner", cur.PID)
		d.reply(w, http.StatusConflict, conflict(cur, r))

		return
	}
	// The same wrapper registers again to move its route to the port the app listens on.
	update := ok && cur.PID == r.PID
	prev := maps.Clone(d.routes)
	r.Started = time.Now()
	if update {
		r.Started = cur.Started
	}
	d.routes[r.Host] = r
	if err := d.commit(prev); err != nil {
		d.fail(w, http.StatusInternalServerError, err)

		return
	}
	msg := "route added"
	if update {
		msg = "route updated"
	}
	d.log.Info(msg, "host", r.Host, "port", r.Port, "pid", r.PID, "cwd", r.Cwd)
	d.reply(w, http.StatusOK, r)
}

func conflict(cur, r route.Route) route.Conflict {
	c := route.Conflict{Host: cur.Host, PID: cur.PID, Cwd: cur.Cwd}
	if cur.Cwd == r.Cwd {
		c.Error = fmt.Sprintf("%s is already running here (pid %d)", cur.Host, cur.PID)
	} else {
		c.Error = fmt.Sprintf("%s is already used by %s (pid %d); use --name to pick another name", cur.Host, cur.Cwd, cur.PID)
	}

	return c
}

func validate(r route.Route) error {
	if !strings.HasSuffix(r.Host, "."+name.TLD) {
		return fmt.Errorf("host %q must end in .%s", r.Host, name.TLD)
	}
	if r.PID <= 0 {
		return fmt.Errorf("invalid pid %d", r.PID)
	}
	for _, p := range append([]int{r.Port}, ports(r.Paths)...) {
		if p <= 0 || p > 65535 {
			return fmt.Errorf("invalid port %d", p)
		}
	}

	return nil
}

func ports(ps []route.Path) []int {
	out := make([]int, len(ps))
	for i, p := range ps {
		out[i] = p.Port
	}

	return out
}

func (d *daemon) deregister(w http.ResponseWriter, req *http.Request) {
	host := req.PathValue("host")
	pid, err := strconv.Atoi(req.URL.Query().Get("pid"))
	if err != nil {
		d.fail(w, http.StatusBadRequest, fmt.Errorf("pid: %w", err))

		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	cur, ok := d.routes[host]
	if !ok || cur.PID != pid {
		d.log.Info("deregister skipped", "host", host, "pid", pid, "reason", "not owner or already gone")
		w.WriteHeader(http.StatusNoContent)

		return
	}
	prev := maps.Clone(d.routes)
	d.drop([]string{host}, "wrapper exited")
	if err := d.commit(prev); err != nil {
		d.fail(w, http.StatusInternalServerError, err)

		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *daemon) reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		d.log.Error("write reply", "err", err)
	}
}

func (d *daemon) fail(w http.ResponseWriter, status int, err error) {
	d.log.Error("request failed", "status", status, "err", err)
	http.Error(w, err.Error(), status)
}
