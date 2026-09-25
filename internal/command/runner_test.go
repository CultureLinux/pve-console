package command

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestExecRunnerRejectsEmptyCommand(t *testing.T) {
	runner := NewExecRunner(time.Second)

	_, err := runner.Run(context.Background(), "")

	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCommandErrorIncludesStderr(t *testing.T) {
	err := CommandError{
		Result: Result{
			Command: "qm",
			Stderr:  []byte("VM is locked\n"),
		},
		Err: errors.New("exit status 255"),
	}

	if got := err.Error(); !strings.Contains(got, "VM is locked") {
		t.Fatalf("expected stderr in error, got %q", got)
	}
}

func TestMockRunnerRecordsCalls(t *testing.T) {
	runner := &MockRunner{
		Responses: map[string]Result{
			"pvesh get /nodes": {Stdout: []byte("[]")},
		},
	}

	result, err := runner.Run(context.Background(), "pvesh", "get", "/nodes")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result.Stdout) != "[]" {
		t.Fatalf("unexpected stdout: %q", string(result.Stdout))
	}
	if len(runner.Calls) != 1 || runner.Calls[0] != "pvesh get /nodes" {
		t.Fatalf("unexpected calls: %#v", runner.Calls)
	}
}
