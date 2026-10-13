package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/eduardosasso/hh/internal/client"
	"github.com/eduardosasso/hh/internal/home"
	"github.com/eduardosasso/hh/internal/name"
	"github.com/eduardosasso/hh/internal/port"
	"github.com/eduardosasso/hh/internal/route"
)

type address struct {
	URL  string `json:"url"`
	Host string `json:"host"`
	Port int    `json:"port"`
}

func urlCmd(args []string) error {
	fs := flag.NewFlagSet("url", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print {url, host, port}")
	if err := fs.Parse(args); err != nil {
		return err
	}
	a, err := resolve(name.Options{})
	if err != nil {
		return err
	}
	if !*asJSON {
		fmt.Println(a.URL)

		return nil
	}
	return json.NewEncoder(os.Stdout).Encode(a)
}

// resolve reports the address without starting anything. A live route keeps its port.
func resolve(o name.Options) (address, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return address{}, fmt.Errorf("cwd: %w", err)
	}
	host, err := name.Host(cwd, o)
	if err != nil {
		return address{}, err
	}
	u, err := home.URL(host)
	if err != nil {
		return address{}, err
	}
	routes, err := live()
	if err != nil {
		return address{}, err
	}
	for _, r := range routes {
		if r.Host == host {
			return address{URL: u, Host: host, Port: r.Port}, nil
		}
	}
	p, err := port.Pick(host, taken(routes))
	if err != nil {
		return address{}, err
	}

	return address{URL: u, Host: host, Port: p}, nil
}

// live lists routes, or none when the daemon is down.
func live() ([]route.Route, error) {
	c, err := client.New()
	if err != nil {
		return nil, err
	}
	rs, err := c.Routes()
	if errors.Is(err, client.ErrDown) {
		return nil, nil
	}

	return rs, err
}

func taken(rs []route.Route) map[int]bool {
	m := map[int]bool{}
	for _, r := range rs {
		m[r.Port] = true
		for _, p := range r.Paths {
			m[p.Port] = true
		}
	}

	return m
}
