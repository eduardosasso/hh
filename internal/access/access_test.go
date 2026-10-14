package access

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const wait = 3 * time.Second

func fixture(t *testing.T) [][]byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "caddy.log"))
	if err != nil {
		t.Fatal(err)
	}

	return bytes.Split(bytes.TrimSpace(b), []byte("\n"))
}

func TestParse(t *testing.T) {
	lines := fixture(t)
	ok, err := Parse(lines[0])
	if err != nil {
		t.Fatal(err)
	}
	if ok.Host != "site.localhost" || ok.Method != "GET" || ok.URI != "/" || ok.Status != 200 || ok.Size != 3 {
		t.Fatalf("entry %+v", ok)
	}
	if ok.Type != "text/html" || ok.Proto != "HTTP/2.0" {
		t.Fatalf("type %q proto %q", ok.Type, ok.Proto)
	}
	if ok.Duration.Round(time.Millisecond) != 6*time.Millisecond || ok.App.Round(time.Millisecond) != 6*time.Millisecond {
		t.Fatalf("duration %s app %s", ok.Duration, ok.App)
	}
	if ok.Time.Unix() != 1792000290 {
		t.Fatalf("time %s", ok.Time)
	}
	if _, has := ok.Raw["request"]; !has {
		t.Fatal("raw fields missing")
	}

	failed, err := Parse(lines[1])
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != 502 || failed.App != 0 || !strings.Contains(failed.Error, "connection refused") {
		t.Fatalf("502 entry %+v", failed)
	}

	moved, err := Parse(lines[2])
	if err != nil {
		t.Fatal(err)
	}
	if moved.Location != "https://site.localhost:48443/a" {
		t.Fatalf("location %q", moved.Location)
	}
}

func TestParseRejects(t *testing.T) {
	for _, line := range []string{"not json", `{"msg":"other"}`} {
		if _, err := Parse([]byte(line)); !errors.Is(err, ErrLine) {
			t.Errorf("%q: err %v", line, err)
		}
	}
}

// line builds an access log line for uri.
func line(uri string) string {
	return fmt.Sprintf(`{"ts":1,"request":{"host":"a.localhost","method":"GET","uri":%q},"status":200}`+"\n", uri)
}

func appendLines(t *testing.T, path string, uris ...string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, u := range uris {
		if _, err := f.WriteString(line(u)); err != nil {
			t.Fatal(err)
		}
	}
}

func next(t *testing.T, ch <-chan Entry) string {
	t.Helper()
	select {
	case e := <-ch:
		return e.URI
	case <-time.After(wait):
		t.Fatal("no entry")
	}

	return ""
}

func expect(t *testing.T, ch <-chan Entry, uris ...string) {
	t.Helper()
	for _, u := range uris {
		if got := next(t, ch); got != u {
			t.Fatalf("got %q, want %q", got, u)
		}
	}
}

func follow(t *testing.T, path string, o Options) <-chan Entry {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ch, err := Follow(ctx, path, o)
	if err != nil {
		t.Fatal(err)
	}

	return ch
}

func TestFollowRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	appendLines(t, path, "/old")
	ch := follow(t, path, Options{})
	appendLines(t, path, "/a")
	expect(t, ch, "/a")

	// Rotate the way Caddy does: rename, then a new file at the same path.
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	appendLines(t, path+".1", "/tail-of-old")
	appendLines(t, path, "/b", "/c")
	expect(t, ch, "/tail-of-old", "/b", "/c")

	// Truncate in place.
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * Poll)
	appendLines(t, path, "/d")
	expect(t, ch, "/d")
}

func TestFollowLast(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	appendLines(t, path, "/1", "/skip", "/2", "/3", "/skip")
	keep := func(e Entry) bool { return e.URI != "/skip" }
	ch := follow(t, path, Options{Last: 2, Keep: keep})
	expect(t, ch, "/2", "/3")
	appendLines(t, path, "/skip", "/4")
	expect(t, ch, "/4")

	none := follow(t, path, Options{Keep: keep})
	appendLines(t, path, "/5")
	expect(t, none, "/5")
}

func TestFollowMissingAndPartial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	var mu sync.Mutex
	var warned []error
	ch := follow(t, path, Options{Last: 5, Warn: func(err error) {
		mu.Lock()
		defer mu.Unlock()
		warned = append(warned, err)
	}})
	appendLines(t, path, "/first")
	expect(t, ch, "/first")

	// A line split across writes is read once it is whole.
	full := line("/split")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("garbage\n" + full[:10]); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * Poll)
	if _, err := f.WriteString(full[10:]); err != nil {
		t.Fatal(err)
	}
	expect(t, ch, "/split")
	mu.Lock()
	defer mu.Unlock()
	if len(warned) != 1 || !errors.Is(warned[0], ErrLine) {
		t.Fatalf("warnings %v", warned)
	}
}
