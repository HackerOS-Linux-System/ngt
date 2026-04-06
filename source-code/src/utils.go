package src

import "os/exec"

// buildCmd is a thin wrapper so we can intercept exec calls in tests.
func buildCmd(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
