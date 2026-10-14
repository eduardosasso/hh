package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eduardosasso/hh/internal/access"
	"github.com/eduardosasso/hh/internal/route"
)

func at() time.Time {
	return time.Date(2026, 10, 14, 16, 2, 11, 0, time.Local)
}

func entry(method, uri string, status int, total, app time.Duration) access.Entry {
	return access.Entry{Time: at(), Host: "shop.localhost", Method: method, URI: uri, Status: status, Duration: total, App: app}
}

func TestFormat(t *testing.T) {
	html := entry("GET", "/products/42", 200, 41*time.Millisecond, 38*time.Millisecond)
	html.Type, html.Size = "text/html; charset=utf-8", 12000

	pledge := entry("POST", "/api/orders", 500, 812*time.Millisecond, 790*time.Millisecond)
	pledge.Type, pledge.Size = "application/json", 1234

	checkout := entry("GET", "/checkout", 303, 9*time.Millisecond, 9*time.Millisecond)
	checkout.Location = "https://checkout.stripe.com/c/pay/cs_test"

	ws := entry("GET", "/__vite_hmr", 101, 32*time.Second, time.Millisecond)

	dead := entry("GET", "/api/health", 502, time.Millisecond, 0)
	dead.Error = "dial tcp [::1]:47433: connect: connection refused"

	slow := entry("GET", "/slow", 504, 3*time.Second, 0)
	slow.Error = "net/http: timeout awaiting response headers"

	long := entry("GET", "/"+strings.Repeat("a", 80), 200, 1200*time.Millisecond, 1198*time.Millisecond)
	long.Type, long.Size = "text/plain", 812

	png := entry("GET", "/logo.png", 200, 300*time.Microsecond, 200*time.Microsecond)
	png.Type, png.Size = "image/png", 3_400_000

	cases := []struct {
		name string
		in   access.Entry
		want string
	}{
		{"html within app slack", html, "16:02:11  shop  GET   /products/42         200   41ms  html 12kB"},
		{"json with app time", pledge, "16:02:11  shop  POST  /api/orders          500  812ms  (app 790ms) json 1.2kB"},
		{"redirect", checkout, "16:02:11  shop  GET   /checkout            303    9ms  → https://checkout.stripe.com/c/pay/cs_test"},
		{"websocket", ws, "16:02:11  shop  GET   /__vite_hmr          101  ws 32s"},
		{"refused", dead, "16:02:11  shop  GET   /api/health          502    1ms  !! nothing listens on 47433"},
		{"timeout", slow, "16:02:11  shop  GET   /slow                504     3s  !! timeout"},
		{"truncated", long, "16:02:11  shop  GET   /" + strings.Repeat("a", 58) + "…  200   1.2s  plain 812B"},
		{"sub millisecond", png, "16:02:11  shop  GET   /logo.png            200   <1ms  png 3.4MB"},
	}
	for _, c := range cases {
		if got := format(c.in, len("shop")); got != c.want {
			t.Errorf("%s:\ngot  %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestSpanAndSize(t *testing.T) {
	spans := map[time.Duration]string{
		500 * time.Microsecond:  "<1ms",
		41 * time.Millisecond:   "41ms",
		1200 * time.Millisecond: "1.2s",
		32 * time.Second:        "32s",
		125 * time.Second:       "2m5s",
	}
	for d, want := range spans {
		if got := span(d); got != want {
			t.Errorf("span(%s) = %q, want %q", d, got, want)
		}
	}
	sizes := map[int]string{0: "", 812: "812B", 1000: "1kB", 1234: "1.2kB", 12000: "12kB", 3_400_000: "3.4MB"}
	for n, want := range sizes {
		if got := bytesize(n); got != want {
			t.Errorf("bytesize(%d) = %q, want %q", n, got, want)
		}
	}
	kindsWant := map[string]string{"text/html; charset=utf-8": "html", "text/javascript": "js", "text/css": "css", "image/svg+xml": "svg", "font/woff2": "woff2", "": ""}
	for ct, want := range kindsWant {
		if got := kind(ct); got != want {
			t.Errorf("kind(%q) = %q, want %q", ct, got, want)
		}
	}
}

func scoped(all bool, host string, routes ...route.Route) *scope {
	return &scope{all: all, host: host, fetch: func() ([]route.Route, error) { return routes, nil }}
}

func TestScope(t *testing.T) {
	routes := []route.Route{
		{Host: "shop.localhost", Cwd: "/code/shop"},
		{Host: "api.shop.localhost", Cwd: "/code/shop"},
		{Host: "wt.shop.localhost", Cwd: "/code/shop-wt"},
		{Host: "other.localhost"},
	}
	s := scoped(false, "shop.localhost", routes...)
	cases := map[string]bool{
		"shop.localhost":           true,
		"acme.shop.localhost":      true,
		"api.shop.localhost":       false,
		"x.api.shop.localhost":     false,
		"wt.shop.localhost":        false,
		"other.localhost":          false,
		"notshop.localhost":        false,
		"acme.other.localhost":     false,
		"deep.acme.shop.localhost": true,
	}
	for host, want := range cases {
		if got := s.keep(access.Entry{Host: host}); got != want {
			t.Errorf("keep(%s) = %v, want %v", host, got, want)
		}
	}
	if !scoped(true, "", routes...).keep(access.Entry{Host: "api.shop.localhost"}) {
		t.Error("--all must keep every host")
	}
	// Tailing an app that is not running still matches its host.
	if !scoped(false, "idle.localhost").keep(access.Entry{Host: "t.idle.localhost"}) {
		t.Error("idle app tenant not kept")
	}
	if got := s.owner("acme.wt.shop.localhost").Cwd; got != "/code/shop-wt" {
		t.Errorf("owner folder %q", got)
	}
}

func TestTailFlags(t *testing.T) {
	o, err := tailFlags([]string{"shop", "-n", "5", "--json"})
	if err != nil || o.name != "shop" || o.last != 5 || !o.json {
		t.Fatalf("%+v %v", o, err)
	}
	if o, err = tailFlags(nil); err != nil || o.last != tailLast {
		t.Fatalf("%+v %v", o, err)
	}
	for _, bad := range [][]string{{"a", "b"}, {"--all", "a"}, {"-n", "-1"}} {
		if _, err := tailFlags(bad); err == nil {
			t.Errorf("%v: no error", bad)
		}
	}
}

func TestJSON(t *testing.T) {
	var out bytes.Buffer
	p := &printer{out: &out, json: true, scope: scoped(false, "shop.localhost", route.Route{Host: "shop.localhost", Cwd: "/code/shop"})}
	e := access.Entry{Host: "acme.shop.localhost", Raw: map[string]any{"status": float64(200)}}
	if err := p.print(e); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["app"] != "acme.shop" || got["host"] != "acme.shop.localhost" || got["folder"] != "/code/shop" || got["status"] != float64(200) {
		t.Fatalf("%v", got)
	}
}
