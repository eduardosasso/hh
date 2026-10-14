package main

import (
	"errors"
	"testing"

	"github.com/eduardosasso/hh/internal/listen"
	"github.com/eduardosasso/hh/internal/route"
)

const user = "/Users/me"

var routes = []route.Route{
	{Host: "overdew.localhost", PID: 100, Cwd: user + "/moonshot/overdew"},
	{Host: "quip.localhost", PID: 200, Cwd: user + "/code/quip"},
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
	got := busy("quip.localhost", 3000, 47433, holder(held, own, noCwd, user))
	want := "hh: quip.localhost usually listens on 3000, but 3000 is held by bun (pid 4242) running overdew.localhost from ~/moonshot/overdew\n" +
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

func TestPick(t *testing.T) {
	r := route.Route{Port: 47433, Paths: []route.Path{{Path: "/hmr", Port: 24678}}}
	cases := []struct {
		ss   []listen.Socket
		want int
	}{
		{nil, 0},
		{[]listen.Socket{{Port: 9229}, {Port: 47433}}, 47433},
		{[]listen.Socket{{Port: 24678}}, 0},
		{[]listen.Socket{{Port: 24678}, {Port: 9229}, {Port: 8787}}, 8787},
	}
	for _, c := range cases {
		if got := pick(c.ss, r); got != c.want {
			t.Errorf("pick(%v) = %d, want %d", c.ss, got, c.want)
		}
	}
}

func TestMovedLine(t *testing.T) {
	got := movedLine("https://quip.localhost", 8787, 47433)
	if want := "→ https://quip.localhost (app listens on 8787, ignoring PORT=47433)"; got != want {
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
