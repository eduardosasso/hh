// Package home holds the machine-level locations and listen ports of hh.
package home

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

const (
	dirName     = ".hh"
	socketName  = "hh.sock"
	stateName   = "routes.json"
	logName     = "daemon.log"
	storageName = "caddy"
	defaultHTTP = 80
	dirMode     = 0o700
	logMode     = 0o600

	// DefaultHTTPS is the port that URLs leave out.
	DefaultHTTPS = 443

	// HTTPSEnv and HTTPEnv override the proxy listen ports, mainly for tests.
	HTTPSEnv = "HH_HTTPS_PORT"
	HTTPEnv  = "HH_HTTP_PORT"
)

// Dir is ~/.hh.
func Dir() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}

	return filepath.Join(h, dirName), nil
}

func join(name string) (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}

	return filepath.Join(d, name), nil
}

// Socket is the daemon control socket.
func Socket() (string, error) { return join(socketName) }

// State is the persisted route table.
func State() (string, error) { return join(stateName) }

// Log is the daemon log file.
func Log() (string, error) { return join(logName) }

// Storage is the Caddy storage root.
func Storage() (string, error) { return join(storageName) }

// RootCert is the internal CA root certificate inside Caddy storage.
func RootCert() (string, error) {
	s, err := Storage()
	if err != nil {
		return "", err
	}

	return filepath.Join(s, "pki", "authorities", "local", "root.crt"), nil
}

// HTTPS is the port the proxy serves TLS on.
func HTTPS() (int, error) { return listen(HTTPSEnv, DefaultHTTPS) }

// HTTP is the port the proxy redirects from.
func HTTP() (int, error) { return listen(HTTPEnv, defaultHTTP) }

func listen(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	p, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s=%q: %w", key, v, err)
	}

	return p, nil
}

// URL is the public address of host on the configured HTTPS port.
func URL(host string) (string, error) {
	p, err := HTTPS()
	if err != nil {
		return "", err
	}

	return Address(host, p), nil
}

// Address is https://host, with the port only when it is not 443.
func Address(host string, https int) string {
	if https == DefaultHTTPS {
		return "https://" + host
	}

	return fmt.Sprintf("https://%s:%d", host, https)
}

// Mkdir creates ~/.hh.
func Mkdir() error {
	d, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d, dirMode); err != nil {
		return fmt.Errorf("create %s: %w", d, err)
	}

	return nil
}

// OpenLog opens the daemon log for appending, creating ~/.hh if needed.
func OpenLog() (*os.File, error) {
	if err := Mkdir(); err != nil {
		return nil, err
	}
	p, err := Log()
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, logMode)
	if err != nil {
		return nil, fmt.Errorf("open log: %w", err)
	}

	return f, nil
}
