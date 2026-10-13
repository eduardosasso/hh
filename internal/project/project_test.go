package project

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eduardosasso/hh/internal/setup"
)

const pkg = "{\n  \"name\": \"app\",\n  \"scripts\": {\n    \"build\": \"tsc\",\n    \"dev\": \"%s\"\n  },\n  \"devDependencies\": {}\n}\n"

func write(t *testing.T, dir, file, body string) string {
	t.Helper()
	p := filepath.Join(dir, file)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	return p
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

func TestPackage(t *testing.T) {
	cases := []struct{ name, in, status, out string }{
		{"dev script", strings.Replace(pkg, "%s", "vite", 1), Updated, strings.Replace(pkg, "%s", "hh vite", 1)},
		{"already prefixed", strings.Replace(pkg, "%s", "hh vite", 1), Unchanged, strings.Replace(pkg, "%s", "hh vite", 1)},
		{"no dev", "{\n  \"scripts\": {\n    \"build\": \"tsc\"\n  }\n}\n", Skipped + " (no scripts.dev)", "{\n  \"scripts\": {\n    \"build\": \"tsc\"\n  }\n}\n"},
		{"no scripts", "{\n  \"name\": \"x\"\n}\n", Skipped + " (no scripts.dev)", "{\n  \"name\": \"x\"\n}\n"},
		{"dev outside scripts", "{\n  \"dev\": \"x\",\n  \"scripts\": {}\n}\n", Skipped + " (no scripts.dev)", "{\n  \"dev\": \"x\",\n  \"scripts\": {}\n}\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := write(t, t.TempDir(), "package.json", c.in)
			status, err := Package(p)
			if err != nil {
				t.Fatal(err)
			}
			if status != c.status || read(t, p) != c.out {
				t.Fatalf("status %q, file %q", status, read(t, p))
			}
		})
	}
}

func TestProcfile(t *testing.T) {
	cases := []struct{ name, in, status, out string }{
		{"web line", "web: bun run start\nworker: bun w\n", Updated, "web: hh bun run start\nworker: bun w\n"},
		{"already prefixed", "web: hh bun run start\n", Unchanged, "web: hh bun run start\n"},
		{"no web", "worker: bun w\n", Skipped + " (no web process)", "worker: bun w\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := write(t, t.TempDir(), "Procfile", c.in)
			status, err := Procfile(p)
			if err != nil {
				t.Fatal(err)
			}
			if status != c.status || read(t, p) != c.out {
				t.Fatalf("status %q, file %q", status, read(t, p))
			}
		})
	}
}

func TestAgents(t *testing.T) {
	cases := []struct {
		name, in, status, out string
		exists                bool
	}{
		{"created", "", Created, setup.Rule + "\n", false},
		{"appended", "# Rules\n\nBe kind.\n", Updated, "# Rules\n\nBe kind.\n\n" + setup.Rule + "\n", true},
		{"appended no newline", "# Rules", Updated, "# Rules\n\n" + setup.Rule + "\n", true},
		{"already has rule", "# Rules\n\n" + setup.Rule + "\n", Unchanged, "# Rules\n\n" + setup.Rule + "\n", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "AGENTS.md")
			if c.exists {
				write(t, dir, "AGENTS.md", c.in)
			}
			status, err := Agents(p)
			if err != nil {
				t.Fatal(err)
			}
			if status != c.status || read(t, p) != c.out {
				t.Fatalf("status %q, file %q", status, read(t, p))
			}
		})
	}
}

func TestInit(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", strings.Replace(pkg, "%s", "vite", 1))
	write(t, dir, "Makefile", "dev:\n\tvite\n")
	var first, second bytes.Buffer
	if err := Init(dir, &first); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"updated package.json", "created AGENTS.md", "Makefile:"} {
		if !strings.Contains(first.String(), want) {
			t.Fatalf("missing %q in %q", want, first.String())
		}
	}
	if err := Init(dir, &second); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(second.String(), "unchanged package.json") || !strings.Contains(second.String(), "unchanged AGENTS.md") {
		t.Fatalf("second run %q", second.String())
	}
	if read(t, filepath.Join(dir, "Makefile")) != "dev:\n\tvite\n" {
		t.Fatal("Makefile edited")
	}
}

func TestInitUnreadable(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "package.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Init(dir, &bytes.Buffer{}); err == nil {
		t.Fatal("want error")
	}
}
