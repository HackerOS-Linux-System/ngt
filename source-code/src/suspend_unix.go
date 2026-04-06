//go:build !windows

package src

import "syscall"

func sendSIGTSTP(pid int) error {
	return syscall.Kill(pid, syscall.SIGTSTP)
}
