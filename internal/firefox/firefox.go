// Package firefox makes Firefox trust the OS root store, which it ignores by default.
package firefox

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
)

const (
	pref     = "security.enterprise_roots.enabled"
	line     = `user_pref("` + pref + `", true);`
	prefs    = "prefs.js"
	userJS   = "user.js"
	fileMode = 0o644
)

// enabled matches a user.js line that sets the pref to true.
var enabled = regexp.MustCompile(`^\s*user_pref\(\s*"` + regexp.QuoteMeta(pref) + `"\s*,\s*true\s*\)\s*;`)

// running reports whether a Firefox process exists. Tests replace it.
var running = func() (bool, error) {
	err := exec.Command("pgrep", "-x", "firefox").Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}

	return err == nil, err
}

// root is the folder that holds the Firefox profiles.
func root() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(h, "Library", "Application Support", "Firefox", "Profiles"), nil
	}

	return filepath.Join(h, ".mozilla", "firefox"), nil
}

// profiles lists the profile directories: those with a prefs.js.
func profiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	found := []string{}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if _, err := os.Stat(filepath.Join(p, prefs)); err == nil {
			found = append(found, p)
		}
	}

	return found, nil
}

// has reports whether user.js already enables the pref.
func has(b []byte) bool {
	s := bufio.NewScanner(bytes.NewReader(b))
	for s.Scan() {
		if enabled.MatchString(s.Text()) {
			return true
		}
	}

	return false
}

// add puts the pref in a profile's user.js and reports whether it changed the file.
func add(profile string) (bool, error) {
	path := filepath.Join(profile, userJS)
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	if has(b) {
		return false, nil
	}
	if len(b) > 0 && b[len(b)-1] != '\n' {
		b = append(b, '\n')
	}
	b = append(b, line+"\n"...)
	if err := os.WriteFile(path, b, fileMode); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}

	return true, nil
}

// Enable sets the enterprise roots pref in every Firefox profile.
func Enable(out io.Writer) error {
	dir, err := root()
	if err != nil {
		return err
	}
	found, err := profiles(dir)
	if errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(out, "firefox: not installed, skipped")

		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w; set %s to true in about:config by hand", dir, err, pref)
	}
	changed := false
	for _, p := range found {
		did, err := add(p)
		if err != nil {
			return err
		}
		changed = changed || did
		if did {
			fmt.Fprintf(out, "firefox: %s now trusts OS roots\n", filepath.Base(p))

			continue
		}
		fmt.Fprintf(out, "firefox: %s already trusts OS roots\n", filepath.Base(p))
	}
	if !changed {
		return nil
	}
	up, err := running()
	if err != nil {
		return fmt.Errorf("check for a running Firefox: %w", err)
	}
	if up {
		fmt.Fprintln(out, "firefox: restart Firefox to apply")
	}

	return nil
}
