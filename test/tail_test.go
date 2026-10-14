//go:build integration

package test

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	// wsOpen is how long the test upstream keeps a WebSocket open.
	wsOpen = 1500 * time.Millisecond
	secret = "hunter2"
)

// buffer is a goroutine-safe stdout sink.
type buffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (b *buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.b.Write(p)
}

func (b *buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.b.String()
}

// tail runs hh tail until its output holds every want, then stops it with Ctrl-C.
func tail(t *testing.T, bin string, env []string, dir string, want []string, args ...string) string {
	t.Helper()
	var out buffer
	cmd := exec.Command(bin, append([]string{"tail"}, args...)...)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = dir, env, &out, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	has := func() bool {
		for _, w := range want {
			if !strings.Contains(out.String(), w) {
				return false
			}
		}

		return true
	}
	end := time.Now().Add(deadline)
	for !has() && time.Now().Before(end) {
		time.Sleep(retry)
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("hh tail exit: %v", err)
	}
	if !has() {
		t.Fatalf("hh tail %v: want %q in:\n%s", args, want, out.String())
	}
	t.Logf("hh tail %s:\n%s", strings.Join(args, " "), out.String())

	return out.String()
}

func accessLog(t *testing.T, h string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(h, ".hh", "access.log"))
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

func TestTail(t *testing.T) {
	bin, env, h, https := boot(t)
	site := pages(t, h, "tailsite")
	_, lines := launch(t, bin, env, site, "python3", "-m", "http.server")
	expect(t, lines, "→ https://tailsite.localhost")
	c := trusted(t, h)
	base := fmt.Sprintf("https://tailsite.localhost:%d", https)
	fetch(t, c, base+"/", http.StatusOK)
	req, err := http.NewRequest(http.MethodGet, base+"/?with=cookie", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", "session="+secret)
	req.Header.Set("Authorization", "Bearer "+secret)
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	fetch(t, c, base+"/missing", http.StatusNotFound)

	out := tail(t, bin, env, site, []string{"/missing"}, "-n", "5")
	var ok, missing bool
	for _, l := range strings.Split(out, "\n") {
		ok = ok || (strings.Contains(l, " tailsite ") && strings.Contains(l, " 200 "))
		missing = missing || (strings.Contains(l, " tailsite ") && strings.Contains(l, " 404 ") && strings.Contains(l, "/missing"))
	}
	if !ok || !missing {
		t.Fatalf("no 200 and 404 lines for tailsite:\n%s", out)
	}

	var raw string
	for _, l := range strings.Split(accessLog(t, h), "\n") {
		if strings.Contains(l, "with=cookie") {
			raw = l
		}
	}
	if raw == "" {
		t.Fatal("cookie request not in the access log")
	}
	t.Logf("raw access log line:\n%s", raw)
	for _, leak := range []string{"Cookie", "Authorization", secret} {
		if strings.Contains(raw, leak) {
			t.Errorf("access log line holds %q", leak)
		}
	}
	if !strings.Contains(raw, `"app_latency":`) {
		t.Error("access log line has no app_latency")
	}
}

func TestTailProxyError(t *testing.T) {
	bin, env, h, https := boot(t)
	closed := free(t)
	_, lines := launch(t, bin, env, h, "--name", "dead", "--port", strconv.Itoa(closed), "sleep", "60")
	expect(t, lines, "→ https://dead.localhost")
	fetch(t, trusted(t, h), fmt.Sprintf("https://dead.localhost:%d/api/health", https), http.StatusBadGateway)
	tail(t, bin, env, h, []string{fmt.Sprintf("!! nothing listens on %d", closed)}, "dead", "-n", "5")
}

// upgrader answers every request with 101, holds the connection for wsOpen, then closes it.
func upgrader(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)

			return
		}
		defer conn.Close()
		fmt.Fprint(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		if err := rw.Flush(); err != nil {
			t.Errorf("flush: %v", err)
		}
		time.Sleep(wsOpen)
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	return ln.Addr().(*net.TCPAddr).Port
}

func TestTailWebSocket(t *testing.T) {
	bin, env, h, https := boot(t)
	up := upgrader(t)
	_, lines := launch(t, bin, env, h, "--name", "sock", "--port", strconv.Itoa(up), "sleep", "60")
	expect(t, lines, "→ https://sock.localhost")

	cfg := trusted(t, h).Transport.(*http.Transport).TLSClientConfig.Clone()
	cfg.ServerName = "sock.localhost"
	conn, err := tls.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(https)), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET /__hmr HTTP/1.1\r\nHost: sock.localhost:%d\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n", https)
	r := bufio.NewReader(conn)
	status, err := r.ReadString('\n')
	if err != nil || !strings.Contains(status, "101") {
		t.Fatalf("upgrade status %q, %v", status, err)
	}
	// Read until the proxy closes the connection after the upstream does.
	for {
		if _, err := r.ReadByte(); err != nil {
			break
		}
	}
	tail(t, bin, env, h, []string{"/__hmr", "101  ws 1."}, "sock", "-n", "5")
}
