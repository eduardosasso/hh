// Package simulator adds the root to booted iOS Simulators, which keep their own trust store.
package simulator

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"sort"
	"strings"
)

// device is one entry of `simctl list devices booted -j`.
type device struct {
	UDID string `json:"udid"`
	Name string `json:"name"`
}

// list is the shape of `simctl list devices booted -j`, keyed by runtime.
type list struct {
	Devices map[string][]device `json:"devices"`
}

// supported reports whether this OS can run simulators. Tests replace it.
var supported = func() bool { return runtime.GOOS == "darwin" }

// installed reports whether xcrun exists. Tests replace it.
var installed = func() bool {
	_, err := exec.LookPath("xcrun")

	return err == nil
}

// simctl runs `xcrun simctl` and returns stdout. Tests replace it.
var simctl = func(args ...string) ([]byte, error) {
	out, err := exec.Command("xcrun", append([]string{"simctl"}, args...)...).Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return nil, fmt.Errorf("simctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(exit.Stderr)))
	}
	if err != nil {
		return nil, fmt.Errorf("simctl %s: %w", strings.Join(args, " "), err)
	}

	return out, nil
}

// booted lists the running simulators.
func booted() ([]device, error) {
	out, err := simctl("list", "devices", "booted", "-j")
	if err != nil {
		return nil, err
	}
	var l list
	if err := json.Unmarshal(out, &l); err != nil {
		return nil, fmt.Errorf("parse simctl device list: %w", err)
	}
	found := []device{}
	for _, ds := range l.Devices {
		found = append(found, ds...)
	}
	sort.Slice(found, func(i, j int) bool { return found[i].Name < found[j].Name })

	return found, nil
}

// Trust adds the root at path to every booted simulator. Adding it twice is a no-op.
func Trust(out io.Writer, path string) error {
	if !supported() {
		return nil
	}
	if !installed() {
		fmt.Fprintln(out, "simulator: Xcode not installed, skipped")

		return nil
	}
	found, err := booted()
	if err != nil {
		return err
	}
	if len(found) == 0 {
		fmt.Fprintln(out, "simulator: none booted, skipped; boot one, then run `hh trust`")

		return nil
	}
	for _, d := range found {
		if _, err := simctl("keychain", d.UDID, "add-root-cert", path); err != nil {
			return err
		}
		fmt.Fprintf(out, "simulator: %s trusts the root\n", d.Name)
	}

	return nil
}
