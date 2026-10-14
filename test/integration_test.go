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
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	deadline = 20 * time.Second
	retry    = 200 * time.Millisecond
	page     = "hello from hh"
	sweep    = 7 * time.Second

	orphanWait = 3 * time.Second
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

// boot builds hh and starts a daemon on free ports under a temp HOME.
func boot(t *testing.T) (bin string, env []string, h string, https int) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed")
	}
	// Unix socket paths are capped near 104 bytes, so keep HOME short.
	h, err := os.MkdirTemp("", "hh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(h) })
	bin = build(t, h)
	https, http80 := free(t), free(t)
	// A PORT from the shell running the tests would pin every app to one port.
	env = append(slices.DeleteFunc(os.Environ(), func(v string) bool { return strings.HasPrefix(v, "PORT=") }), "HOME="+h,
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

	return bin, env, h, https
}

func TestProxy(t *testing.T) {
	bin, env, h, https := boot(t)
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
	t.Cleanup(func() {
		if app.ProcessState == nil {
			stop(t, app)
		}
	})

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

	ls := func() string {
		cmd := exec.Command(bin, "ls")
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("hh ls: %v\n%s", err, out)
		}

		return string(out)
	}
	if out := ls(); !strings.Contains(out, "site.localhost") {
		t.Fatalf("hh ls: %s", out)
	}
	// A wrapper killed with SIGKILL cannot deregister; the sweep must drop it.
	// Its python child survives, so stop that one by pid. Match the name: hh also runs ps.
	child, err := exec.Command("pgrep", "-i", "-P", strconv.Itoa(app.Process.Pid), "python").Output()
	if err != nil {
		t.Fatalf("find python child: %v", err)
	}
	t.Cleanup(func() {
		if err := exec.Command("kill", strings.Fields(string(child))...).Run(); err != nil {
			t.Logf("kill python: %v", err)
		}
	})
	if err := app.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	// Reap it as a shell would; a zombie still answers kill(pid, 0).
	if err := app.Wait(); err == nil {
		t.Fatal("wrapper exited cleanly after SIGKILL")
	}
	end := time.Now().Add(sweep)
	for strings.Contains(ls(), "site.localhost") {
		if time.Now().After(end) {
			t.Fatalf("route still listed %s after SIGKILL", sweep)
		}
		time.Sleep(retry)
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

// held is a port no hh hash lands on, so the app visibly ignores PORT.
const held = 38123

// launch starts hh with args in dir and streams its stdout lines.
func launch(t *testing.T, bin string, env []string, dir string, args ...string) (*exec.Cmd, <-chan string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir, cmd.Env, cmd.Stderr = dir, env, os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			stop(t, cmd)
		}
	})
	lines := make(chan string)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()

	return cmd, lines
}

// expect waits for a stdout line containing want.
func expect(t *testing.T, lines <-chan string, want string) string {
	t.Helper()

	return expectWithin(t, lines, want, deadline)
}

// expectWithin waits up to wait for a stdout line containing want and returns it.
func expectWithin(t *testing.T, lines <-chan string, want string, wait time.Duration) string {
	t.Helper()
	timer := time.After(wait)
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatalf("stdout closed before %q", want)
			}
			if strings.Contains(l, want) {
				go func() {
					for range lines {
					}
				}()

				return l
			}
		case <-timer:
			t.Fatalf("no line with %q after %s", want, wait)
		}
	}
}

func TestDiscover(t *testing.T) {
	bin, env, h, https := boot(t)
	site := filepath.Join(h, "disc")
	if err := os.Mkdir(site, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "index.html"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	moved := fmt.Sprintf("(app listens on %d, ignoring PORT=", held)
	serve := func(name string) []string {
		return []string{"--name", name, "python3", "-m", "http.server", strconv.Itoa(held)}
	}

	// disc2 runs alone once, so hh learns its usual port.
	learn, lines := launch(t, bin, env, site, serve("disc2")...)
	expect(t, lines, moved)
	stop(t, learn)
	ports, err := os.ReadFile(filepath.Join(h, ".hh", "ports.json"))
	if err != nil || !strings.Contains(string(ports), fmt.Sprintf(`"disc2.localhost": %d`, held)) {
		t.Fatalf("ports.json: %s, %v", ports, err)
	}

	_, lines = launch(t, bin, env, site, serve("disc")...)
	expect(t, lines, moved)
	body := fetch(t, trusted(t, h), fmt.Sprintf("https://disc.localhost:%d/", https), http.StatusOK)
	if !strings.Contains(body, page) {
		t.Fatalf("body %q", body)
	}
	who := exec.Command(bin, "who", strconv.Itoa(held))
	who.Env = env
	out, err := who.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "disc.localhost") {
		t.Fatalf("hh who: %v\n%s", err, out)
	}

	// disc2 again: its usual port is now held by disc's python.
	var stderr strings.Builder
	second := exec.Command(bin, serve("disc2")...)
	second.Dir, second.Env, second.Stderr = site, env, &stderr
	err = second.Run()
	if err == nil {
		t.Fatal("second app exited 0 on a held port")
	}
	msg := strings.ToLower(stderr.String())
	if !strings.Contains(msg, "held by python") || !strings.Contains(msg, "running disc.localhost") {
		t.Fatalf("stderr:\n%s", stderr.String())
	}
}

func TestOrphans(t *testing.T) {
	bin, env, h, _ := boot(t)
	pidFile := filepath.Join(h, "sleep.pid")
	// The child starts sleep in its own session, lives long enough to be seen, then exits.
	script := fmt.Sprintf(`import subprocess, time
p = subprocess.Popen(["sleep", "300"], start_new_session=True)
open(%q, "w").write(str(p.pid))
time.sleep(1)`, pidFile)
	var stderr strings.Builder
	app := exec.Command(bin, "--name", "orph", "python3", "-c", script)
	app.Dir, app.Env, app.Stderr = h, env, &stderr
	if err := app.Run(); err != nil {
		t.Fatalf("hh: %v\n%s", err, stderr.String())
	}
	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(b))
	if err != nil {
		t.Fatal(err)
	}
	end := time.Now().Add(orphanWait)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(end) {
			if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
				t.Logf("kill sleep: %v", err)
			}
			t.Fatalf("sleep %d alive %s after hh exited", pid, orphanWait)
		}
		time.Sleep(retry)
	}
	if !strings.Contains(stderr.String(), "stopped 1 leftover process (sleep)") {
		t.Fatalf("stderr:\n%s", stderr.String())
	}
}

const (
	// internalPort is a helper port the dev script opens before the app.
	internalPort = 38200
	// pinned is a PORT the caller sets on purpose.
	pinned = 38300
	// settled is how soon the route must reach PORT once the app listens.
	settled = 5 * time.Second
)

// pages writes an index.html with body into a new folder under dir.
func pages(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, body)
	if err := os.Mkdir(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "index.html"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	return p
}

func TestDiscoverLaterPort(t *testing.T) {
	bin, env, h, https := boot(t)
	first, second := pages(t, h, "first-server"), pages(t, h, "second-server")
	script := filepath.Join(h, "dev.sh")
	body := fmt.Sprintf(`#!/bin/sh
python3 -m http.server %d --directory %q &
sleep 2
exec python3 -m http.server "$PORT" --directory %q
`, internalPort, first, second)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	_, lines := launch(t, bin, env, h, "--name", "later", script)
	expect(t, lines, fmt.Sprintf("(app listens on %d, ignoring PORT=", internalPort))
	line := expectWithin(t, lines, "(app now listens on PORT=", settled)
	if !strings.HasPrefix(line, "→ https://later.localhost") {
		t.Fatalf("line %q", line)
	}
	got := fetch(t, trusted(t, h), fmt.Sprintf("https://later.localhost:%d/", https), http.StatusOK)
	if !strings.Contains(got, "second-server") {
		t.Fatalf("body %q", got)
	}
}

func TestEnvPort(t *testing.T) {
	bin, env, h, https := boot(t)
	site := pages(t, h, "pinned-server")
	env = append(env, fmt.Sprintf("PORT=%d", pinned))

	_, lines := launch(t, bin, env, site, "--name", "pinned", "sh", "-c", `exec python3 -m http.server "$PORT"`)
	expect(t, lines, "→ https://pinned.localhost")
	got := fetch(t, trusted(t, h), fmt.Sprintf("https://pinned.localhost:%d/", https), http.StatusOK)
	if !strings.Contains(got, "pinned-server") {
		t.Fatalf("body %q", got)
	}
	ls := exec.Command(bin, "ls")
	ls.Env = env
	out, err := ls.CombinedOutput()
	if err != nil || !strings.Contains(string(out), strconv.Itoa(pinned)) {
		t.Fatalf("hh ls: %v\n%s", err, out)
	}
}
