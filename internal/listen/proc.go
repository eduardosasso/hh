package listen

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// listenState is TCP_LISTEN in /proc/net/tcp.
const listenState = "0A"

// socket is a listening row of /proc/net/tcp before its inode maps to a pid.
type socket struct {
	inode string
	entry
}

// parseNet reads /proc/net/tcp or /proc/net/tcp6 and keeps listening rows.
func parseNet(out string) ([]socket, error) {
	var ss []socket
	for line := range strings.Lines(out) {
		f := strings.Fields(line)
		if len(f) < 10 || f[3] != listenState {
			continue
		}
		hostHex, portHex, ok := strings.Cut(f[1], ":")
		if !ok {
			return nil, fmt.Errorf("proc address %q", f[1])
		}
		port, err := strconv.ParseUint(portHex, 16, 16)
		if err != nil {
			return nil, fmt.Errorf("proc port %q: %w", portHex, err)
		}
		ip, err := procIP(hostHex)
		if err != nil {
			return nil, err
		}
		ss = append(ss, socket{inode: f[9], entry: entry{port: int(port), ip: ip}})
	}

	return ss, nil
}

// procIP decodes the kernel's hex address: 32-bit words in host (little-endian) order.
func procIP(h string) (net.IP, error) {
	b, err := hex.DecodeString(h)
	if err != nil || (len(b) != net.IPv4len && len(b) != net.IPv6len) {
		return nil, fmt.Errorf("proc ip %q", h)
	}
	ip := make(net.IP, len(b))
	for i := 0; i < len(b); i += 4 {
		binary.BigEndian.PutUint32(ip[i:], binary.LittleEndian.Uint32(b[i:]))
	}

	return ip, nil
}
