package project

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eduardosasso/hh/internal/setup"
)

func TestGlobal(t *testing.T) {
	cases := []struct {
		name, existing, answer string
		mode                   int
		tty                    bool
		want, said             string
	}{
		{"yes", "", "y\n", Ask, true, setup.Rule + "\n", "created ~/AGENTS.md"},
		{"enter", "# Mine\n", "\n", Ask, true, "# Mine\n\n" + setup.Rule + "\n", "updated ~/AGENTS.md"},
		{"already present", setup.Rule + "\n", "n\n", Ask, true, setup.Rule + "\n", "already has the agent rule"},
		{"no", "", "n\n", Ask, true, "", setup.Rule},
		{"flag yes", "", "", Yes, true, setup.Rule + "\n", "created ~/AGENTS.md"},
		{"flag no", "", "y\n", No, true, "", setup.Rule},
		{"not a terminal", "", "y\n", Ask, false, "", setup.Rule},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			path := filepath.Join(home, "AGENTS.md")
			if c.existing != "" {
				if err := os.WriteFile(path, []byte(c.existing), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var out bytes.Buffer
			if err := Global(strings.NewReader(c.answer), &out, c.mode, c.tty); err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(path)
			if c.existing != "" && c.want == "" {
				c.want = c.existing
			}
			if string(got) != c.want {
				t.Errorf("file = %q, want %q", got, c.want)
			}
			if !strings.Contains(out.String(), c.said) {
				t.Errorf("output = %q, want %q", out.String(), c.said)
			}
		})
	}
}
