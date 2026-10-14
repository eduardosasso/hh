// Command hh runs a dev command on a managed port behind https://<name>.localhost.
package main

import (
	"errors"
	"fmt"
	"os"
)

const usage = `hh — HTTPS here

Usage:
  hh [flags] <cmd...>            run cmd on a managed port, print its URL
  hh [flags] <label> -- <cmd...> same, as <label>.<host>
  hh url [--json]                print the URL for this folder
  hh ls                          list live routes
  hh who [port]                  show which process listens on each port
  hh open                        open the URL in the browser
  hh init                        bake the hh prefix into this project's dev script
  hh setup [--agents|--no-agents] one-time machine setup; --agents adds the rule to ~/AGENTS.md, --no-agents skips it
  hh trust                       trust the root certificate, including Firefox
  hh daemon                      run the proxy in the foreground
  hh logs                        tail daemon logs
  hh tail [name] [--all] [--json] [-n N]  stream requests for this app

Flags:
  --name <n>            override the derived name
  --port <p>            use a fixed port
  --route <path>=<port> send path to another local port (repeatable)
`

// exitError carries a specific exit code up to main.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }

var commands = map[string]func([]string) error{
	"url":    urlCmd,
	"ls":     lsCmd,
	"who":    whoCmd,
	"open":   openCmd,
	"init":   initCmd,
	"setup":  setupCmd,
	"trust":  trustCmd,
	"daemon": daemonCmd,
	"logs":   logsCmd,
	"tail":   tailCmd,
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(usage)

		return 0
	}
	cmd, ok := commands[args[0]]
	if !ok {
		return code(wrap(args))
	}

	return code(cmd(args[1:]))
}

func code(err error) int {
	if err == nil {
		return 0
	}
	var e *exitError
	if errors.As(err, &e) {
		if e.err != nil {
			fmt.Fprintln(os.Stderr, "hh:", e.err)
		}

		return e.code
	}
	fmt.Fprintln(os.Stderr, "hh:", err)

	return 1
}
