package command

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
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

func shellOrSkip(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is not available")
	}
}

func TestRunLinesReportsLinesInOrderAndKeepsTheOutput(t *testing.T) {
	shellOrSkip(t)
	var lines []string
	// The last line has no line break; stderr and stdout are interleaved.
	script := `echo one; echo two >&2; printf 'three\r\n'; printf 'four'`
	result, err := NewExecRunner(5*time.Second).RunLines(context.Background(), func(line string) { lines = append(lines, line) }, "sh", "-c", script)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := strings.Join(lines, "|"); got != "one|two|three|four" {
		t.Fatalf("lines = %q", got)
	}
	if got := string(result.Stdout); got != "one\ntwo\nthree\r\nfour" {
		t.Fatalf("the whole output is kept, got %q", got)
	}
	if len(result.Stderr) != 0 {
		t.Fatalf("a success has no error output: %q", result.Stderr)
	}
}

func TestRunLinesFailureKeepsTheEndOfTheOutputAsErrorOutput(t *testing.T) {
	shellOrSkip(t)
	var count int
	script := `i=1; while [ $i -le 15 ]; do echo line$i; i=$((i+1)); done; echo boom >&2; exit 100`
	result, err := NewExecRunner(5*time.Second).RunLines(context.Background(), func(string) { count++ }, "sh", "-c", script)

	var commandErr CommandError
	if !errors.As(err, &commandErr) || result.ExitCode != 100 {
		t.Fatalf("exit code 100 expected, got %d, %v", result.ExitCode, err)
	}
	if count != 16 {
		t.Fatalf("16 lines expected, got %d", count)
	}
	stderr := string(result.Stderr)
	if !strings.HasSuffix(stderr, "boom") || strings.Contains(stderr, "line5\n") || !strings.Contains(stderr, "line7") {
		t.Fatalf("the last %d lines are the error output: %q", failureTailLines, stderr)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("the error says why: %v", err)
	}
}

func TestRunLinesStopsOnTimeout(t *testing.T) {
	shellOrSkip(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := NewExecRunner(5*time.Second).RunLines(ctx, func(string) {}, "sh", "-c", "echo start; sleep 5")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a timeout is reported, got %v", err)
	}
}

func TestExecRunnerIsALineRunner(t *testing.T) {
	var _ LineRunner = ExecRunner{}
}

func TestRedactArgs(t *testing.T) {
	t.Parallel()
	failed := CommandError{
		Result: Result{
			Command: "pvesh",
			Args:    []string{"set", "/access/password", "-userid", "alice@pve", "-password", "hunter2hunter2"},
			Stderr:  []byte("400 Parameter verification failed.\npassword: value 'hunter2hunter2' is not allowed"),
			Stdout:  []byte("hunter2hunter2"),
		},
		Err: errors.New("exit status 255"),
	}

	got := RedactArgs(fmt.Errorf("wrapped: %w", failed), "-password")
	var redacted CommandError
	if !errors.As(got, &redacted) {
		t.Fatalf("err = %T", got)
	}
	line := Key(redacted.Result.Command, redacted.Result.Args...)
	if line != "pvesh set /access/password -userid alice@pve -password ***" {
		t.Fatalf("line = %q", line)
	}
	for _, text := range []string{redacted.Error(), string(redacted.Result.Stderr), string(redacted.Result.Stdout), fmt.Sprint(redacted.Result.Args)} {
		if strings.Contains(text, "hunter2") {
			t.Fatalf("secret left in %q", text)
		}
	}
	if !errors.Is(got, failed.Err) {
		t.Fatal("the cause must stay reachable")
	}
	if failed.Result.Args[5] != "hunter2hunter2" {
		t.Fatal("the original error must not change")
	}
}

func TestRedactArgsLeavesTheRest(t *testing.T) {
	t.Parallel()
	if RedactArgs(nil, "-password") != nil {
		t.Fatal("nil stays nil")
	}
	plain := errors.New("boom")
	if got := RedactArgs(plain, "-password"); got != plain {
		t.Fatalf("got %v", got)
	}
	failed := CommandError{Result: Result{Command: "pvesh", Args: []string{"get", "/x", "-password"}, Stderr: []byte("boom")}}
	got := RedactArgs(failed, "-password")
	var again CommandError
	if !errors.As(got, &again) || len(again.Result.Args) != 3 || again.Result.Args[2] != "-password" || string(again.Result.Stderr) != "boom" {
		t.Fatalf("a flag without a value changes nothing: %+v", again.Result)
	}
	empty := CommandError{Result: Result{Args: []string{"-value", "", "x"}, Stderr: []byte("abc")}}
	var out CommandError
	if !errors.As(RedactArgs(empty, "-value"), &out) || string(out.Result.Stderr) != "abc" {
		t.Fatal("an empty value must not blank the output")
	}
}
