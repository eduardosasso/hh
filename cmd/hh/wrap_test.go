package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/eduardosasso/hh/internal/route"
)

func TestParse(t *testing.T) {
	t.Setenv(portEnv, "")
	o, err := parse([]string{"--name", "blog", "--port", "3000", "--route", "/hmr=24678", "bun", "run", "dev"})
	if err != nil {
		t.Fatal(err)
	}
	if o.name != "blog" || o.port != 3000 || o.label != "" {
		t.Fatalf("got %+v", o)
	}
	if !slices.Equal(o.paths, []route.Path{{Path: "/hmr", Port: 24678}}) {
		t.Fatalf("paths %+v", o.paths)
	}
	if !slices.Equal(o.cmd, []string{"bun", "run", "dev"}) {
		t.Fatalf("cmd %v", o.cmd)
	}
}

func TestParseLabel(t *testing.T) {
	o, err := parse([]string{"api", "--", "bun", "run", "api", "--", "x"})
	if err != nil {
		t.Fatal(err)
	}
	if o.label != "api" || !slices.Equal(o.cmd, []string{"bun", "run", "api", "--", "x"}) {
		t.Fatalf("got %+v", o)
	}
}

func TestParseChildFlags(t *testing.T) {
	t.Setenv(portEnv, "")
	o, err := parse([]string{"vite", "--port", "5173"})
	if err != nil {
		t.Fatal(err)
	}
	if o.port != 0 || !slices.Equal(o.cmd, []string{"vite", "--port", "5173"}) {
		t.Fatalf("got %+v", o)
	}
}

func TestParseErrors(t *testing.T) {
	t.Setenv(portEnv, "")
	for _, args := range [][]string{{}, {"api", "--"}, {"--route", "hmr=1", "x"}, {"--route", "/hmr=x", "x"}} {
		if _, err := parse(args); err == nil {
			t.Errorf("parse(%v) accepted", args)
		}
	}
}

func TestLookup(t *testing.T) {
	_, err := lookup("hh-no-such-command")
	if code(err) != notFound {
		t.Fatalf("got exit %d", code(err))
	}
}

func TestParseEnvPort(t *testing.T) {
	t.Setenv(portEnv, "54118")
	o, err := parse([]string{"./bin/dev.sh"})
	if err != nil || o.port != 54118 {
		t.Fatalf("port %d, err %v", o.port, err)
	}
	o, err = parse([]string{"--port", "3000", "./bin/dev.sh"})
	if err != nil || o.port != 3000 {
		t.Fatalf("--port lost to env: port %d, err %v", o.port, err)
	}
}

func TestParseEnvPortInvalid(t *testing.T) {
	for _, v := range []string{"abc", "0", "65536", "-1", "80x"} {
		t.Setenv(portEnv, v)
		_, err := parse([]string{"./bin/dev.sh"})
		if err == nil || !strings.Contains(err.Error(), "is not a port") {
			t.Errorf("PORT=%q: err %v", v, err)
		}
	}
}
