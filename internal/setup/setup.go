// Package setup prepares a machine for hh: port binding, login service, CA trust.
package setup

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/eduardosasso/hh/internal/client"
	"github.com/eduardosasso/hh/internal/firefox"
	"github.com/eduardosasso/hh/internal/home"
	"github.com/eduardosasso/hh/internal/service"
	"github.com/eduardosasso/hh/internal/trust"
	"github.com/eduardosasso/hh/internal/wait"
)

// Rule is the one line agents need.
const Rule = "Start every dev server with the `hh` prefix. Use the URL it prints, never `localhost:<port>`."

const timeout = 10 * time.Second

// Run does the one-time setup and reports to out.
func Run(out io.Writer) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate hh binary: %w", err)
	}
	if runtime.GOOS == "linux" {
		if err := setcap(exe); err != nil {
			return err
		}
	}
	c, err := client.New()
	if err != nil {
		return err
	}
	if err := stopAdHoc(c); err != nil {
		return err
	}
	log, err := home.Log()
	if err != nil {
		return err
	}
	if err := home.Mkdir(); err != nil {
		return err
	}
	if err := service.Install(exe, log); err != nil {
		return fmt.Errorf("install service: %w", err)
	}
	if err := c.Wait(timeout); err != nil {
		return err
	}
	if err := rootReady(); err != nil {
		return err
	}
	if err := Trust(out); err != nil {
		return err
	}
	fmt.Fprintln(out, "hh is ready.")

	return nil
}

// Trust installs the root in the OS trust store if needed, then enables it in Firefox.
// A Firefox failure is reported but does not fail: the OS trust is done.
func Trust(out io.Writer) error {
	ok, err := trust.Trusted()
	if err != nil {
		return err
	}
	if !ok {
		if err := trust.Install(); err != nil {
			return err
		}
	}
	if err := firefox.Enable(out); err != nil {
		fmt.Fprintln(out, "firefox:", err)
	}

	return nil
}

// setcap lets the binary bind :443 and :80 without root.
func setcap(exe string) error {
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", exe, err)
	}
	cmd := exec.Command("sudo", "setcap", "cap_net_bind_service=+ep", real)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("setcap on %s: %w", real, err)
	}

	return nil
}

// stopAdHoc stops a daemon started on demand so the service can own the socket.
func stopAdHoc(c *client.Client) error {
	installed, err := service.Installed()
	if err != nil || installed {
		return err
	}
	h, err := c.Health()
	if errors.Is(err, client.ErrDown) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := syscall.Kill(h.PID, syscall.SIGTERM); err != nil {
		return fmt.Errorf("stop daemon pid %d: %w", h.PID, err)
	}
	err = wait.Until(timeout, func() (bool, error) {
		_, err := c.Health()
		if errors.Is(err, client.ErrDown) {
			return true, nil
		}

		return false, err
	})
	if errors.Is(err, wait.ErrTimeout) {
		return fmt.Errorf("daemon pid %d did not stop", h.PID)
	}

	return err
}

// rootReady waits for the daemon to write the CA root on first start.
func rootReady() error {
	path, err := home.RootCert()
	if err != nil {
		return err
	}
	err = wait.Until(timeout, func() (bool, error) {
		_, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}

		return err == nil, err
	})
	if errors.Is(err, wait.ErrTimeout) {
		return fmt.Errorf("root certificate missing at %s; see `hh logs`", path)
	}

	return err
}
