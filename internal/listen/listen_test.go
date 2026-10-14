package listen

import (
	"net"
	"os"
	"os/exec"
	"slices"
	"testing"
)

func TestListening(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	want := l.Addr().(*net.TCPAddr).Port

	ss, err := Listening([]int{os.Getpid()})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(ss, func(s Socket) bool { return s.Port == want && s.PID == os.Getpid() && s.Command != "" }) {
		t.Fatalf("port %d not in %+v", want, ss)
	}
}

func TestDescendants(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cmd.Process.Kill(); err != nil {
			t.Logf("kill sleep: %v", err)
		}
		_ = cmd.Wait() // killed on purpose; the error is expected
	})

	pids, err := Descendants(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(pids, cmd.Process.Pid) {
		t.Fatalf("sleep %d not in %v", cmd.Process.Pid, pids)
	}
	if slices.Contains(pids, os.Getpid()) {
		t.Fatal("root listed as its own descendant")
	}
}

func TestCwd(t *testing.T) {
	got, err := Cwd(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got != wd {
		t.Fatalf("got %q, want %q", got, wd)
	}
}

func TestParsePS(t *testing.T) {
	ps, err := parsePS("  1     0 /sbin/launchd\n 42     1 /Applications/Google Chrome.app/Contents/MacOS/Google Chrome\n 43 42 node\n")
	if err != nil {
		t.Fatal(err)
	}
	if ps[1].comm != "launchd" || ps[42].comm != "Google Chrome" || ps[43].comm != "node" || ps[43].ppid != 42 {
		t.Fatalf("got %+v", ps)
	}
	if got := descendants(ps, 1); !slices.Equal(got, []int{42, 43}) {
		t.Fatalf("descendants %v", got)
	}
}

func TestParseLsof(t *testing.T) {
	es, err := parseLsof("p10\nf4\nn*:8000\nf5\nn[::1]:8001\np11\nf3\nn192.168.1.5:9000\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 3 || es[0].pid != 10 || es[0].port != 8000 || es[1].port != 8001 || es[2].pid != 11 {
		t.Fatalf("got %+v", es)
	}
	reach := []bool{es[0].reachable(), es[1].reachable(), es[2].reachable()}
	if !slices.Equal(reach, []bool{true, true, false}) {
		t.Fatalf("reachable %v", reach)
	}
	dir, err := parseCwd("p10\nfcwd\nn/Users/me/code\n")
	if err != nil || dir != "/Users/me/code" {
		t.Fatalf("cwd %q, %v", dir, err)
	}
}

func TestParseNet(t *testing.T) {
	tcp := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 111 1 0000000000000000 100 0 0 10 0
   1: 0100007F:1F91 0100007F:9C40 01 00000000:00000000 00:00000000 00000000  1000        0 112 1 0000000000000000 100 0 0 10 0
`
	ss, err := parseNet(tcp)
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 1 || ss[0].port != 8080 || ss[0].inode != "111" || !ss[0].ip.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatalf("got %+v", ss)
	}
	tcp6 := `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000000000000000000001000000:0BB8 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 222 1 0000000000000000 100 0 0 10 0
`
	ss, err = parseNet(tcp6)
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 1 || ss[0].port != 3000 || !ss[0].ip.Equal(net.IPv6loopback) {
		t.Fatalf("got %+v", ss)
	}
}
