package command

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// MockRunner answers commands from canned responses, for tests.
type MockRunner struct {
	Responses map[string]Result
	Errors    map[string]error
	// Missing lists command names that are not installed: running one fails the
	// way a real missing binary does (errors.Is(err, exec.ErrNotFound)).
	Missing []string
	Calls   []string
}

func (r *MockRunner) Run(ctx context.Context, command string, args ...string) (Result, error) {
	key := Key(command, args...)
	r.Calls = append(r.Calls, key)

	for _, missing := range r.Missing {
		if missing == command {
			return Result{Command: command, ExitCode: -1}, CommandError{
				Result: Result{Command: command, ExitCode: -1},
				Err:    &exec.Error{Name: command, Err: exec.ErrNotFound},
			}
		}
	}

	if err, ok := r.Errors[key]; ok {
		result := r.Responses[key]
		return result, err
	}

	if result, ok := r.Responses[key]; ok {
		return result, nil
	}

	return Result{}, fmt.Errorf("no mock response for %s", key)
}

func Key(command string, args ...string) string {
	if len(args) == 0 {
		return command
	}

	return command + " " + strings.Join(args, " ")
}
