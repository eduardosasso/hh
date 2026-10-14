package main

import (
	"io"
	"os"
	"testing"
)

func TestVersion(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = stdout })

	code := run([]string{"version"})
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 || string(out) != version+"\n" {
		t.Fatalf("code %d, out %q", code, out)
	}
}
