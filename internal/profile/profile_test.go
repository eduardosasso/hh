package profile

import (
	"slices"
	"testing"
)

func TestApply(t *testing.T) {
	cases := []struct {
		in, want []string
	}{
		{[]string{"vite"}, []string{"vite", "--port", "41000", "--strictPort"}},
		{[]string{"./node_modules/.bin/vite", "dev"}, []string{"./node_modules/.bin/vite", "dev", "--port", "41000", "--strictPort"}},
		{[]string{"vite", "--port", "5173"}, []string{"vite", "--port", "5173"}},
		{[]string{"vite", "--port=5173"}, []string{"vite", "--port=5173"}},
		{[]string{"wrangler", "dev"}, []string{"wrangler", "dev", "--port", "41000"}},
		{[]string{"uvicorn", "app:app"}, []string{"uvicorn", "app:app", "--port", "41000"}},
		{[]string{"php", "-S"}, []string{"php", "-S", "localhost:41000"}},
		{[]string{"php", "-S", "-t", "public"}, []string{"php", "-S", "localhost:41000", "-t", "public"}},
		{[]string{"php", "-S", "0.0.0.0:8000"}, []string{"php", "-S", "0.0.0.0:8000"}},
		{[]string{"php", "artisan", "serve"}, []string{"php", "artisan", "serve"}},
		{[]string{"python3", "-m", "http.server"}, []string{"python3", "-m", "http.server", "41000"}},
		{[]string{"python", "-m", "http.server", "8000"}, []string{"python", "-m", "http.server", "8000"}},
		{[]string{"python", "manage.py", "runserver"}, []string{"python", "manage.py", "runserver"}},
		{[]string{"bun", "run", "dev"}, []string{"bun", "run", "dev"}},
	}
	for _, c := range cases {
		if got := Apply(c.in, 41000); !slices.Equal(got, c.want) {
			t.Errorf("Apply(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestExpand(t *testing.T) {
	in := []string{"rails", "s", "-p", "$PORT", "--bind=127.0.0.1:${PORT}", "$PORTS", "$HOME"}
	want := []string{"rails", "s", "-p", "41000", "--bind=127.0.0.1:41000", "$PORTS", "$HOME"}
	if got := Expand(in, 41000); !slices.Equal(got, want) {
		t.Fatalf("got %v", got)
	}
}
