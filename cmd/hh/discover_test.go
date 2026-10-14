package main

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/eduardosasso/hh/internal/listen"
	"github.com/eduardosasso/hh/internal/route"
)

const user = "/Users/me"

var routes = []route.Route{
	{Host: "blog.localhost", PID: 100, Cwd: user + "/code/blog"},
	{Host: "shop.localhost", PID: 200, Cwd: user + "/code/shop"},
}

func tree(pid int) ([]int, error) {
	return map[int][]int{100: {4242, 4243}, 200: {201}}[pid], nil
}

func noCwd(int) (string, error) { return "", errors.New("unexpected cwd lookup") }

func TestBusyOwned(t *testing.T) {
	own, err := owners(routes, tree)
	if err != nil {
		t.Fatal(err)
	}
	held := []listen.Socket{{Port: 3000, PID: 4242, Command: "bun"}}
	got := busy("shop.localhost", 3000, 47433, holder(held, own, noCwd, user))
	want := "hh: shop.localhost usually listens on 3000, but 3000 is held by bun (pid 4242) running blog.localhost from ~/code/blog\n" +
		"hh: free port for this run: 47433. Read $PORT in your dev script to use it."
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestBusyForeign(t *testing.T) {
	own, err := owners(routes, tree)
	if err != nil {
		t.Fatal(err)
	}
	held := []listen.Socket{{Port: 3000, PID: 9000, Command: "node"}}
	cwd := func(int) (string, error) { return user + "/code/other", nil }
	if got, want := holder(held, own, cwd, user), "node (pid 9000) in ~/code/other"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := holder(nil, own, noCwd, user), "another process"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestOwnersError(t *testing.T) {
	_, err := owners(routes, func(int) ([]int, error) { return nil, errors.New("ps failed") })
	if err == nil {
		t.Fatal("tree error dropped")
	}
}

func TestOpen(t *testing.T) {
	paths := []route.Path{{Path: "/hmr", Port: 24678}}
	ss := []listen.Socket{{Port: 9229, PID: 1}, {Port: 24678, PID: 1}, {Port: 8787, PID: 1}, {Port: 8787, PID: 2}}
	if got := open(ss, paths); !slices.Equal(got, []int{8787, 9229}) {
		t.Fatalf("got %v", got)
	}
}

const (
	assigned = 47433
	internal = 54141
	usualAt  = 3000
)

type fakeDaemon struct {
	moves  []int
	usual  map[string]int
	output strings.Builder
}

func fake(usual int) (*watcher, *fakeDaemon) {
	d := &fakeDaemon{usual: map[string]int{}}
	w := &watcher{
		register: func(r route.Route) error { d.moves = append(d.moves, r.Port); return nil },
		remember: func(host string, p int) error { d.usual[host] = p; return nil },
		forget:   func(host string) error { delete(d.usual, host); return nil },
		out:      &d.output,
		r:        route.Route{Host: "shop.localhost", Port: assigned},
		url:      "https://shop.localhost",
		usual:    usual,
		routed:   assigned,
	}

	return w, d
}

func TestSeePortLater(t *testing.T) {
	w, d := fake(0)
	for _, snap := range [][]int{nil, {internal}, {internal}, {internal, assigned}, {internal, assigned}} {
		w.see(snap)
	}
	if w.found != assigned || !slices.Equal(d.moves, []int{internal, assigned}) {
		t.Fatalf("found %d, moves %v", w.found, d.moves)
	}
	if _, ok := d.usual["shop.localhost"]; ok {
		t.Fatalf("usual port kept: %v", d.usual)
	}
	want := "→ https://shop.localhost (app listens on 54141, ignoring PORT=47433)\n" +
		"→ https://shop.localhost (app now listens on PORT=47433)\n"
	if got := d.output.String(); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestSeePortFirst(t *testing.T) {
	w, d := fake(0)
	w.see([]int{assigned})
	w.see([]int{internal, assigned})
	if w.found != assigned || len(d.moves) != 0 || d.output.Len() != 0 {
		t.Fatalf("found %d, moves %v, output %q", w.found, d.moves, d.output.String())
	}
}

func TestSeeUsual(t *testing.T) {
	w, d := fake(usualAt)
	w.see([]int{internal})
	w.see([]int{usualAt, internal})
	if w.found != usualAt || !slices.Equal(d.moves, []int{internal, usualAt}) {
		t.Fatalf("found %d, moves %v", w.found, d.moves)
	}
	if d.usual["shop.localhost"] != usualAt {
		t.Fatalf("usual %v", d.usual)
	}
}

func TestSeeStable(t *testing.T) {
	w, d := fake(0)
	w.see([]int{9000})
	w.see([]int{8000, 9000})
	if w.found != 9000 || !slices.Equal(d.moves, []int{9000}) {
		t.Fatalf("found %d, moves %v", w.found, d.moves)
	}
}

func TestSeeClosed(t *testing.T) {
	w, d := fake(0)
	w.see([]int{9000})
	w.see([]int{8000, 9000})
	w.see([]int{8000})
	if w.found != 8000 || !slices.Equal(d.moves, []int{9000, 8000}) {
		t.Fatalf("found %d, moves %v", w.found, d.moves)
	}
	w.see(nil)
	if w.found != 8000 || len(d.moves) != 2 {
		t.Fatalf("moved with nothing listening: found %d, moves %v", w.found, d.moves)
	}
}

func TestChoose(t *testing.T) {
	cases := []struct {
		listening, seen      []int
		usual, current, want int
	}{
		{nil, nil, 0, 0, 0},
		{[]int{9229, assigned}, []int{9229, assigned}, 0, 9229, assigned},
		{[]int{9229, usualAt}, []int{9229, usualAt}, usualAt, 9229, usualAt},
		{[]int{8000, 9000}, []int{9000, 8000}, 0, 0, 9000},
		{[]int{8000}, []int{9000, 8000}, usualAt, 9000, 8000},
	}
	for _, c := range cases {
		if got := choose(c.listening, c.seen, assigned, c.usual, c.current); got != c.want {
			t.Errorf("choose(%v, %v, usual %d, current %d) = %d, want %d", c.listening, c.seen, c.usual, c.current, got, c.want)
		}
	}
}

func TestMovedLine(t *testing.T) {
	got := movedLine("https://shop.localhost", 8787, assigned)
	if want := "→ https://shop.localhost (app listens on 8787, ignoring PORT=47433)"; got != want {
		t.Fatalf("got %q", got)
	}
}

func TestTilde(t *testing.T) {
	for in, want := range map[string]string{user: "~", user + "/x": "~/x", user + "x": user + "x", "/tmp": "/tmp"} {
		if got := tilde(in, user); got != want {
			t.Errorf("tilde(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStopped(t *testing.T) {
	got := stopped([]listen.Process{{PID: 1, Command: "stripe"}, {PID: 2, Command: "wrangler"}, {PID: 3, Command: "wrangler"}})
	if want := "hh: stopped 3 leftover processes (stripe, wrangler)"; got != want {
		t.Fatalf("got %q", got)
	}
	if got := stopped([]listen.Process{{PID: 1, Command: "sleep"}}); got != "hh: stopped 1 leftover process (sleep)" {
		t.Fatalf("got %q", got)
	}
}
