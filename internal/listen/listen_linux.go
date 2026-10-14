package listen

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const procRoot = "/proc"

var netTables = []string{"net/tcp", "net/tcp6"}

func scan(pids []int) ([]entry, error) {
	var ss []socket
	for _, t := range netTables {
		b, err := os.ReadFile(filepath.Join(procRoot, t))
		if errors.Is(err, fs.ErrNotExist) {
			continue // no IPv6 on this kernel
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", t, err)
		}
		rows, err := parseNet(string(b))
		if err != nil {
			return nil, err
		}
		ss = append(ss, rows...)
	}
	if len(pids) == 0 {
		all, err := everyPID()
		if err != nil {
			return nil, err
		}
		pids = all
	}
	owners, err := inodes(pids)
	if err != nil {
		return nil, err
	}
	var es []entry
	for _, s := range ss {
		if pid, ok := owners[s.inode]; ok {
			s.pid = pid
			es = append(es, s.entry)
		}
	}

	return es, nil
}

func everyPID() ([]int, error) {
	ds, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", procRoot, err)
	}
	var pids []int
	for _, d := range ds {
		if pid, err := strconv.Atoi(d.Name()); err == nil {
			pids = append(pids, pid)
		}
	}

	return pids, nil
}

// inodes maps socket inodes to the pid holding them. Processes that exit or
// belong to another user are skipped: their fds are not ours to read.
func inodes(pids []int) (map[string]int, error) {
	out := map[string]int{}
	for _, pid := range pids {
		dir := filepath.Join(procRoot, strconv.Itoa(pid), "fd")
		fds, err := os.ReadDir(dir)
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", dir, err)
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(dir, fd.Name()))
			if err != nil {
				continue // fd closed between ReadDir and Readlink
			}
			if inode, ok := strings.CutPrefix(link, "socket:["); ok {
				out[strings.TrimSuffix(inode, "]")] = pid
			}
		}
	}

	return out, nil
}

func cwd(pid int) (string, error) {
	p, err := os.Readlink(filepath.Join(procRoot, strconv.Itoa(pid), "cwd"))
	if err != nil {
		return "", fmt.Errorf("cwd of pid %d: %w", pid, err)
	}

	return p, nil
}
