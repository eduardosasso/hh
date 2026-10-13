package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/eduardosasso/hh/internal/route"
)

const stateMode = 0o600

// table is the route set keyed by host. Callers hold the daemon lock.
type table map[string]route.Route

func (t table) list() []route.Route {
	out := make([]route.Route, 0, len(t))
	for _, r := range t {
		out = append(out, r)
	}

	return sorted(out)
}

// dead returns hosts whose wrapper pid is gone.
func (t table) dead() []string {
	var out []string
	for h, r := range t {
		if !alive(r.PID) {
			out = append(out, h)
		}
	}

	return out
}

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)

	return err == nil || errors.Is(err, syscall.EPERM)
}

func load(path string) (table, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return table{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}
	var rs []route.Route
	if err := json.Unmarshal(b, &rs); err != nil {
		return nil, fmt.Errorf("decode state %s: %w", path, err)
	}
	t := table{}
	for _, r := range rs {
		t[r.Host] = r
	}

	return t, nil
}

func (t table) save(path string) error {
	b, err := json.MarshalIndent(t.list(), "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".routes-*.json")
	if err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	if _, err := tmp.Write(b); err != nil {
		return errors.Join(fmt.Errorf("write state: %w", err), tmp.Close(), os.Remove(tmp.Name()))
	}
	if err := tmp.Chmod(stateMode); err != nil {
		return errors.Join(fmt.Errorf("chmod state: %w", err), tmp.Close(), os.Remove(tmp.Name()))
	}
	if err := tmp.Close(); err != nil {
		return errors.Join(fmt.Errorf("close state: %w", err), os.Remove(tmp.Name()))
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}

	return nil
}
