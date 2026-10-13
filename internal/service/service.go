// Package service installs and starts the daemon as a login service.
package service

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	fileMode = 0o644
	dirMode  = 0o755
)

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}

	return nil
}

func write(path, body string) error {
	if err := os.WriteFile(path, []byte(body), fileMode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}
