package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"sync"
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

// LineRunner is a Runner that can also report the output of a command line by
// line while it runs, for the commands the user watches (apt). The standard
// and error outputs are merged in the order they were written.
type LineRunner interface {
	Runner
	RunLines(ctx context.Context, onLine func(string), command string, args ...string) (Result, error)
}

// failureTailLines is the number of output lines kept as the error output of a
// line-by-line command that failed.
const failureTailLines = 10

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
	return r.run(ctx, nil, command, args...)
}

// RunLines implements LineRunner. onLine is called from the goroutine that
// copies the output, one call at a time, with the line without its line break.
func (r ExecRunner) RunLines(ctx context.Context, onLine func(string), command string, args ...string) (Result, error) {
	return r.run(ctx, onLine, command, args...)
}

func (r ExecRunner) run(ctx context.Context, onLine func(string), command string, args ...string) (Result, error) {
	if command == "" {
		return Result{}, errors.New("command name cannot be empty")
	}

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	runCtx := ctx
	cancel := func() {}
	if _, ok := ctx.Deadline(); !ok {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(runCtx, command, args...)
	// After the timeout kills the command, a child that still holds the output
	// pipe (dpkg under apt) must not block the return forever.
	cmd.WaitDelay = 3 * time.Second

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var merged *lineWriter
	if onLine == nil {
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
	} else {
		// One writer for both outputs: exec shares it, so lines keep their order.
		merged = &lineWriter{onLine: onLine}
		cmd.Stdout = merged
		cmd.Stderr = merged
	}

	err := cmd.Run()
	result := Result{
		Command:  command,
		Args:     append([]string(nil), args...),
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		ExitCode: 0,
		Duration: time.Since(start),
	}
	if merged != nil {
		merged.flush()
		result.Stdout = merged.all()
		if err != nil {
			result.Stderr = merged.tail(failureTailLines)
		}
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

// lineWriter splits what a command writes into lines and reports each one. It
// keeps the whole output too.
type lineWriter struct {
	onLine  func(string)
	mu      sync.Mutex
	pending []byte
	output  bytes.Buffer
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.output.Write(p)
	w.pending = append(w.pending, p...)
	for {
		i := bytes.IndexByte(w.pending, '\n')
		if i < 0 {
			break
		}
		w.onLine(strings.TrimRight(string(w.pending[:i]), "\r"))
		w.pending = w.pending[i+1:]
	}
	return len(p), nil
}

// flush reports the last line when the command did not end it with a line break.
func (w *lineWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) > 0 {
		w.onLine(strings.TrimRight(string(w.pending), "\r"))
		w.pending = nil
	}
}

func (w *lineWriter) all() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.output.Bytes()...)
}

// tail returns the last n lines of the output.
func (w *lineWriter) tail(n int) []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	lines := strings.Split(strings.TrimRight(w.output.String(), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return []byte(strings.Join(lines, "\n"))
}

// redacted is what replaces a secret in an error.
const redacted = "***"

// RedactArgs hides the secrets of a failed command: the value that follows each
// of flags is replaced in the arguments, and wherever the error output repeats
// it. Errors that are not command errors are returned as they are. The error
// ends up in the log, which must never hold a password or a secret.
func RedactArgs(err error, flags ...string) error {
	var failed CommandError
	if err == nil || !errors.As(err, &failed) {
		return err
	}
	args := slices.Clone(failed.Result.Args)
	var secrets []string
	for i := 0; i+1 < len(args); i++ {
		if slices.Contains(flags, args[i]) {
			if args[i+1] != "" {
				secrets = append(secrets, args[i+1])
			}
			args[i+1] = redacted
			i++
		}
	}
	failed.Result.Args = args
	failed.Result.Stderr = replaceAll(failed.Result.Stderr, secrets)
	failed.Result.Stdout = replaceAll(failed.Result.Stdout, secrets)

	return failed
}

func replaceAll(output []byte, secrets []string) []byte {
	text := string(output)
	for _, secret := range secrets {
		text = strings.ReplaceAll(text, secret, redacted)
	}
	if text == string(output) {
		return output
	}

	return []byte(text)
}
