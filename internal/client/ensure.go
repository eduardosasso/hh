package client

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/eduardosasso/hh/internal/home"
	"github.com/eduardosasso/hh/internal/service"
	"github.com/eduardosasso/hh/internal/wait"
)

const (
	startWait = 10 * time.Second
)

// Ensure returns a client to a running daemon, starting one if needed.
func Ensure() (*Client, error) {
	c, err := New()
	if err != nil {
		return nil, err
	}
	_, err = c.Health()
	if err == nil {
		return c, nil
	}
	if !errors.Is(err, ErrDown) {
		return nil, err
	}
	if err := start(); err != nil {
		return nil, err
	}

	return c, c.Wait(startWait)
}

// Wait polls until the daemon answers or d passes.
func (c *Client) Wait(d time.Duration) error {
	err := wait.Until(d, func() (bool, error) {
		_, err := c.Health()
		if errors.Is(err, ErrDown) {
			return false, nil
		}

		return err == nil, err
	})
	if errors.Is(err, wait.ErrTimeout) {
		return errors.New("daemon did not start; see `hh logs`")
	}

	return err
}

// start prefers the login service and falls back to a detached `hh daemon`.
func start() error {
	ok, err := service.Installed()
	if err != nil {
		return err
	}
	if ok {
		return service.Start()
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate hh binary: %w", err)
	}
	// Crashes before the daemon opens its own logger still reach the log.
	log, err := home.OpenLog()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "daemon")
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	err = cmd.Start()
	if cerr := log.Close(); cerr != nil {
		return errors.Join(err, fmt.Errorf("close log: %w", cerr))
	}
	if err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	return cmd.Process.Release()
}
