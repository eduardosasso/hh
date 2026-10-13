// Package port picks a stable local port for a host.
package port

import (
	"errors"
	"hash/fnv"
	"net"
	"strconv"
	"time"
)

const (
	// Min and Max bound the managed range, inclusive.
	Min  = 40000
	Max  = 49999
	span = Max - Min + 1

	probe = 100 * time.Millisecond
)

// Hash maps host to a fixed port in [Min, Max].
func Hash(host string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(host)) // hash.Hash never returns an error

	return Min + int(h.Sum32()%span)
}

// Pick returns Hash(host), or the next port in range that is free and not taken.
func Pick(host string, taken map[int]bool) (int, error) {
	return pick(Hash(host), taken, Free)
}

func pick(start int, taken map[int]bool, free func(int) bool) (int, error) {
	for i := range span {
		p := Min + (start-Min+i)%span
		if !taken[p] && free(p) {
			return p, nil
		}
	}

	return 0, errors.New("no free port in " + strconv.Itoa(Min) + "-" + strconv.Itoa(Max))
}

// Free reports whether nothing listens on p over IPv4 or IPv6 loopback.
func Free(p int) bool {
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p)))
	if err != nil {
		return false
	}
	// A probe that cannot close cleanly counts as busy.
	if err := l.Close(); err != nil {
		return false
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort("::1", strconv.Itoa(p)), probe)
	if err != nil {
		return true
	}
	_ = c.Close() // probe connection; nothing to flush

	return false
}
