// Package name derives the hostname for a folder.
package name

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// TLD is the suffix every host ends with.
const TLD = "localhost"

var invalid = regexp.MustCompile(`[^a-z0-9]+`)

// Slug lowercases s and turns every run of characters outside [a-z0-9] into "-".
func Slug(s string) string {
	return strings.Trim(invalid.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// Options change how Host derives a name.
type Options struct {
	Label    string
	Override string
}

// Host derives the host for dir: <label>.<branch>.<base>.localhost.
func Host(dir string, o Options) (string, error) {
	base, err := derive(dir, o.Override)
	if err != nil {
		return "", err
	}
	if o.Label == "" {
		return base, nil
	}
	l := Slug(o.Label)
	if l == "" {
		return "", fmt.Errorf("label %q has no usable characters", o.Label)
	}

	return l + "." + base, nil
}

func derive(dir, override string) (string, error) {
	if override != "" {
		return custom(override)
	}
	g, err := inspect(dir)
	if err != nil {
		return "", err
	}
	labels := []string{Slug(filepath.Base(dir))}
	if g != nil {
		labels = g.labels()
	}
	for _, l := range labels {
		if l == "" {
			return "", fmt.Errorf("cannot derive a name from %s; use --name", dir)
		}
	}

	return strings.Join(append(labels, TLD), "."), nil
}

// custom slugs each dot-separated part of a --name value.
func custom(n string) (string, error) {
	parts := strings.Split(strings.TrimSuffix(n, "."+TLD), ".")
	for i, p := range parts {
		parts[i] = Slug(p)
		if parts[i] == "" {
			return "", fmt.Errorf("name %q has an empty part", n)
		}
	}

	return strings.Join(append(parts, TLD), "."), nil
}

type repo struct {
	top    string
	gitDir string
	common string
	branch string
}

func (r *repo) worktree() bool { return r.gitDir != r.common }

func (r *repo) labels() []string {
	if !r.worktree() {
		return []string{Slug(filepath.Base(r.top))}
	}
	branch := r.branch
	if branch == "" {
		branch = filepath.Base(r.top)
	}

	return []string{Slug(branch), Slug(filepath.Base(filepath.Dir(r.common)))}
}

// inspect returns nil when dir is not inside a git work tree.
func inspect(dir string) (*repo, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, nil
	}
	out, err := git(dir, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-dir", "--git-common-dir")
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, nil
		}

		return nil, err
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		return nil, fmt.Errorf("git rev-parse in %s: unexpected output %q", dir, out)
	}
	r := &repo{top: lines[0], gitDir: clean(lines[1]), common: clean(lines[2])}
	if !r.worktree() {
		return r, nil
	}
	// A detached HEAD has no branch; labels() falls back to the folder name.
	if b, err := git(dir, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
		r.branch = b
	}

	return r, nil
}

func clean(p string) string {
	if e, err := filepath.EvalSymlinks(p); err == nil {
		return e
	}

	return filepath.Clean(p)
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(out)), nil
}
