package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/eduardosasso/hh/internal/client"
	"github.com/eduardosasso/hh/internal/home"
	"github.com/eduardosasso/hh/internal/name"
	"github.com/eduardosasso/hh/internal/port"
	"github.com/eduardosasso/hh/internal/profile"
	"github.com/eduardosasso/hh/internal/route"
	"github.com/eduardosasso/hh/internal/trust"
)

// Shell exit codes for a command that cannot run.
const (
	notFound      = 127
	notExecutable = 126
	signalBase    = 128
)

type options struct {
	name  string
	port  int
	paths []route.Path
	label string
	cmd   []string
}

func parse(args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet("hh", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	fs.StringVar(&o.name, "name", "", "override the derived name")
	fs.IntVar(&o.port, "port", 0, "use a fixed port")
	fs.Func("route", "send path to another local port, as path=port", func(v string) error {
		p, err := pathRoute(v)
		o.paths = append(o.paths, p)

		return err
	})
	if err := fs.Parse(args); err != nil {
		return o, &exitError{code: 2}
	}
	rest := fs.Args()
	if len(rest) > 1 && rest[1] == "--" {
		o.label, rest = rest[0], rest[2:]
	}
	if len(rest) == 0 {
		return o, errors.New("no command given; see `hh help`")
	}
	o.cmd = rest

	return o, nil
}

func pathRoute(v string) (route.Path, error) {
	path, p, ok := strings.Cut(v, "=")
	if !ok || !strings.HasPrefix(path, "/") {
		return route.Path{}, fmt.Errorf("want /path=port, got %q", v)
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		return route.Path{}, fmt.Errorf("route port %q: %w", p, err)
	}

	return route.Path{Path: path, Port: n}, nil
}

func wrap(args []string) error {
	o, err := parse(args)
	if err != nil {
		return err
	}
	bin, err := lookup(o.cmd[0])
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cwd: %w", err)
	}
	host, err := name.Host(cwd, name.Options{Label: o.label, Override: o.name})
	if err != nil {
		return err
	}
	c, err := client.Ensure()
	if err != nil {
		return err
	}
	r, err := claim(c, route.Route{Host: host, Port: o.port, PID: os.Getpid(), Cwd: cwd, Paths: o.paths})
	if err != nil {
		return err
	}
	u, err := home.URL(host)
	if err != nil {
		return errors.Join(err, release(c, r))
	}
	fmt.Println("→ " + u)
	warnTrust()
	warnUsual(c, r)

	w := &watcher{c: c, r: r, url: u}
	start := time.Now()
	code, err := spawn(bin, o.cmd, r.Port, env(r, u), w.watch)
	reap(w.family)
	if err == nil && code != 0 && code <= signalBase && w.found == 0 && time.Since(start) < earlyExit {
		failed(c, r)
	}
	if rerr := release(c, r); rerr != nil {
		fmt.Fprintln(os.Stderr, "hh:", rerr)
	}
	if err != nil {
		return err
	}

	return exitCode(code)
}

// lookup mirrors the shell: 127 when missing, 126 when not executable.
func lookup(cmd string) (string, error) {
	bin, err := exec.LookPath(cmd)
	if err == nil {
		return bin, nil
	}
	if errors.Is(err, fs.ErrPermission) {
		return "", &exitError{code: notExecutable, err: fmt.Errorf("permission denied: %s", cmd)}
	}

	msg := "command not found: " + cmd
	if !strings.Contains(cmd, "/") {
		msg += " (not an hh subcommand either; see hh help)"
	}

	return "", &exitError{code: notFound, err: errors.New(msg)}
}

// claim picks a port and registers the route.
func claim(c *client.Client, r route.Route) (route.Route, error) {
	routes, err := c.Routes()
	if err != nil {
		return r, err
	}
	// A stale route for this host (wrapper just died) must not push us off our port.
	others := slices.DeleteFunc(routes, func(o route.Route) bool { return o.Host == r.Host })
	if r.Port == 0 {
		if r.Port, err = port.Pick(r.Host, taken(others)); err != nil {
			return r, err
		}
	}
	err = c.Register(r)
	var conflict *client.ConflictError
	if errors.As(err, &conflict) {
		return r, errors.New(conflict.Error())
	}

	return r, err
}

func release(c *client.Client, r route.Route) error {
	if err := c.Deregister(r.Host, r.PID); err != nil {
		return fmt.Errorf("deregister %s: %w", r.Host, err)
	}

	return nil
}

func warnTrust() {
	ok, err := trust.Trusted()
	if err != nil {
		fmt.Fprintln(os.Stderr, "hh: warning: cannot check root certificate:", err)

		return
	}
	if !ok {
		fmt.Fprintln(os.Stderr, "hh: warning: root certificate not trusted; run `hh setup`")
	}
}

func env(r route.Route, u string) []string {
	p := strconv.Itoa(r.Port)

	return append(os.Environ(), "PORT="+p, "HH_PORT="+p, "HH_URL="+u, "HH_HOST="+r.Host)
}

// spawn runs the command with inherited stdio and forwards signals to it.
// watch runs beside the child and has returned by the time spawn does.
func spawn(bin string, args []string, port int, environ []string, watch func(pid int, done <-chan struct{})) (int, error) {
	args = profile.Expand(profile.Apply(args, port), port)
	cmd := exec.Command(bin, args[1:]...)
	cmd.Args[0] = args[0]
	cmd.Env = environ
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)
	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("start %s: %w", args[0], err)
	}
	done := make(chan struct{})
	watched := make(chan struct{})
	defer func() {
		close(done)
		<-watched
	}()
	go forward(cmd.Process, sigs, done)
	go func() {
		defer close(watched)
		watch(cmd.Process.Pid, done)
	}()

	err := cmd.Wait()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return 1, fmt.Errorf("wait %s: %w", args[0], err)
	}
	ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if ok && ws.Signaled() {
		return signalBase + int(ws.Signal()), nil
	}

	return cmd.ProcessState.ExitCode(), nil
}

func forward(p *os.Process, sigs <-chan os.Signal, done <-chan struct{}) {
	for {
		select {
		case s := <-sigs:
			if err := p.Signal(s); err != nil && !errors.Is(err, os.ErrProcessDone) {
				fmt.Fprintln(os.Stderr, "hh: forward signal:", err)
			}
		case <-done:
			return
		}
	}
}

func exitCode(code int) error {
	if code == 0 {
		return nil
	}

	return &exitError{code: code}
}
