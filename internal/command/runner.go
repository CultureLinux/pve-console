package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

const DefaultTimeout = 30 * time.Second

type Result struct {
	Command  string
	Args     []string
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Duration time.Duration
}

type Runner interface {
	Run(ctx context.Context, command string, args ...string) (Result, error)
}

type ExecRunner struct {
	Timeout time.Duration
}

func NewExecRunner(timeout time.Duration) ExecRunner {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	return ExecRunner{Timeout: timeout}
}

func (r ExecRunner) Run(ctx context.Context, command string, args ...string) (Result, error) {
	if command == "" {
		return Result{}, errors.New("command name cannot be empty")
	}

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(runCtx, command, args...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := Result{
		Command:  command,
		Args:     append([]string(nil), args...),
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		ExitCode: 0,
		Duration: time.Since(start),
	}

	if runCtx.Err() == context.DeadlineExceeded {
		result.ExitCode = -1
		return result, CommandError{Result: result, Err: runCtx.Err()}
	}

	if err == nil {
		return result, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, CommandError{Result: result, Err: err}
	}

	result.ExitCode = -1
	return result, CommandError{Result: result, Err: err}
}

type CommandError struct {
	Result Result
	Err    error
}

func (e CommandError) Error() string {
	if len(e.Result.Stderr) > 0 {
		return fmt.Sprintf("%s failed: %s", e.Result.Command, string(bytes.TrimSpace(e.Result.Stderr)))
	}

	if e.Err != nil {
		return fmt.Sprintf("%s failed: %v", e.Result.Command, e.Err)
	}

	return fmt.Sprintf("%s failed", e.Result.Command)
}

func (e CommandError) Unwrap() error {
	return e.Err
}
