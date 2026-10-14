package listen

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// parseLsof reads `lsof -F pn` output: a p line per process, then n lines per socket.
func parseLsof(out string) ([]entry, error) {
	var es []entry
	pid := 0
	for line := range strings.Lines(out) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		v := line[1:]
		switch line[0] {
		case 'p':
			p, err := strconv.Atoi(v)
			if err != nil {
				return nil, fmt.Errorf("lsof pid %q: %w", v, err)
			}
			pid = p
		case 'n':
			e, err := address(v)
			if err != nil {
				return nil, err
			}
			e.pid = pid
			es = append(es, e)
		}
	}

	return es, nil
}

// address reads lsof names like 127.0.0.1:8000, *:8000 and [::1]:8000.
func address(name string) (entry, error) {
	host, p, err := net.SplitHostPort(name)
	if err != nil {
		return entry{}, fmt.Errorf("lsof address %q: %w", name, err)
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		return entry{}, fmt.Errorf("lsof port %q: %w", name, err)
	}
	if host == "*" {
		return entry{port: port}, nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return entry{}, fmt.Errorf("lsof host %q", name)
	}

	return entry{port: port, ip: ip}, nil
}

// parseCwd reads `lsof -d cwd -F n` output.
func parseCwd(out string) (string, error) {
	for line := range strings.Lines(out) {
		if strings.HasPrefix(line, "n") {
			return strings.TrimSpace(line[1:]), nil
		}
	}

	return "", fmt.Errorf("no cwd in lsof output %q", out)
}
