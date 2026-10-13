package main

import (
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/eduardosasso/hh/internal/route"
)

func lsCmd([]string) error {
	routes, err := live()
	if err != nil {
		return err
	}
	if len(routes) == 0 {
		fmt.Println("no live routes")

		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "HOST\tPORT\tPID\tUPTIME\tFOLDER")
	for _, r := range routes {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n", r.Host, ports(r.Port, r.Paths), r.PID, uptime(r.Started), r.Cwd)
	}

	return w.Flush()
}

func uptime(start time.Time) string {
	return time.Since(start).Truncate(time.Second).String()
}

// ports shows extra --route ports after the main one, like 41000 /hmr=41001.
func ports(main int, paths []route.Path) string {
	s := strconv.Itoa(main)
	for _, p := range paths {
		s += fmt.Sprintf(" %s=%d", p.Path, p.Port)
	}

	return s
}
