package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"pvc/internal/proxmox"
	"pvc/internal/validate"
)

const (
	// defaultWaitTimeout bounds --wait when --timeout is not given.
	defaultWaitTimeout = 30 * time.Minute
	// logTail is the number of task log lines shown when a task fails.
	logTail = 10
)

// taskPollInterval is the delay between two task status polls (a variable so tests can shorten it).
var taskPollInterval = 2 * time.Second

// awaitTask waits for the task of an action when --wait is set. It returns the
// finished task (nil when nothing was awaited) and a non-zero exit code if the
// task failed or could not be followed.
func awaitTask(ctx context.Context, client proxmox.Client, upid string, opts Options, stderr io.Writer) (*proxmox.Task, int) {
	if !opts.Wait || upid == "" {
		return nil, 0
	}

	timeout := defaultWaitTimeout
	if opts.TimeoutSet {
		timeout = opts.Timeout
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	task, err := client.WaitTask(waitCtx, upid, taskPollInterval, nil)
	if err != nil {
		code := printError(stderr, "Unable to follow the task to its end.", err)
		fmt.Fprintf(stderr, "Task: %s\n", upid)
		return nil, code
	}
	if !task.Succeeded() {
		fmt.Fprintf(stderr, "Task failed: %s\nTask: %s\n", task.ExitStatus, upid)
		if lines, err := client.TaskLog(ctx, upid, logTail); err == nil {
			for _, line := range lines {
				fmt.Fprintf(stderr, "  %s\n", line)
			}
		}
		return &task, 1
	}
	return &task, 0
}

// printTaskLine prints the task id of an action: its result when it was awaited,
// otherwise a hint to follow it.
func printTaskLine(w io.Writer, upid string, task *proxmox.Task) {
	switch {
	case upid == "":
	case task != nil:
		fmt.Fprintf(w, "Task: %s finished: %s\n", upid, task.ExitStatus)
	default:
		fmt.Fprintf(w, "Task: %s\n", upid)
	}
}

// doneWord is "completed" when the task was awaited and "requested" otherwise.
func doneWord(task *proxmox.Task) string {
	if task != nil {
		return "completed"
	}
	return "requested"
}

// runTask handles "task status <upid>".
func runTask(ctx context.Context, client proxmox.Client, args []string, opts Options, stdout io.Writer, stderr io.Writer) int {
	if len(args) != 2 || args[0] != "status" {
		fmt.Fprint(stderr, usage)
		return 2
	}
	upid := args[1]
	if err := validate.UPID(upid); err != nil {
		return printError(stderr, "Invalid task id.", err)
	}

	task, err := client.TaskStatus(ctx, upid)
	if err != nil {
		return printError(stderr, "Unable to read the task.", err)
	}
	if opts.Wait && task.Running() {
		finished, code := awaitTask(ctx, client, upid, opts, stderr)
		if code != 0 && finished == nil {
			return code
		}
		if finished != nil {
			task = *finished
		}
	}

	if opts.JSON {
		if code := printJSON(stdout, stderr, task); code != 0 {
			return code
		}
	} else {
		fmt.Fprintf(stdout, "UPID\t%s\nTYPE\t%s\nSTATUS\t%s\n", task.UPID, task.Type, task.Status)
		if task.ExitStatus != "" {
			fmt.Fprintf(stdout, "RESULT\t%s\n", task.ExitStatus)
		}
	}
	if !task.Running() && !task.Succeeded() {
		return 1
	}
	return 0
}
