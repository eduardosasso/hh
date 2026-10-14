package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strconv"
	"text/tabwriter"

	"github.com/eduardosasso/hh/internal/listen"
)

const none = "-"

func whoCmd(args []string) error {
	fs := flag.NewFlagSet("who", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return errors.New("usage: hh who [port]")
	}
	want := 0
	if fs.NArg() == 1 {
		p, err := strconv.Atoi(fs.Arg(0))
		if err != nil {
			return fmt.Errorf("port %q: %w", fs.Arg(0), err)
		}
		want = p
	}
	ss, err := listen.All()
	if err != nil {
		return err
	}
	if want != 0 {
		ss = slices.DeleteFunc(ss, func(s listen.Socket) bool { return s.Port != want })
	}
	if len(ss) == 0 && want != 0 {
		return &exitError{code: 1, err: fmt.Errorf("nothing listens on %d", want)}
	}
	if len(ss) == 0 {
		fmt.Println("nothing listens")

		return nil
	}
	routes, err := live()
	if err != nil {
		return err
	}
	own, err := owners(routes, listen.Descendants)
	if err != nil {
		return err
	}
	user, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PORT\tPID\tCOMMAND\tHOST\tFOLDER")
	for _, s := range ss {
		host, folder := none, none
		if r, ok := own[s.PID]; ok {
			host, folder = r.Host, tilde(r.Cwd, user)
		}
		fmt.Fprintf(w, "%d\t%d\t%s\t%s\t%s\n", s.Port, s.PID, s.Command, host, folder)
	}

	return w.Flush()
}
