package proxmox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"pvc/internal/command"
)

const taskStatusCall = "pvesh get /nodes/pve/tasks/" + testUPID + "/status --output-format json"

func TestTaskStatus(t *testing.T) {
	runner := &command.MockRunner{Responses: map[string]command.Result{
		taskStatusCall: {Stdout: []byte(`{"status":"stopped","exitstatus":"OK","type":"vzdump","user":"root@pam","pid":12,"starttime":1700000000}`)},
	}}
	task, err := Client{Runner: runner, Node: "pve"}.TaskStatus(context.Background(), testUPID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.UPID != testUPID || task.Node != "pve" || task.Type != "vzdump" || task.Running() || !task.Succeeded() {
		t.Fatalf("unexpected task: %+v", task)
	}
}

func TestTaskStatusRejectsInvalidUPID(t *testing.T) {
	runner := &command.MockRunner{}
	for _, upid := range []string{"", "nope", "UPID:pve:../../x"} {
		if _, err := (Client{Runner: runner, Node: "pve"}).TaskStatus(context.Background(), upid); err == nil {
			t.Errorf("TaskStatus(%q) must fail", upid)
		}
	}
	if len(runner.Calls) != 0 {
		t.Fatalf("no command expected, got %v", runner.Calls)
	}
}

func TestTaskSucceeded(t *testing.T) {
	tests := []struct {
		exit string
		want bool
	}{
		{"OK", true},
		{"WARNINGS: 2", true},
		{"job errors", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := (Task{Status: "stopped", ExitStatus: tt.exit}).Succeeded(); got != tt.want {
			t.Errorf("Succeeded(%q) = %t, want %t", tt.exit, got, tt.want)
		}
	}
	if !(Task{Status: "running"}).Running() || (Task{Status: "stopped"}).Running() {
		t.Error("Running() is wrong")
	}
}

func TestTaskLogReturnsTail(t *testing.T) {
	var entries []string
	for i := 1; i <= 8; i++ {
		entries = append(entries, fmt.Sprintf(`{"n":%d,"t":"line %d"}`, i, i))
	}
	runner := &command.MockRunner{Responses: map[string]command.Result{
		"pvesh get /nodes/pve/tasks/" + testUPID + "/log -limit 500 --output-format json": {Stdout: []byte("[" + strings.Join(entries, ",") + "]")},
	}}
	lines, err := Client{Runner: runner, Node: "pve"}.TaskLog(context.Background(), testUPID, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 3 || lines[0] != "line 6" || lines[2] != "line 8" {
		t.Fatalf("unexpected lines: %v", lines)
	}
}

// sequenceRunner answers successive status calls with the given payloads.
type sequenceRunner struct {
	payloads []string
	calls    int
	err      error
}

func (r *sequenceRunner) Run(ctx context.Context, name string, args ...string) (command.Result, error) {
	if r.err != nil {
		return command.Result{}, r.err
	}
	i := r.calls
	if i >= len(r.payloads) {
		i = len(r.payloads) - 1
	}
	r.calls++
	return command.Result{Stdout: []byte(r.payloads[i])}, nil
}

func TestWaitTaskPollsUntilStopped(t *testing.T) {
	runner := &sequenceRunner{payloads: []string{
		`{"status":"running"}`,
		`{"status":"running"}`,
		`{"status":"stopped","exitstatus":"OK"}`,
	}}
	var seen []string
	task, err := Client{Runner: runner, Node: "pve"}.WaitTask(context.Background(), testUPID, time.Millisecond, func(t Task) {
		seen = append(seen, t.Status)
	})
	if err != nil || !task.Succeeded() {
		t.Fatalf("task=%+v err=%v", task, err)
	}
	if runner.calls != 3 || len(seen) != 3 {
		t.Fatalf("calls=%d seen=%v", runner.calls, seen)
	}
}

func TestWaitTaskReportsFailure(t *testing.T) {
	runner := &sequenceRunner{payloads: []string{`{"status":"stopped","exitstatus":"job errors"}`}}
	task, err := Client{Runner: runner, Node: "pve"}.WaitTask(context.Background(), testUPID, time.Millisecond, nil)
	if err != nil || task.Succeeded() || task.ExitStatus != "job errors" {
		t.Fatalf("task=%+v err=%v", task, err)
	}
}

func TestWaitTaskStopsWhenContextEnds(t *testing.T) {
	runner := &sequenceRunner{payloads: []string{`{"status":"running"}`}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	task, err := Client{Runner: runner, Node: "pve"}.WaitTask(ctx, testUPID, 5*time.Millisecond, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline error, got %v", err)
	}
	if !task.Running() {
		t.Fatalf("the last known state should be returned, got %+v", task)
	}
}

func TestWaitTaskPropagatesStatusError(t *testing.T) {
	boom := errors.New("pvesh down")
	_, err := Client{Runner: &sequenceRunner{err: boom}, Node: "pve"}.WaitTask(context.Background(), testUPID, time.Millisecond, nil)
	if !errors.Is(err, boom) {
		t.Fatalf("expected the runner error, got %v", err)
	}
}

func TestClusterTasks(t *testing.T) {
	const aptUPID = "UPID:pve2:0000ABCD:00C4D5E6:67890ABC:aptupdate::root@pam:"
	runner := &command.MockRunner{Responses: map[string]command.Result{
		"pvesh get /cluster/tasks --output-format json": {Stdout: []byte(`[
			{"upid":"` + testUPID + `","node":"pve","type":"vzdump","id":"103","user":"root@pam","starttime":100,"endtime":160,"status":"job errors"},
			{"upid":"` + aptUPID + `","node":"pve2","type":"aptupdate","user":"root@pam","starttime":200},
			{"upid":"UPID:pve:0000ABCD:00C4D5E6:67890ABC:vzdump:101:root@pam:","type":"vzdump","starttime":10,"endtime":20,"status":"WARNINGS: 1"},
			{"upid":"UPID:pve:0000ABCD:00C4D5E6:67890ABC:vzdump:102:root@pam:","type":"vzdump","starttime":10,"endtime":20,"status":"OK"},
			{"upid":"UPID:pve:0000ABCD:00C4D5E6:67890ABC:qmstart:104:root@pam:","starttime":10,"endtime":20},
			{"upid":"not-a-upid","endtime":20,"status":"ERROR"}
		]`)},
	}}
	tasks, err := Client{Runner: runner, Node: "pve"}.ClusterTasks(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 5 {
		t.Fatalf("a row with an invalid UPID must be skipped, got %d tasks: %+v", len(tasks), tasks)
	}

	failed := tasks[0]
	if !failed.Failed() || failed.ExitStatus != "job errors" || failed.ID != "103" || failed.EndTime != 160 || failed.Node != "pve" {
		t.Fatalf("unexpected failed task: %+v", failed)
	}
	if running := tasks[1]; !running.Running() || running.Failed() || running.Node != "pve2" {
		t.Fatalf("a task without endtime is running, not failed: %+v", running)
	}
	if warned := tasks[2]; warned.Failed() || !warned.Succeeded() {
		t.Fatalf("warnings are not a failure: %+v", warned)
	}
	if ok := tasks[3]; ok.Failed() {
		t.Fatalf("OK is not a failure: %+v", ok)
	}
	if unknown := tasks[4]; unknown.Failed() {
		t.Fatalf("a finished task without exit status must not be reported: %+v", unknown)
	}
}

func TestClusterTasksReportsReadErrors(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{},
		Errors:    map[string]error{"pvesh get /cluster/tasks --output-format json": errors.New("permission denied")},
	}
	if _, err := (Client{Runner: runner, Node: "pve"}).ClusterTasks(context.Background()); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("the cause must be kept, got %v", err)
	}
}
