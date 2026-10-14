package listen

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// lsofMiss is lsof's exit code when some requested pid or file has nothing to list.
const lsofMiss = 1

func scan(pids []int) ([]entry, error) {
	args := []string{"-nP", "-w", "-iTCP", "-sTCP:LISTEN", "-F", "pn"}
	if len(pids) > 0 {
		args = append(args, "-a", "-p", pidList(pids))
	}
	out, err := lsof(args...)
	if err != nil {
		return nil, err
	}

	return parseLsof(out)
}

func cwd(pid int) (string, error) {
	out, err := lsof("-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-F", "n")
	if err != nil {
		return "", err
	}

	return parseCwd(out)
}

func lsof(args ...string) (string, error) {
	bin, err := exec.LookPath("lsof")
	if err != nil {
		return "", errors.New("lsof not found in PATH; hh needs it to find listening ports on macOS")
	}
	out, err := exec.Command(bin, args...).Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == lsofMiss {
		return string(out), nil
	}
	if err != nil {
		return "", fmt.Errorf("lsof %s: %w", strings.Join(args, " "), err)
	}

	return string(out), nil
}

func pidList(pids []int) string {
	s := make([]string, len(pids))
	for i, p := range pids {
		s[i] = strconv.Itoa(p)
	}

	return strings.Join(s, ",")
}
