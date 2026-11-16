package main

import (
	"context"
	"os/exec"
)

func archiveCommand(ctx context.Context, args ...string) (*exec.Cmd, error) {
	return exec.CommandContext(ctx, archiveEngine(), args...), nil
}
