package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/eduardosasso/hh/internal/access"
	"github.com/eduardosasso/hh/internal/home"
	"github.com/eduardosasso/hh/internal/name"
	"github.com/eduardosasso/hh/internal/route"
)

const (
	// tailLast is how many past requests `hh tail` prints before following.
	tailLast = 20

	clock       = "15:04:05"
	gap         = "  "
	methodWidth = 4
	pathWidth   = 19
	pathMax     = 60
	spanWidth   = 5
	appWidth    = len("(app 999ms)")
	ellipsis    = "…"

	// appSlack is how far the upstream latency may trail the total before it prints.
	appSlack = 5 * time.Millisecond
	// routeRefresh is how long the live route list is trusted for matching.
	routeRefresh = 2 * time.Second

	kilo     = 1000
	tenfold  = 10
	upgraded = http.StatusSwitchingProtocols
)

// kinds shortens common response content types; anything else shows its subtype.
var kinds = map[string]string{
	"text/html":              "html",
	"application/json":       "json",
	"text/javascript":        "js",
	"application/javascript": "js",
	"text/css":               "css",
	"image/svg+xml":          "svg",
	"image/png":              "png",
}

var refused = regexp.MustCompile(`:(\d+): connect: connection refused`)

type tailOptions struct {
	name string
	all  bool
	json bool
	last int
}

func tailCmd(args []string) error {
	o, err := tailFlags(args)
	if err != nil {
		return err
	}
	target, err := tailHost(o)
	if err != nil {
		return err
	}
	path, err := home.Access()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(os.Stderr, "no requests yet")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	s := &scope{all: o.all, host: target, fetch: live}
	entries, err := access.Follow(ctx, path, access.Options{Last: o.last, Keep: s.keep, Warn: warner()})
	if err != nil {
		return err
	}
	p := &printer{out: os.Stdout, json: o.json, scope: s}
	for e := range entries {
		if err := p.print(e); err != nil {
			return err
		}
	}

	return nil
}

// tailFlags accepts flags before and after the app name.
func tailFlags(args []string) (tailOptions, error) {
	o := tailOptions{}
	set := flag.NewFlagSet("tail", flag.ContinueOnError)
	set.BoolVar(&o.all, "all", false, "every app")
	set.BoolVar(&o.json, "json", false, "one JSON object per request")
	set.IntVar(&o.last, "n", tailLast, "print the last N requests first")
	var names []string
	for {
		if err := set.Parse(args); err != nil {
			return o, &exitError{code: 2}
		}
		if set.NArg() == 0 {
			break
		}
		names = append(names, set.Arg(0))
		args = set.Args()[1:]
	}
	if len(names) > 1 || (o.all && len(names) > 0) {
		return o, errors.New("usage: hh tail [name] [--all] [--json] [-n N]")
	}
	if o.last < 0 {
		return o, fmt.Errorf("-n %d: want 0 or more", o.last)
	}
	if len(names) == 1 {
		o.name = names[0]
	}

	return o, nil
}

// tailHost is the host to show: the named app, else the one for this folder.
func tailHost(o tailOptions) (string, error) {
	if o.all {
		return "", nil
	}
	if o.name != "" {
		return name.Host("", name.Options{Override: o.name})
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cwd: %w", err)
	}

	return name.Host(cwd, name.Options{})
}

// warner reports unreadable log lines once per run and file errors every time.
func warner() func(error) {
	var once sync.Once

	return func(err error) {
		if !errors.Is(err, access.ErrLine) {
			fmt.Fprintln(os.Stderr, "hh:", err)

			return
		}
		once.Do(func() { fmt.Fprintf(os.Stderr, "hh: skipping unreadable request log lines (first: %v)\n", err) })
	}
}

// scope decides which requests belong to the app, using the live routes.
type scope struct {
	all     bool
	host    string
	fetch   func() ([]route.Route, error)
	mu      sync.Mutex
	routes  []route.Route
	fetched time.Time
}

func (s *scope) keep(e access.Entry) bool {
	return s.all || s.owner(e.Host).Host == s.host
}

// owner is the most specific route that serves host: the host itself or a parent
// whose wildcard covers it. A host with no route belongs to the tailed app when it
// sits under it, so tailing works before the app starts.
func (s *scope) owner(host string) route.Route {
	best := route.Route{}
	for _, r := range append(s.live(), route.Route{Host: s.host}) {
		if r.Host != "" && covers(r.Host, host) && len(r.Host) > len(best.Host) {
			best = r
		}
	}

	return best
}

func covers(parent, host string) bool {
	return host == parent || strings.HasSuffix(host, "."+parent)
}

func (s *scope) live() []route.Route {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.fetched) < routeRefresh {
		return s.routes
	}
	s.fetched = time.Now()
	rs, err := s.fetch()
	if err != nil {
		fmt.Fprintln(os.Stderr, "hh: list routes:", err)

		return s.routes
	}
	s.routes = rs

	return rs
}

type printer struct {
	out   io.Writer
	json  bool
	scope *scope
	width int
}

func (p *printer) print(e access.Entry) error {
	if p.json {
		return p.object(e)
	}
	p.width = max(p.width, len(appName(e.Host)))
	_, err := fmt.Fprintln(p.out, format(e, p.width))

	return err
}

// object is the raw log line plus app, host, and the folder of the live route.
func (p *printer) object(e access.Entry) error {
	out := make(map[string]any, len(e.Raw))
	for k, v := range e.Raw {
		out[k] = v
	}
	out["app"] = appName(e.Host)
	out["host"] = e.Host
	out["folder"] = p.scope.owner(e.Host).Cwd
	b, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	_, err = fmt.Fprintln(p.out, string(b))

	return err
}

func appName(host string) string {
	return strings.TrimSuffix(host, "."+name.TLD)
}

// format renders one request as a human line. width pads the app column.
func format(e access.Entry, width int) string {
	head := fmt.Sprintf("%s"+gap+"%-*s"+gap+"%-*s"+gap+"%-*s"+gap+"%d"+gap+"%*s",
		e.Time.Local().Format(clock), width, appName(e.Host), methodWidth, e.Method,
		pathWidth, truncate(e.URI), e.Status, spanWidth, timing(e))
	var rest []string
	if a := appTime(e); a != "" {
		rest = append(rest, fmt.Sprintf("%-*s", appWidth, a))
	}
	if d := detail(e); d != "" {
		rest = append(rest, d)
	}
	if len(rest) == 0 {
		return head
	}

	return strings.TrimRight(head+gap+strings.Join(rest, " "), " ")
}

func upgrade(e access.Entry) bool { return e.Status == upgraded && e.Error == "" }

// timing is the total duration, or how long a WebSocket stayed open.
func timing(e access.Entry) string {
	if upgrade(e) {
		return "ws " + span(e.Duration)
	}

	return span(e.Duration)
}

// appTime shows the upstream latency when it differs enough from the total.
func appTime(e access.Entry) string {
	if e.App == 0 || upgrade(e) || e.Duration-e.App <= appSlack {
		return ""
	}

	return "(app " + span(e.App) + ")"
}

func detail(e access.Entry) string {
	switch {
	case e.Error != "":
		return "!! " + reason(e.Error)
	case upgrade(e):
		return ""
	case e.Status >= http.StatusMultipleChoices && e.Status < http.StatusBadRequest && e.Location != "":
		return "→ " + e.Location
	}

	return strings.TrimSpace(kind(e.Type) + " " + bytesize(e.Size))
}

// reason turns the proxy error Caddy recorded into a few words.
func reason(err string) string {
	if m := refused.FindStringSubmatch(err); m != nil {
		return "nothing listens on " + m[1]
	}
	if strings.Contains(err, "timeout") || strings.Contains(err, "deadline exceeded") {
		return "timeout"
	}

	return err
}

func kind(contentType string) string {
	if contentType == "" {
		return ""
	}
	t, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		t, _, _ = strings.Cut(contentType, ";")
	}
	if k, ok := kinds[t]; ok {
		return k
	}
	_, sub, _ := strings.Cut(t, "/")

	return sub
}

func truncate(uri string) string {
	r := []rune(uri)
	if len(r) <= pathMax {
		return uri
	}

	return string(r[:pathMax-1]) + ellipsis
}

// bytesize prints 812B, 1.2kB, 12kB, 3.4MB. Zero prints nothing.
func bytesize(n int) string {
	switch {
	case n == 0:
		return ""
	case n < kilo:
		return fmt.Sprintf("%dB", n)
	case n < kilo*kilo:
		return scaled(float64(n)/kilo, "kB")
	}

	return scaled(float64(n)/(kilo*kilo), "MB")
}

// scaled keeps one decimal below ten: 1.2kB, 12kB.
func scaled(v float64, unit string) string {
	if v >= tenfold {
		return fmt.Sprintf("%.0f%s", v, unit)
	}

	return strings.TrimSuffix(fmt.Sprintf("%.1f", v), ".0") + unit
}

// span prints <1ms, 41ms, 1.2s, 32s, 2m5s.
func span(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return "<1ms"
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < tenfold*time.Second:
		return scaled(d.Seconds(), "s")
	case d < time.Minute:
		return fmt.Sprintf("%.0fs", d.Seconds())
	}

	return d.Round(time.Second).String()
}
