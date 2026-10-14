package main

import (
	"os"

	"github.com/eduardosasso/hh/internal/setup"
)

func trustCmd([]string) error {
	return setup.Trust(os.Stdout)
}
