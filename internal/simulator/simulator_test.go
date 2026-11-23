package simulator

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

const root = "/tmp/root.crt"

const two = `{"devices":{
  "com.apple.CoreSimulator.SimRuntime.iOS-27-2":[{"udid":"B","name":"iPhone 18 Pro"},{"udid":"A","name":"iPhone 17"}],
  "com.apple.CoreSimulator.SimRuntime.watchOS-12-0":[]
}}`

// fake replaces simctl with a canned device list and records every call.
func fake(t *testing.T, devices string, addErr error) *[]string {
	t.Helper()
	calls := []string{}
	supported = func() bool { return true }
	installed = func() bool { return true }
	simctl = func(args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		if args[0] == "list" {
			return []byte(devices), nil
		}

		return nil, addErr
	}

	return &calls
}

func run(t *testing.T) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := Trust(&out, root)

	return out.String(), err
}

func TestTrustsEveryBooted(t *testing.T) {
	calls := fake(t, two, nil)
	out, err := run(t)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"list devices booted -j",
		"keychain A add-root-cert " + root,
		"keychain B add-root-cert " + root,
	}
	if strings.Join(*calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls %q", *calls)
	}
	if out != "simulator: iPhone 17 trusts the root\nsimulator: iPhone 18 Pro trusts the root\n" {
		t.Fatalf("got %q", out)
	}
}

func TestNoneBooted(t *testing.T) {
	calls := fake(t, `{"devices":{}}`, nil)
	out, err := run(t)
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || !strings.Contains(out, "none booted") {
		t.Fatalf("calls %q out %q", *calls, out)
	}
}

func TestXcodeMissing(t *testing.T) {
	calls := fake(t, two, nil)
	installed = func() bool { return false }
	out, err := run(t)
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 || out != "simulator: Xcode not installed, skipped\n" {
		t.Fatalf("calls %q out %q", *calls, out)
	}
}

func TestUnsupportedOS(t *testing.T) {
	calls := fake(t, two, nil)
	supported = func() bool { return false }
	out, err := run(t)
	if err != nil || out != "" || len(*calls) != 0 {
		t.Fatalf("err %v calls %q out %q", err, *calls, out)
	}
}

func TestAddFails(t *testing.T) {
	fake(t, two, errors.New("boom"))
	if _, err := run(t); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("got %v", err)
	}
}

func TestBadList(t *testing.T) {
	fake(t, "not json", nil)
	if _, err := run(t); err == nil || !strings.Contains(err.Error(), "parse simctl") {
		t.Fatalf("got %v", err)
	}
}
