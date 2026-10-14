package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/eduardosasso/hh/internal/listen"
	"github.com/eduardosasso/hh/internal/wait"
)

// graceful is how long leftover processes get between SIGTERM and SIGKILL.
const graceful = 2 * time.Second

// reap stops what the child left running. It signals only pids from family,
// and only while they still run the same command.
func reap(family []listen.Process) {
	left, err := listen.Running(family)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hh: find leftover processes:", err)

		return
	}
	if len(left) == 0 {
		return
	}
	send(left, syscall.SIGTERM)
	err = wait.Until(graceful, func() (bool, error) {
		alive, err := listen.Running(left)

		return len(alive) == 0, err
	})
	if errors.Is(err, wait.ErrTimeout) {
		alive, rerr := listen.Running(left)
		if rerr != nil {
			fmt.Fprintln(os.Stderr, "hh: find leftover processes:", rerr)
		}
		send(alive, syscall.SIGKILL)
	} else if err != nil {
		fmt.Fprintln(os.Stderr, "hh: find leftover processes:", err)
	}
	fmt.Fprintln(os.Stderr, stopped(left))
}

func send(ps []listen.Process, sig syscall.Signal) {
	for _, p := range ps {
		if err := syscall.Kill(p.PID, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
			fmt.Fprintf(os.Stderr, "hh: %s pid %d: %v\n", sig, p.PID, err)
		}
	}
}

// stopped reads like: hh: stopped 2 leftover processes (stripe, wrangler)
func stopped(ps []listen.Process) string {
	var names []string
	for _, p := range ps {
		if !slices.Contains(names, p.Command) {
			names = append(names, p.Command)
		}
	}
	noun := "processes"
	if len(ps) == 1 {
		noun = "process"
	}

	return fmt.Sprintf("hh: stopped %d leftover %s (%s)", len(ps), noun, strings.Join(names, ", "))
}
