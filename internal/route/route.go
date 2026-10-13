// Package route defines the route record shared by the daemon and its clients.
package route

import "time"

// Path sends a path prefix to another local port.
type Path struct {
	Path string `json:"path"`
	Port int    `json:"port"`
}

// Route maps a host to a local port for as long as PID lives.
type Route struct {
	Host    string    `json:"host"`
	Port    int       `json:"port"`
	PID     int       `json:"pid"`
	Cwd     string    `json:"cwd"`
	Paths   []Path    `json:"routes,omitempty"`
	Started time.Time `json:"started"`
}

// Conflict is the body the daemon returns when a host is taken.
type Conflict struct {
	Error string `json:"error"`
	Host  string `json:"host"`
	PID   int    `json:"pid"`
	Cwd   string `json:"cwd"`
}
