package main

import (
	"context"
	"os/exec"
)

func archiveCommand(ctx context.Context, args ...string) (*exec.Cmd, error) {
	// Apply limits in the bundled small native shell, never inside a Go runtime
	// whose existing virtual address space may already exceed the limit.
	// This command is constant; all paths and filenames are positional arguments.
	launch := []string{"-c", `ulimit -v 786432 && ulimit -t 120 && exec "$@"`, "pdf-archive", archiveEngine()}
	return exec.CommandContext(ctx, "/bin/sh", append(launch, args...)...), nil
}
