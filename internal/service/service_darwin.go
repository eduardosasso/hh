package service

import (
	"errors"
	"fmt"
	"html"
	"io/fs"
	"os"
	"path/filepath"
)

// Label is the launchd job label.
const Label = "com.github.eduardosasso.hh"

func plist() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}

	return filepath.Join(h, "Library", "LaunchAgents", Label+".plist"), nil
}

func domain() string { return fmt.Sprintf("gui/%d", os.Getuid()) }

func target() string { return domain() + "/" + Label }

// Installed reports whether the launchd job file exists.
func Installed() (bool, error) {
	p, err := plist()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}

	return err == nil, err
}

// Start starts the installed job, loading it first when launchd does not know it.
func Start() error {
	if loaded() {
		return run("launchctl", "kickstart", target())
	}
	p, err := plist()
	if err != nil {
		return err
	}

	return run("launchctl", "bootstrap", domain(), p)
}

// loaded asks launchd; a job it does not know makes print exit non-zero.
func loaded() bool { return run("launchctl", "print", target()) == nil }

// Install writes the job for exe, logging to log, and (re)loads it.
func Install(exe, log string) error {
	p, err := plist()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), dirMode); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(p), err)
	}
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key><array><string>%s</string><string>daemon</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, Label, html.EscapeString(exe), html.EscapeString(log), html.EscapeString(log))
	if err := write(p, body); err != nil {
		return err
	}
	if loaded() {
		if err := run("launchctl", "bootout", target()); err != nil {
			return err
		}
	}

	return run("launchctl", "bootstrap", domain(), p)
}
