package daemon

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/eduardosasso/hh/internal/route"
)

func TestProxiesOrder(t *testing.T) {
	routes := []route.Route{
		{Host: "overdew.localhost", Port: 1},
		{Host: "wt-173.overdew.localhost", Port: 2},
		{Host: "quip.localhost", Port: 3},
	}
	var got []string
	for _, p := range proxies(routes) {
		got = append(got, p["match"].([]object)[0]["host"].([]string)[0])
	}
	want := []string{
		"wt-173.overdew.localhost",
		"overdew.localhost",
		"quip.localhost",
		"*.wt-173.overdew.localhost",
		"*.overdew.localhost",
		"*.quip.localhost",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestPathRoutes(t *testing.T) {
	h := handler(route.Route{Host: "a.localhost", Port: 1, Paths: []route.Path{{Path: "/hmr", Port: 2}}})
	b, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"subroute"`, `"/hmr","/hmr/*"`, `"localhost:2"`, `"localhost:1"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in %s", want, s)
		}
	}
	if strings.Index(s, "localhost:2") > strings.Index(s, "localhost:1") {
		t.Error("path route must come before the default upstream")
	}
}

func TestConfigValid(t *testing.T) {
	b, err := Config([]route.Route{{Host: "quip.localhost", Port: 40000}}, Settings{Storage: "/s", Log: "/l", HTTPS: 443, HTTP: 80})
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"disabled":true`) {
		t.Error("admin endpoint must be disabled")
	}
}
