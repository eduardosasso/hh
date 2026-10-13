package service

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Unit is the systemd user unit name.
const Unit = "hh.service"

func unitPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config dir: %w", err)
	}

	return filepath.Join(dir, "systemd", "user", Unit), nil
}

// Installed reports whether the unit file exists.
func Installed() (bool, error) {
	p, err := unitPath()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}

	return err == nil, err
}

// Start starts the installed unit.
func Start() error { return run("systemctl", "--user", "start", Unit) }

// Install writes the unit for exe and (re)starts it. The daemon writes its own log file.
func Install(exe, _ string) error {
	p, err := unitPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), dirMode); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(p), err)
	}
	body := fmt.Sprintf(`[Unit]
Description=hh HTTPS proxy

[Service]
ExecStart=%q daemon
Restart=always

[Install]
WantedBy=default.target
`, exe)
	if err := write(p, body); err != nil {
		return err
	}
	if err := run("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	if err := run("systemctl", "--user", "enable", Unit); err != nil {
		return err
	}

	return run("systemctl", "--user", "restart", Unit)
}
