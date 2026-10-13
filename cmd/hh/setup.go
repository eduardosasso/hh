package main

import (
	"os"

	"github.com/eduardosasso/hh/internal/setup"
)

func setupCmd([]string) error { return setup.Run(os.Stdout) }
