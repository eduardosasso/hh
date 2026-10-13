package main

import (
	"fmt"
	"os"

	"github.com/eduardosasso/hh/internal/project"
)

func initCmd([]string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cwd: %w", err)
	}

	return project.Init(cwd, os.Stdout)
}
