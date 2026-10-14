package main

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/eduardosasso/hh/internal/client"
	"github.com/eduardosasso/hh/internal/listen"
	"github.com/eduardosasso/hh/internal/port"
	"github.com/eduardosasso/hh/internal/route"
	"github.com/eduardosasso/hh/internal/usual"
)

const (
	// pollEvery is how often the wrapper looks for the port the app listens on.
	pollEvery = 250 * time.Millisecond
	// treeEvery is how often the child's process tree is refreshed once the port is known.
	treeEvery = time.Second
	// earlyExit is how soon a failed exit counts as a failed start.
	earlyExit = 15 * time.Second

	exitedHint = "hh: the app exited before it listened. Run hh who to see which ports are in use."
)

// watcher follows the port the app listens on, moves the route there, and
// keeps the child's process tree for the orphan sweep.
type watcher struct {
	register func(route.Route) error
	remember func(host string, port int) error
	forget   func(host string) error
	out      io.Writer
	r        route.Route
	url      string
	// usual is the host's usual port from ports.json, or 0.
	usual int
	// routed is the port the daemon sends the host to.
	routed int
	// found is the chosen listening port, or 0 before the app listens.
	found int
	// seen lists every port the tree listened on, in order of first sight.
	seen   []int
	family []listen.Process
}

func newWatcher(c *client.Client, r route.Route, url string) *watcher {
	u, err := usual.Port(r.Host)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hh: read usual port:", err)
	}

	return &watcher{
		register: c.Register, remember: usual.Remember, forget: usual.Forget, out: os.Stdout,
		r: r, url: url, usual: u, routed: r.Port,
	}
}

// watch polls the child's process tree until done closes: fast until the app
// listens, then slower, so a port that opens later still wins.
func (w *watcher) watch(pid int, done <-chan struct{}) {
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	discover := true
	for {
		select {
		case <-done:
			return
		case <-tick.C:
		}
		tree, err := listen.Tree(pid)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hh: process tree:", err)

			return
		}
		w.family = tree
		if !discover {
			continue
		}
		ss, err := listen.Listening(pids(pid, tree))
		if err != nil {
			fmt.Fprintln(os.Stderr, "hh: port discovery stopped:", err)
			discover = false
			tick.Reset(treeEvery)

			continue
		}
		before := w.found
		w.see(open(ss, w.r.Paths))
		if before == 0 && w.found != 0 {
			tick.Reset(treeEvery)
		}
	}
}

func pids(root int, tree []listen.Process) []int {
	out := []int{root}
	for _, p := range tree {
		out = append(out, p.PID)
	}

	return out
}

// open lists the listening ports, lowest first, without --route ports.
func open(ss []listen.Socket, paths []route.Path) []int {
	var ps []int
	for _, s := range ss {
		routed := slices.ContainsFunc(paths, func(p route.Path) bool { return p.Port == s.Port })
		if !routed && !slices.Contains(ps, s.Port) {
			ps = append(ps, s.Port)
		}
	}
	slices.Sort(ps)

	return ps
}

// see takes one snapshot of listening ports and moves the route when the choice changes.
func (w *watcher) see(ports []int) {
	for _, p := range ports {
		if !slices.Contains(w.seen, p) {
			w.seen = append(w.seen, p)
		}
	}
	p := choose(ports, w.seen, w.r.Port, w.usual, w.found)
	if p == 0 || p == w.found {
		return
	}
	w.found = p
	w.settle(p)
}

// choose prefers PORT, then the usual port, then the current choice so the
// route does not flap, then the first port seen. Only listening ports count.
func choose(listening, seen []int, assigned, usual, current int) int {
	for _, p := range append([]int{assigned, usual, current}, seen...) {
		if p != 0 && slices.Contains(listening, p) {
			return p
		}
	}

	return 0
}

// settle routes the host to p and updates its usual port.
func (w *watcher) settle(p int) {
	if p != w.routed {
		moved := w.r
		moved.Port = p
		if err := w.register(moved); err != nil {
			fmt.Fprintf(os.Stderr, "hh: move %s to port %d: %v\n", w.r.Host, p, err)
		} else {
			w.routed = p
			fmt.Fprintln(w.out, movedLine(w.url, p, w.r.Port))
		}
	}
	if err := w.keep(p); err != nil {
		fmt.Fprintln(os.Stderr, "hh:", err)
	}
}

// keep remembers p as the usual port, or forgets it once the app listens on PORT.
func (w *watcher) keep(p int) error {
	if p == w.r.Port {
		return w.forget(w.r.Host)
	}

	return w.remember(w.r.Host, p)
}

func movedLine(url string, found, assigned int) string {
	if found == assigned {
		return fmt.Sprintf("→ %s (app now listens on PORT=%d)", url, assigned)
	}

	return fmt.Sprintf("→ %s (app listens on %d, ignoring PORT=%d)", url, found, assigned)
}

// warnUsual tells, before the app starts, who holds its usual port.
func warnUsual(c *client.Client, r route.Route) {
	msg, _, err := explain(c, r)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hh: check usual port:", err)

		return
	}
	if msg != "" {
		fmt.Fprintln(os.Stderr, msg)
	}
}

// failed explains an app that exited non-zero before it listened.
func failed(c *client.Client, r route.Route) {
	msg, known, err := explain(c, r)
	switch {
	case err != nil:
		fmt.Fprintln(os.Stderr, "hh: check usual port:", err)
	case msg != "":
		fmt.Fprintln(os.Stderr, msg)
	case !known:
		fmt.Fprintln(os.Stderr, exitedHint)
	}
}

// explain says who holds the usual port of r.Host. known is false when no usual port is stored.
func explain(c *client.Client, r route.Route) (msg string, known bool, err error) {
	u, err := usual.Port(r.Host)
	if err != nil || u == 0 {
		return "", false, err
	}
	all, err := listen.All()
	if err != nil {
		return "", true, err
	}
	held := slices.DeleteFunc(all, func(s listen.Socket) bool { return s.Port != u })
	if len(held) == 0 && port.Free(u) {
		return "", true, nil
	}
	routes, err := c.Routes()
	if err != nil {
		return "", true, err
	}
	own, err := owners(routes, listen.Descendants)
	if err != nil {
		return "", true, err
	}
	user, err := os.UserHomeDir()
	if err != nil {
		return "", true, fmt.Errorf("home dir: %w", err)
	}

	return busy(r.Host, u, r.Port, holder(held, own, listen.Cwd, user)), true, nil
}

func busy(host string, usual, free int, by string) string {
	return fmt.Sprintf("hh: %s usually listens on %d, but %d is held by %s\n"+
		"hh: free port for this run: %d. Read $PORT in your dev script to use it.", host, usual, usual, by, free)
}

// holder names the process on a port, and the hh route it runs under if any.
func holder(held []listen.Socket, own map[int]route.Route, cwd func(int) (string, error), user string) string {
	if len(held) == 0 {
		return "another process"
	}
	s := held[0]
	if o, ok := own[s.PID]; ok {
		return fmt.Sprintf("%s (pid %d) running %s from %s", s.Command, s.PID, o.Host, tilde(o.Cwd, user))
	}
	dir, err := cwd(s.PID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hh: cwd of pid %d: %v\n", s.PID, err)

		return fmt.Sprintf("%s (pid %d)", s.Command, s.PID)
	}
	s.Cwd = dir

	return fmt.Sprintf("%s (pid %d) in %s", s.Command, s.PID, tilde(s.Cwd, user))
}

// owners maps every pid in a route's wrapper tree to that route.
func owners(routes []route.Route, tree func(int) ([]int, error)) (map[int]route.Route, error) {
	own := map[int]route.Route{}
	for _, r := range routes {
		kids, err := tree(r.PID)
		if err != nil {
			return nil, fmt.Errorf("process tree of %s: %w", r.Host, err)
		}
		for _, pid := range append([]int{r.PID}, kids...) {
			own[pid] = r
		}
	}

	return own, nil
}

// tilde shows paths under the home folder as ~.
func tilde(path, user string) string {
	if path == user {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, user+"/"); ok {
		return "~/" + rest
	}

	return path
}
