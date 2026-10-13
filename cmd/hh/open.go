package main

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/eduardosasso/hh/internal/name"
)

func openCmd([]string) error {
	a, err := resolve(name.Options{})
	if err != nil {
		return err
	}
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	if out, err := exec.Command(opener, a.URL).CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", opener, a.URL, err, out)
	}

	return nil
}
