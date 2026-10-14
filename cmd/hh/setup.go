package main

import (
	"errors"
	"flag"
	"os"

	"github.com/eduardosasso/hh/internal/project"
	"github.com/eduardosasso/hh/internal/setup"
)

func setupCmd(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	yes := fs.Bool("agents", false, "add the agent rule to ~/AGENTS.md without asking")
	no := fs.Bool("no-agents", false, "skip the ~/AGENTS.md prompt")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *yes && *no {
		return errors.New("--agents and --no-agents conflict")
	}
	mode := project.Ask
	if *yes {
		mode = project.Yes
	}
	if *no {
		mode = project.No
	}
	if err := setup.Run(os.Stdout); err != nil {
		return err
	}

	return project.Global(os.Stdin, os.Stdout, mode, terminal(os.Stdin))
}

func terminal(f *os.File) bool {
	info, err := f.Stat()

	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
