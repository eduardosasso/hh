// Package usual remembers the port an app listens on when it ignores PORT.
package usual

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/eduardosasso/hh/internal/home"
)

const fileMode = 0o600

// Port is the usual port of host, or 0 when none is known.
func Port(host string) (int, error) {
	m, err := read()
	if err != nil {
		return 0, err
	}

	return m[host], nil
}

// Remember records port as the usual port of host.
func Remember(host string, port int) error {
	return change(func(m map[string]int) bool {
		if m[host] == port {
			return false
		}
		m[host] = port

		return true
	})
}

// Forget drops host, for an app that now listens on PORT.
func Forget(host string) error {
	return change(func(m map[string]int) bool {
		if _, ok := m[host]; !ok {
			return false
		}
		delete(m, host)

		return true
	})
}

func read() (map[string]int, error) {
	p, err := home.Ports()
	if err != nil {
		return nil, err
	}

	return load(p)
}

// change applies f and saves only when f reports a change.
func change(f func(map[string]int) bool) error {
	p, err := home.Ports()
	if err != nil {
		return err
	}
	m, err := load(p)
	if err != nil {
		return err
	}
	if !f(m) {
		return nil
	}
	if err := home.Mkdir(); err != nil {
		return err
	}

	return save(p, m)
}

func load(path string) (map[string]int, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]int{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	m := map[string]int{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}

	return m, nil
}

// save writes through a temp file so concurrent readers never see half a file.
func save(path string, m map[string]int) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ports-*.json")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if _, err := tmp.Write(b); err != nil {
		return errors.Join(fmt.Errorf("write %s: %w", path, err), tmp.Close(), os.Remove(tmp.Name()))
	}
	if err := tmp.Chmod(fileMode); err != nil {
		return errors.Join(fmt.Errorf("chmod %s: %w", path, err), tmp.Close(), os.Remove(tmp.Name()))
	}
	if err := tmp.Close(); err != nil {
		return errors.Join(fmt.Errorf("close %s: %w", path, err), os.Remove(tmp.Name()))
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}

	return nil
}
