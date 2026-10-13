package name

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func newRepo(t *testing.T, folder string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), folder)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "init", "-q", "-b", "main")
	run(t, dir, "commit", "-q", "--allow-empty", "-m", "init")

	return dir
}

func host(t *testing.T, dir string, o Options) string {
	t.Helper()
	h, err := Host(dir, o)
	if err != nil {
		t.Fatal(err)
	}

	return h
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Quip":                "quip",
		"fix/recap-unread":    "fix-recap-unread",
		"--My__App  v2--":     "my-app-v2",
		"wt-173":              "wt-173",
		"feature/ÜBER/thing!": "feature-ber-thing",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMainCheckout(t *testing.T) {
	dir := newRepo(t, "Quip")
	if got := host(t, dir, Options{}); got != "quip.localhost" {
		t.Fatalf("got %q", got)
	}
	run(t, dir, "switch", "-q", "-c", "feat/auth")
	if got := host(t, dir, Options{}); got != "quip.localhost" {
		t.Fatalf("branch switch changed host: %q", got)
	}
	sub := filepath.Join(dir, "web", "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := host(t, sub, Options{}); got != "quip.localhost" {
		t.Fatalf("subfolder: %q", got)
	}
}

func TestWorktree(t *testing.T) {
	dir := newRepo(t, "overdew")
	wt := filepath.Join(filepath.Dir(dir), "overdew-wt-173")
	run(t, dir, "worktree", "add", "-q", "-b", "fix/recap-unread", wt)
	if got := host(t, wt, Options{}); got != "fix-recap-unread.overdew.localhost" {
		t.Fatalf("got %q", got)
	}
	if got := host(t, dir, Options{}); got != "overdew.localhost" {
		t.Fatalf("main checkout: %q", got)
	}
}

func TestLabel(t *testing.T) {
	dir := newRepo(t, "quip")
	if got := host(t, dir, Options{Label: "API"}); got != "api.quip.localhost" {
		t.Fatalf("got %q", got)
	}
}

func TestNoGit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "My Site")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := host(t, dir, Options{}); got != "my-site.localhost" {
		t.Fatalf("got %q", got)
	}
}

func TestOverride(t *testing.T) {
	dir := newRepo(t, "quip")
	cases := map[string]string{
		"blog":           "blog.localhost",
		"Admin.Quip":     "admin.quip.localhost",
		"blog.localhost": "blog.localhost",
	}
	for in, want := range cases {
		if got := host(t, dir, Options{Override: in}); got != want {
			t.Errorf("Override %q = %q, want %q", in, got, want)
		}
	}
	if got := host(t, dir, Options{Override: "blog", Label: "api"}); got != "api.blog.localhost" {
		t.Errorf("override with label: %q", got)
	}
	if _, err := Host(dir, Options{Override: "a..b"}); err == nil {
		t.Error("empty part accepted")
	}
}
