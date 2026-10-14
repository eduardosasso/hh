package project

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/eduardosasso/hh/internal/setup"
)

// Modes for the global agent rule step.
const (
	Ask = iota
	Yes
	No
)

const (
	globalName = "AGENTS.md"
	prompt     = "Add the agent rule to ~/AGENTS.md? [Y/n] "
)

// Global offers the agent rule for ~/AGENTS.md. With Ask it prompts on in when tty is true.
// When the rule is not added, it prints the rule line for the user to paste.
func Global(in io.Reader, out io.Writer, mode int, tty bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("locate home: %w", err)
	}
	path := filepath.Join(home, globalName)
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if strings.Contains(string(data), setup.Rule) {
		fmt.Fprintln(out, "~/AGENTS.md already has the agent rule")

		return nil
	}
	yes, err := confirm(in, out, mode, tty)
	if err != nil {
		return err
	}
	if !yes {
		fmt.Fprintf(out, "Add this line to your agent instructions (AGENTS.md):\n\n%s\n", setup.Rule)

		return nil
	}
	status, err := Agents(path)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s ~/AGENTS.md\n", status)

	return nil
}

func confirm(in io.Reader, out io.Writer, mode int, tty bool) (bool, error) {
	switch {
	case mode == Yes:
		return true, nil
	case mode == No || !tty:
		return false, nil
	}
	fmt.Fprint(out, prompt)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read answer: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))

	return answer == "" || answer == "y" || answer == "yes", nil
}
