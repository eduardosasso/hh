// Package home holds the machine-level locations and listen ports of hh.
package home

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

const (
	dirName      = ".hh"
	socketName   = "hh.sock"
	stateName    = "routes.json"
	logName      = "daemon.log"
	storageName  = "caddy"
	defaultHTTPS = 443
	defaultHTTP  = 80

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
func HTTPS() (int, error) { return listen(HTTPSEnv, defaultHTTPS) }

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

// URL is the public address of host, with the port only when it is not 443.
func URL(host string) (string, error) {
	p, err := HTTPS()
	if err != nil {
		return "", err
	}
	if p == defaultHTTPS {
		return "https://" + host, nil
	}

	return fmt.Sprintf("https://%s:%d", host, p), nil
}
