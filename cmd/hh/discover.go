package main

import (
	"fmt"
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

// watcher finds the port the app listens on, moves the route there, and
// keeps the child's process tree for the orphan sweep.
type watcher struct {
	c      *client.Client
	r      route.Route
	url    string
	found  int
	family []listen.Process
}

// watch polls the child's process tree until done closes.
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
		p, err := listening(pid, tree, w.r)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hh: port discovery stopped:", err)
		}
		if err != nil || p != 0 {
			discover = false
			tick.Reset(treeEvery)
		}
		if p != 0 {
			w.found = p
			w.settle(p)
		}
	}
}

func listening(pid int, tree []listen.Process, r route.Route) (int, error) {
	pids := []int{pid}
	for _, p := range tree {
		pids = append(pids, p.PID)
	}
	ss, err := listen.Listening(pids)
	if err != nil {
		return 0, err
	}

	return pick(ss, r), nil
}

// pick prefers PORT, skips --route ports, then takes the lowest port.
func pick(ss []listen.Socket, r route.Route) int {
	var ps []int
	for _, s := range ss {
		if s.Port == r.Port {
			return r.Port
		}
		if !slices.ContainsFunc(r.Paths, func(p route.Path) bool { return p.Port == s.Port }) {
			ps = append(ps, s.Port)
		}
	}
	if len(ps) == 0 {
		return 0
	}

	return slices.Min(ps)
}

// settle moves the route when the app ignores PORT and remembers its usual port.
func (w *watcher) settle(p int) {
	if p == w.r.Port {
		if err := usual.Forget(w.r.Host); err != nil {
			fmt.Fprintln(os.Stderr, "hh:", err)
		}

		return
	}
	moved := w.r
	moved.Port = p
	if err := w.c.Register(moved); err != nil {
		fmt.Fprintf(os.Stderr, "hh: move %s to port %d: %v\n", w.r.Host, p, err)
	} else {
		fmt.Println(movedLine(w.url, p, w.r.Port))
	}
	if err := usual.Remember(w.r.Host, p); err != nil {
		fmt.Fprintln(os.Stderr, "hh:", err)
	}
}

func movedLine(url string, found, assigned int) string {
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
