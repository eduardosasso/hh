package firefox

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func profilesDir(t *testing.T, home string) string {
	t.Helper()
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "Firefox", "Profiles")
	}

	return filepath.Join(home, ".mozilla", "firefox")
}

func profile(t *testing.T, home, name string) string {
	t.Helper()
	p := filepath.Join(profilesDir(t, home), name)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "prefs.js"), []byte("// prefs\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	return p
}

func write(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(p, "user.js"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(p, "user.js"))
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

func run(t *testing.T, home string, up bool) string {
	t.Helper()
	t.Setenv("HOME", home)
	running = func() (bool, error) { return up, nil }
	var out bytes.Buffer
	if err := Enable(&out); err != nil {
		t.Fatal(err)
	}

	return out.String()
}

func TestNotInstalled(t *testing.T) {
	out := run(t, t.TempDir(), true)
	if out != "firefox: not installed, skipped\n" {
		t.Fatalf("got %q", out)
	}
}

func TestCreatesUserJS(t *testing.T) {
	home := t.TempDir()
	p := profile(t, home, "abc.default")
	out := run(t, home, false)
	if read(t, p) != line+"\n" {
		t.Fatalf("user.js %q", read(t, p))
	}
	if out != "firefox: abc.default now trusts OS roots\n" {
		t.Fatalf("got %q", out)
	}
}

func TestAppendsAfterMissingNewline(t *testing.T) {
	home := t.TempDir()
	p := profile(t, home, "abc.default")
	write(t, p, `user_pref("a", 1);`)
	run(t, home, false)
	if want := `user_pref("a", 1);` + "\n" + line + "\n"; read(t, p) != want {
		t.Fatalf("user.js %q", read(t, p))
	}
}

func TestAlreadyEnabled(t *testing.T) {
	home := t.TempDir()
	p := profile(t, home, "abc.default")
	content := "user_pref(\"a\", 1);\nuser_pref(\"security.enterprise_roots.enabled\",true);"
	write(t, p, content)
	out := run(t, home, true)
	if read(t, p) != content {
		t.Fatalf("user.js changed: %q", read(t, p))
	}
	if out != "firefox: abc.default already trusts OS roots\n" {
		t.Fatalf("got %q", out)
	}
}

func TestTwoProfiles(t *testing.T) {
	home := t.TempDir()
	a := profile(t, home, "a.default")
	b := profile(t, home, "b.dev")
	write(t, b, line+"\n")
	out := run(t, home, true)
	if read(t, a) != line+"\n" || read(t, b) != line+"\n" {
		t.Fatalf("a %q b %q", read(t, a), read(t, b))
	}
	for _, want := range []string{"a.default now trusts", "b.dev already trusts", "restart Firefox to apply"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

func TestIgnoresDirWithoutPrefs(t *testing.T) {
	home := t.TempDir()
	stray := filepath.Join(profilesDir(t, home), "Crash Reports")
	if err := os.MkdirAll(stray, 0o755); err != nil {
		t.Fatal(err)
	}
	out := run(t, home, true)
	if out != "" {
		t.Fatalf("got %q", out)
	}
	if _, err := os.Stat(filepath.Join(stray, "user.js")); err == nil {
		t.Fatal("user.js created in stray dir")
	}
}

func TestNoRestartNoteWhenUnchanged(t *testing.T) {
	home := t.TempDir()
	p := profile(t, home, "abc.default")
	write(t, p, line+"\n")
	if out := run(t, home, true); strings.Contains(out, "restart") {
		t.Fatalf("got %q", out)
	}
}
