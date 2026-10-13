// Package project bakes the hh prefix into a project's dev script and agent rules.
package project

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/eduardosasso/hh/internal/setup"
)

const prefix = "hh "

// Outcomes printed per file.
const (
	Updated   = "updated"
	Created   = "created"
	Unchanged = "unchanged"
	Skipped   = "skipped"
)

const makeHint = "Makefile: prefix the dev recipe command with `hh` yourself"

var (
	scriptsKey = regexp.MustCompile(`"scripts"\s*:\s*\{`)
	devKey     = regexp.MustCompile(`"dev"\s*:\s*"`)
	webLine    = regexp.MustCompile(`(?m)^web:[ \t]*`)
	devTarget  = regexp.MustCompile(`(?m)^dev:`)
)

// Init prefixes the dev command in dir and writes the agent rule, reporting one line per file.
func Init(dir string, out io.Writer) error {
	steps := []struct {
		file string
		fn   func(string) (string, error)
	}{
		{"package.json", Package},
		{"Procfile", Procfile},
		{"AGENTS.md", Agents},
	}
	for _, s := range steps {
		status, err := s.fn(filepath.Join(dir, s.file))
		if err != nil {
			return err
		}
		if status != "" {
			fmt.Fprintf(out, "%s %s\n", status, s.file)
		}
	}
	hint, err := Makefile(filepath.Join(dir, "Makefile"))
	if err != nil {
		return err
	}
	if hint {
		fmt.Fprintln(out, makeHint)
	}

	return nil
}

// edit reads path, applies fn, and writes back when the text changed.
// A missing file returns an empty status.
func edit(path string, fn func(string) (string, string)) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	next, status := fn(string(data))
	if next == string(data) {
		return status, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(next), info.Mode().Perm()); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}

	return status, nil
}

// Package prefixes scripts.dev in the package.json at path, keeping every other byte.
func Package(path string) (string, error) {
	return edit(path, func(text string) (string, string) {
		at := valueStart(text)
		if at < 0 {
			return text, Skipped + " (no scripts.dev)"
		}
		if strings.HasPrefix(text[at:], prefix) {
			return text, Unchanged
		}

		return text[:at] + prefix + text[at:], Updated
	})
}

// valueStart returns the index just after the opening quote of scripts.dev, or -1.
func valueStart(text string) int {
	loc := scriptsKey.FindStringIndex(text)
	if loc == nil {
		return -1
	}
	body := text[loc[1]:]
	end := closing(body)
	m := devKey.FindStringIndex(body[:end])
	if m == nil {
		return -1
	}

	return loc[1] + m[1]
}

// closing returns the index of the brace that ends an object whose opening brace was just consumed.
func closing(body string) int {
	inString := false
	for i := 0; i < len(body); i++ {
		switch c := body[i]; {
		case inString && c == '\\':
			i++
		case c == '"':
			inString = !inString
		case !inString && c == '}':
			return i
		}
	}

	return len(body)
}

// Procfile prefixes the web process command in the Procfile at path.
func Procfile(path string) (string, error) {
	return edit(path, func(text string) (string, string) {
		loc := webLine.FindStringIndex(text)
		if loc == nil {
			return text, Skipped + " (no web process)"
		}
		if strings.HasPrefix(text[loc[1]:], prefix) {
			return text, Unchanged
		}

		return text[:loc[1]] + prefix + text[loc[1]:], Updated
	})
}

// Makefile reports whether the Makefile at path has a dev target. It never edits the file.
func Makefile(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	return devTarget.Match(data), nil
}

// Agents appends the agent rule to the AGENTS.md at path, creating the file when missing.
func Agents(path string) (string, error) {
	status, err := edit(path, func(text string) (string, string) {
		if strings.Contains(text, setup.Rule) {
			return text, Unchanged
		}
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		if text != "" {
			text += "\n"
		}

		return text + setup.Rule + "\n", Updated
	})
	if err != nil || status != "" {
		return status, err
	}
	if err := os.WriteFile(path, []byte(setup.Rule+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}

	return Created, nil
}
