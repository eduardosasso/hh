package main

import (
	"os"
	"os/exec"
	"strconv"

	"github.com/eduardosasso/hh/internal/home"
)

const tailLines = 100

func logsCmd([]string) error {
	path, err := home.Log()
	if err != nil {
		return err
	}
	cmd := exec.Command("tail", "-n", strconv.Itoa(tailLines), "-F", path)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr

	return cmd.Run()
}
