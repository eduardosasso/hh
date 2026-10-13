package port

import (
	"net"
	"testing"
)

func TestHashStable(t *testing.T) {
	for _, h := range []string{"quip.localhost", "api.quip.localhost", "x.localhost"} {
		a, b := Hash(h), Hash(h)
		if a != b {
			t.Fatalf("%s: %d != %d", h, a, b)
		}
		if a < Min || a > Max {
			t.Fatalf("%s: %d out of range", h, a)
		}
	}
	if Hash("quip.localhost") == Hash("api.quip.localhost") {
		t.Fatal("distinct hosts share a port")
	}
}

func TestPickSkipsTaken(t *testing.T) {
	free := func(int) bool { return true }
	p, err := pick(Max, map[int]bool{Max: true}, free)
	if err != nil {
		t.Fatal(err)
	}
	if p != Min {
		t.Fatalf("want wrap to %d, got %d", Min, p)
	}
}

func TestPickSkipsBusy(t *testing.T) {
	start := 45000
	busy := func(p int) bool { return p != start && p != start+1 }
	p, err := pick(start, nil, busy)
	if err != nil {
		t.Fatal(err)
	}
	if p != start+2 {
		t.Fatalf("got %d", p)
	}
}

func TestPickExhausted(t *testing.T) {
	if _, err := pick(Min, nil, func(int) bool { return false }); err == nil {
		t.Fatal("want error")
	}
}

func TestFree(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	p := l.Addr().(*net.TCPAddr).Port
	if Free(p) {
		t.Fatalf("port %d reported free while bound", p)
	}
	h := "busy.localhost"
	got, err := Pick(h, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got < Min || got > Max {
		t.Fatalf("Pick out of range: %d", got)
	}
}
