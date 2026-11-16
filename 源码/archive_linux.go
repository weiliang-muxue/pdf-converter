package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// The short-lived decoder gets a hard address-space ceiling. The HTTP service
// and Word engine are unaffected. 7zzs is static and requires no loader/library.
func init() {
	if len(os.Args) < 2 || os.Args[1] != "--archive-worker" {
		return
	}
	executable := archiveEngine()
	args := append([]string{executable}, os.Args[2:]...)
	env := os.Environ()
	err := syscall.Setrlimit(syscall.RLIMIT_AS, &syscall.Rlimit{Cur: 768 << 20, Max: 768 << 20})
	if err == nil {
		err = syscall.Setrlimit(syscall.RLIMIT_CPU, &syscall.Rlimit{Cur: 120, Max: 120})
	}
	if err == nil {
		err = syscall.Exec(executable, args, env)
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
func archiveCommand(ctx context.Context, args ...string) (*exec.Cmd, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, executable, append([]string{"--archive-worker"}, args...)...), nil
}
