package backup

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestExecRunnerRetainsStderrAndExitStatus(t *testing.T) {
	err := (ExecRunner{}).Run(context.Background(), "sh", []string{"-c", "printf 'actual tar reason\\n' >&2; exit 23"}, nil)
	var commandErr *CommandError
	if !errors.As(err, &commandErr) {
		t.Fatalf("expected CommandError, got %T: %v", err, err)
	}
	if commandErr.Status != 23 {
		t.Fatalf("expected exit status 23, got %d", commandErr.Status)
	}
	if !strings.Contains(err.Error(), "sh exited with status 23: actual tar reason") {
		t.Fatalf("stderr or status missing from error: %v", err)
	}
	if errors.Unwrap(commandErr) == nil {
		t.Fatal("original exit error was not wrapped")
	}
}

func TestExecRunnerReportsEmptyStderrMeaningfully(t *testing.T) {
	err := (ExecRunner{}).Run(context.Background(), "sh", []string{"-c", "exit 7"}, nil)
	if err == nil || !strings.Contains(err.Error(), "sh exited with status 7") {
		t.Fatalf("empty stderr error was not useful: %v", err)
	}
}

func TestExecRunnerBoundsAndMarksTruncatedStderr(t *testing.T) {
	err := (ExecRunner{}).Run(context.Background(), "sh", []string{"-c", "head -c 20000 /dev/zero | tr '\\0' x >&2; exit 2"}, nil)
	var commandErr *CommandError
	if !errors.As(err, &commandErr) {
		t.Fatalf("expected CommandError, got %T: %v", err, err)
	}
	if len(commandErr.Stderr) > commandStderrLimit+len(commandStderrTruncationMarker) {
		t.Fatalf("stderr was not bounded: %d bytes", len(commandErr.Stderr))
	}
	if !strings.HasSuffix(commandErr.Stderr, commandStderrTruncationMarker) {
		t.Fatalf("truncated stderr marker missing: %q", commandErr.Stderr[len(commandErr.Stderr)-64:])
	}
}

func TestExecRunnerDoesNotCollectStdout(t *testing.T) {
	err := (ExecRunner{}).Run(context.Background(), "sh", []string{"-c", "head -c 1048576 /dev/zero; printf 'stderr only' >&2; exit 2"}, nil)
	var commandErr *CommandError
	if !errors.As(err, &commandErr) {
		t.Fatalf("expected CommandError, got %T: %v", err, err)
	}
	if commandErr.Stderr != "stderr only" || strings.Contains(err.Error(), "\\x00") {
		t.Fatalf("stdout was retained or stderr changed: %#v", commandErr)
	}
}

func TestExecRunnerWaitsForDescendantHoldingOutputPipes(t *testing.T) {
	started := time.Now()
	err := (ExecRunner{}).Run(context.Background(), "sh", []string{"-c", "(sleep 0.2; printf descendant-finished >&2) & exit 0"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 150*time.Millisecond {
		t.Fatalf("runner returned before the command descendant closed inherited output: %s", elapsed)
	}
}
