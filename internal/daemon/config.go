package daemon

import (
	"cmp"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"slices"
	"strings"

	"github.com/eduardosasso/hh/internal/home"
	"github.com/eduardosasso/hh/internal/route"
)

// streamCloseDelay keeps WebSockets (HMR) open across config reloads,
// which happen every time any app starts or stops.
const streamCloseDelay = "24h"

// Settings are the machine-level inputs to the Caddy config.
type Settings struct {
	Storage string
	Log     string
	HTTPS   int
	HTTP    int
}

type object = map[string]any

// Config builds the Caddy JSON config for routes.
func Config(routes []route.Route, s Settings) ([]byte, error) {
	cfg := object{
		"admin":   object{"disabled": true, "config": object{"persist": false}},
		"storage": object{"module": "file_system", "root": s.Storage},
		"logging": object{"logs": object{"default": object{
			"writer":  object{"output": "file", "filename": s.Log, "roll": false},
			"encoder": object{"format": "console", "level_format": "upper", "time_local": true},
		}}},
		"apps": object{
			"http": object{
				"http_port":  s.HTTP,
				"https_port": s.HTTPS,
				"servers": object{
					"https": object{
						"listen":                  []string{fmt.Sprintf(":%d", s.HTTPS)},
						"routes":                  append(proxies(routes), fallback(routes, s.HTTPS)),
						"tls_connection_policies": []object{{}},
						"automatic_https":         object{"disable_redirects": true},
					},
					"redirect": object{
						"listen": []string{fmt.Sprintf(":%d", s.HTTP)},
						"routes": []object{redirect(s.HTTPS)},
					},
				},
			},
			"tls": object{"automation": object{"policies": []object{{
				"issuers":               []object{{"module": "internal", "ca": "local"}},
				"on_demand":             true,
				"disable_ocsp_stapling": true,
			}}}},
			"pki": object{"certificate_authorities": object{"local": object{
				"name":                     "hh",
				"root_common_name":         "hh Local Root",
				"intermediate_common_name": "hh Local Intermediate",
				"install_trust":            false,
			}}},
		},
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("encode caddy config: %w", err)
	}

	return b, nil
}

type match struct {
	pattern string
	route   route.Route
}

func (m match) wildcard() bool { return strings.HasPrefix(m.pattern, "*.") }

// order puts exact hosts before wildcards, and deeper names first, so the most
// specific host wins: wt-173.overdew.localhost beats *.overdew.localhost.
func order(a, b match) int {
	if a.wildcard() != b.wildcard() {
		if a.wildcard() {
			return 1
		}

		return -1
	}

	return cmp.Or(
		cmp.Compare(strings.Count(b.pattern, "."), strings.Count(a.pattern, ".")),
		strings.Compare(a.pattern, b.pattern),
	)
}

func proxies(routes []route.Route) []object {
	var ms []match
	for _, r := range routes {
		ms = append(ms, match{r.Host, r}, match{"*." + r.Host, r})
	}
	slices.SortFunc(ms, order)
	out := make([]object, len(ms))
	for i, m := range ms {
		out[i] = object{
			"match":    []object{{"host": []string{m.pattern}}},
			"handle":   []object{handler(m.route)},
			"terminal": true,
		}
	}

	return out
}

func handler(r route.Route) object {
	if len(r.Paths) == 0 {
		return proxy(r.Port)
	}
	var subs []object
	for _, p := range r.Paths {
		subs = append(subs, object{
			"match":  []object{{"path": prefixes(p.Path)}},
			"handle": []object{proxy(p.Port)},
		})
	}
	subs = append(subs, object{"handle": []object{proxy(r.Port)}})

	return object{"handler": "subroute", "routes": subs}
}

func prefixes(p string) []string {
	if strings.HasSuffix(p, "*") {
		return []string{p}
	}
	p = strings.TrimSuffix(p, "/")

	return []string{p, p + "/*"}
}

// proxy dials localhost so apps bound to either 127.0.0.1 or ::1 work.
func proxy(port int) object {
	return object{
		"handler":            "reverse_proxy",
		"upstreams":          []object{{"dial": fmt.Sprintf("localhost:%d", port)}},
		"stream_close_delay": streamCloseDelay,
	}
}

func redirect(https int) object {
	target := home.Address("{http.request.host}", https) + "{http.request.uri}"

	return object{"handle": []object{{
		"handler":     "static_response",
		"status_code": http.StatusPermanentRedirect,
		"headers":     object{"Location": []string{target}},
	}}}
}

// fallback answers unknown hosts with 503 and the live routes.
func fallback(routes []route.Route, https int) object {
	var b strings.Builder
	b.WriteString(`<!doctype html><meta charset="utf-8"><title>hh: no app here</title>`)
	b.WriteString(`<body style="font-family: system-ui; margin: 3rem"><h1>No app on this host</h1>`)
	if len(routes) == 0 {
		b.WriteString("<p>No live routes. Start one with <code>hh &lt;cmd&gt;</code>.</p>")
	} else {
		b.WriteString("<p>Live routes:</p><ul>")
		for _, r := range sorted(routes) {
			fmt.Fprintf(&b, `<li><a href="%s">%s</a> → %d <small>%s</small></li>`,
				html.EscapeString(home.Address(r.Host, https)), html.EscapeString(r.Host), r.Port, html.EscapeString(r.Cwd))
		}
		b.WriteString("</ul>")
	}

	return object{"handle": []object{{
		"handler":     "static_response",
		"status_code": http.StatusServiceUnavailable,
		"headers":     object{"Content-Type": []string{"text/html; charset=utf-8"}},
		"body":        b.String(),
	}}}
}

func sorted(routes []route.Route) []route.Route {
	out := slices.Clone(routes)
	slices.SortFunc(out, func(a, b route.Route) int { return strings.Compare(a.Host, b.Host) })

	return out
}
