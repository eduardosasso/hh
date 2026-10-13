// Package profile adds port flags for tools that ignore $PORT.
package profile

import (
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var variable = regexp.MustCompile(`\$\{PORT\}|\$PORT\b`)

// Expand replaces $PORT and ${PORT} in every argument.
func Expand(args []string, port int) []string {
	p := strconv.Itoa(port)

	return mapArgs(args, func(a string) string { return variable.ReplaceAllString(a, p) })
}

// Apply adds the port flag a known tool needs, unless the user passed one.
func Apply(args []string, port int) []string {
	if len(args) == 0 {
		return args
	}
	p := strconv.Itoa(port)
	switch filepath.Base(args[0]) {
	case "vite":
		return flagged(args, "--port", p, "--strictPort")
	case "wrangler", "uvicorn":
		return flagged(args, "--port", p)
	case "php":
		return php(args, p)
	case "python", "python3":
		return python(args, p)
	}

	return args
}

func flagged(args []string, extra ...string) []string {
	if slices.ContainsFunc(args, isPortFlag) {
		return args
	}

	return append(slices.Clone(args), extra...)
}

func isPortFlag(a string) bool { return a == "--port" || strings.HasPrefix(a, "--port=") }

// php -S takes the address as the next argument.
func php(args []string, p string) []string {
	i := slices.Index(args, "-S")
	if i < 0 || (i+1 < len(args) && !strings.HasPrefix(args[i+1], "-")) {
		return args
	}

	return slices.Insert(slices.Clone(args), i+1, "localhost:"+p)
}

// python -m http.server takes the port as a positional argument.
func python(args []string, p string) []string {
	i := slices.Index(args, "http.server")
	if i < 1 || args[i-1] != "-m" || slices.ContainsFunc(args[i+1:], numeric) {
		return args
	}

	return append(slices.Clone(args), p)
}

func numeric(a string) bool {
	_, err := strconv.Atoi(a)

	return err == nil
}

func mapArgs(args []string, f func(string) string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = f(a)
	}

	return out
}
