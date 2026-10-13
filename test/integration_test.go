//go:build integration

package test

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	deadline = 20 * time.Second
	retry    = 200 * time.Millisecond
	page     = "hello from hh"
)

func free(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	return l.Addr().(*net.TCPAddr).Port
}

func build(t *testing.T, dir string) string {
	t.Helper()
	bin := filepath.Join(dir, "hh")
	out, err := exec.Command("go", "build", "-o", bin, "../cmd/hh").CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	return bin
}

func stop(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Logf("interrupt %s: %v", cmd.Path, err)
	}
	if err := cmd.Wait(); err != nil {
		t.Logf("wait %s: %v", cmd.Path, err)
	}
}

func TestProxy(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed")
	}
	// Unix socket paths are capped near 104 bytes, so keep HOME short.
	h, err := os.MkdirTemp("", "hh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(h) })
	bin := build(t, h)
	https, http80 := free(t), free(t)
	env := append(os.Environ(), "HOME="+h,
		fmt.Sprintf("HH_HTTPS_PORT=%d", https), fmt.Sprintf("HH_HTTP_PORT=%d", http80))

	daemon := exec.Command(bin, "daemon")
	daemon.Env = env
	if err := daemon.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop(t, daemon)
		if log, err := os.ReadFile(filepath.Join(h, ".hh", "daemon.log")); err == nil && t.Failed() {
			t.Logf("daemon log:\n%s", log)
		}
	})

	site := filepath.Join(h, "site")
	if err := os.Mkdir(site, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "index.html"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	app := exec.Command(bin, "python3", "-m", "http.server")
	app.Dir, app.Env, app.Stderr = site, env, os.Stderr
	stdout, err := app.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })

	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	go io.Copy(io.Discard, stdout)
	want := fmt.Sprintf("→ https://site.localhost:%d\n", https)
	if line != want {
		t.Fatalf("got %q, want %q", line, want)
	}

	c := trusted(t, h)
	for _, host := range []string{"site.localhost", "tenant.site.localhost"} {
		body := fetch(t, c, fmt.Sprintf("https://%s:%d/", host, https), http.StatusOK)
		if !strings.Contains(body, page) {
			t.Fatalf("%s: body %q", host, body)
		}
	}
	body := fetch(t, c, fmt.Sprintf("https://other.localhost:%d/", https), http.StatusServiceUnavailable)
	if !strings.Contains(body, "site.localhost") {
		t.Fatalf("503 page does not list live routes: %q", body)
	}
}

// trusted returns a client that trusts only the daemon root and dials loopback.
func trusted(t *testing.T, h string) *http.Client {
	t.Helper()
	pem, err := os.ReadFile(filepath.Join(h, ".hh", "caddy", "pki", "authorities", "local", "root.crt"))
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		t.Fatal("bad root pem")
	}
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		_, p, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		var d net.Dialer

		return d.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", p))
	}

	return &http.Client{Timeout: deadline, Transport: &http.Transport{
		DialContext:     dial,
		TLSClientConfig: &tls.Config{RootCAs: pool},
	}}
}

// fetch retries until the status matches, since the app may still be binding.
func fetch(t *testing.T, c *http.Client, url string, status int) string {
	t.Helper()
	end := time.Now().Add(deadline)
	for {
		res, err := c.Get(url)
		if err == nil {
			b, rerr := io.ReadAll(res.Body)
			res.Body.Close()
			if rerr == nil && res.StatusCode == status {
				return string(b)
			}
			err = fmt.Errorf("status %d, read err %v", res.StatusCode, rerr)
		}
		if time.Now().After(end) {
			t.Fatalf("GET %s: %v", url, err)
		}
		time.Sleep(retry)
	}
}
