package backup

import (
	"context"
	"errors"
	"os"
	"os/exec"
)

type CommandRunner interface {
	Run(context.Context, string, []string, []string) error
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args []string, env []string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if env == nil {
		env = os.Environ()
	}
	cmd.Env = env
	if err := cmd.Run(); err != nil {
		return errors.New("external command failed")
	}
	return nil
}
