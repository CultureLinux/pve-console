package command

import (
	"context"
	"fmt"
	"strings"
)

type MockRunner struct {
	Responses map[string]Result
	Errors    map[string]error
	Calls     []string
}

func (r *MockRunner) Run(ctx context.Context, command string, args ...string) (Result, error) {
	key := Key(command, args...)
	r.Calls = append(r.Calls, key)

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
