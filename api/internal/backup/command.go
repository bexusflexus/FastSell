package backup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const commandStderrLimit = 8 * 1024

const commandStderrTruncationMarker = "\n[stderr truncated]"

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
	// Non-file writers make os/exec drain pipes through Wait. In addition to
	// bounding output, this waits for inherited descriptors held by helpers such
	// as GNU tar's zstd child after the top-level tar process exits.
	cmd.Stdout = io.Discard
	stderr := newBoundedCommandStderr(commandStderrLimit)
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		status := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			status = exitErr.ExitCode()
		}
		return &CommandError{Executable: name, Status: status, Stderr: stderr.String(), Err: err}
	}
	return nil
}

// CommandError reports a failed executable without exposing its arguments or environment.
type CommandError struct {
	Executable string
	Status     int
	Stderr     string
	Err        error
}

func (e *CommandError) Error() string {
	detail := strings.TrimSpace(e.Stderr)
	if e.Status >= 0 {
		if detail != "" {
			return fmt.Sprintf("%s exited with status %d: %s", e.Executable, e.Status, detail)
		}
		return fmt.Sprintf("%s exited with status %d: %v", e.Executable, e.Status, e.Err)
	}
	if detail != "" {
		return fmt.Sprintf("%s failed: %v: %s", e.Executable, e.Err, detail)
	}
	return fmt.Sprintf("%s failed: %v", e.Executable, e.Err)
}

func (e *CommandError) Unwrap() error { return e.Err }

type boundedCommandStderr struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func newBoundedCommandStderr(limit int) *boundedCommandStderr {
	return &boundedCommandStderr{limit: limit}
}

func (w *boundedCommandStderr) Write(data []byte) (int, error) {
	originalLength := len(data)
	remaining := w.limit - w.buffer.Len()
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = w.buffer.Write(data)
	}
	if originalLength > remaining {
		w.truncated = true
	}
	return originalLength, nil
}

func (w *boundedCommandStderr) String() string {
	if !w.truncated {
		return w.buffer.String()
	}
	return w.buffer.String() + commandStderrTruncationMarker
}
