package usual

import (
	"path/filepath"
	"testing"
)

func TestMissing(t *testing.T) {
	m, err := load(filepath.Join(t.TempDir(), "ports.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 {
		t.Fatalf("got %v", m)
	}
}

func TestRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ports.json")
	if err := save(p, map[string]int{"shop.localhost": 3000}); err != nil {
		t.Fatal(err)
	}
	m, err := load(p)
	if err != nil {
		t.Fatal(err)
	}
	if m["shop.localhost"] != 3000 || len(m) != 1 {
		t.Fatalf("got %v", m)
	}
}

func TestRememberForget(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := Remember("shop.localhost", 8787); err != nil {
		t.Fatal(err)
	}
	if p, err := Port("shop.localhost"); err != nil || p != 8787 {
		t.Fatalf("port %d, %v", p, err)
	}
	if err := Forget("shop.localhost"); err != nil {
		t.Fatal(err)
	}
	if p, err := Port("shop.localhost"); err != nil || p != 0 {
		t.Fatalf("port %d after forget, %v", p, err)
	}
}
