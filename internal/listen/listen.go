// Package listen finds listening TCP sockets and the process trees that own them.
package listen

import (
	"cmp"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Socket is a listening TCP port and the process that holds it.
type Socket struct {
	Port    int
	PID     int
	Command string
	// Cwd is empty until the caller fills it with Cwd(PID), only when it prints it.
	Cwd string
}

// entry is a raw listening socket. A nil ip means all interfaces.
type entry struct {
	pid  int
	port int
	ip   net.IP
}

// reachable reports whether a client on this machine reaches the socket via loopback.
func (e entry) reachable() bool {
	return e.ip == nil || e.ip.IsLoopback() || e.ip.IsUnspecified()
}

type proc struct {
	ppid int
	comm string
}

// Listening returns the sockets that pids listen on via loopback or all interfaces.
func Listening(pids []int) ([]Socket, error) {
	if len(pids) == 0 {
		return nil, nil
	}
	es, err := scan(pids)
	if err != nil {
		return nil, err
	}

	return label(slices.DeleteFunc(es, func(e entry) bool { return !e.reachable() }))
}

// All returns every listening socket on the machine that this user can see.
func All() ([]Socket, error) {
	es, err := scan(nil)
	if err != nil {
		return nil, err
	}

	return label(es)
}

// Cwd is the working directory of pid.
func Cwd(pid int) (string, error) { return cwd(pid) }

// Process is a running process and its command name.
type Process struct {
	PID     int
	Command string
}

// Descendants returns every process below pid, not pid itself.
func Descendants(pid int) ([]int, error) {
	ps, err := procs()
	if err != nil {
		return nil, err
	}

	return descendants(ps, pid), nil
}

// Tree is Descendants with each command name, so a later check can tell a
// reused pid from the original process.
func Tree(pid int) ([]Process, error) {
	ps, err := procs()
	if err != nil {
		return nil, err
	}

	return named(ps, descendants(ps, pid)), nil
}

// Running returns the processes in want that still run the same command.
func Running(want []Process) ([]Process, error) {
	if len(want) == 0 {
		return nil, nil
	}
	ps, err := procs()
	if err != nil {
		return nil, err
	}

	return slices.DeleteFunc(slices.Clone(want), func(p Process) bool {
		cur, ok := ps[p.PID]

		return !ok || cur.comm != p.Command
	}), nil
}

func named(ps map[int]proc, pids []int) []Process {
	out := make([]Process, len(pids))
	for i, pid := range pids {
		out[i] = Process{PID: pid, Command: ps[pid].comm}
	}

	return out
}

func descendants(ps map[int]proc, root int) []int {
	children := map[int][]int{}
	for pid, p := range ps {
		children[p.ppid] = append(children[p.ppid], pid)
	}
	var out []int
	queue := []int{root}
	for len(queue) > 0 {
		kids := children[queue[0]]
		queue = append(queue[1:], kids...)
		out = append(out, kids...)
	}
	slices.Sort(out)

	return out
}

// label dedupes entries by pid and port and adds the command name.
func label(es []entry) ([]Socket, error) {
	if len(es) == 0 {
		return nil, nil
	}
	ps, err := procs()
	if err != nil {
		return nil, err
	}
	var out []Socket
	for _, e := range es {
		s := Socket{Port: e.port, PID: e.pid, Command: ps[e.pid].comm}
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b Socket) int { return cmp.Or(cmp.Compare(a.Port, b.Port), cmp.Compare(a.PID, b.PID)) })

	return out, nil
}

func procs() (map[int]proc, error) {
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=,comm=").Output()
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}

	return parsePS(string(out))
}

// parsePS reads `ps -o pid=,ppid=,comm=`. macOS prints comm as a full path.
func parsePS(out string) (map[int]proc, error) {
	ps := map[int]proc{}
	for line := range strings.Lines(out) {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			return nil, fmt.Errorf("ps pid %q: %w", f[0], err)
		}
		ppid, err := strconv.Atoi(f[1])
		if err != nil {
			return nil, fmt.Errorf("ps ppid %q: %w", f[1], err)
		}
		comm := strings.Join(f[2:], " ")
		if strings.HasPrefix(comm, "/") {
			comm = filepath.Base(comm)
		}
		ps[pid] = proc{ppid: ppid, comm: comm}
	}

	return ps, nil
}
