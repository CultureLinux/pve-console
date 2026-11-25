package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"pvc/internal/command"
	"pvc/internal/proxmox"
)

// taskRunner answers the task status call with a scripted sequence and
// everything else from a fixed map.
type taskRunner struct {
	fixed    map[string]command.Result
	statuses []string
	polls    int
	calls    []string
}

func (r *taskRunner) Run(ctx context.Context, name string, args ...string) (command.Result, error) {
	key := command.Key(name, args...)
	r.calls = append(r.calls, key)
	if key == statusOf {
		i := r.polls
		if i >= len(r.statuses) {
			i = len(r.statuses) - 1
		}
		r.polls++
		return command.Result{Stdout: []byte(r.statuses[i])}, nil
	}
	if result, ok := r.fixed[key]; ok {
		return result, nil
	}
	if key == "pvesh get /nodes/pve/qemu/300/status/current --output-format json" {
		// The guest type probe that precedes every guest action.
		return command.Result{Stdout: []byte(`{"name":"vm","status":"running"}`)}, nil
	}
	return command.Result{}, context.Canceled
}

func execTask(t *testing.T, runner command.Runner, args ...string) (int, string, string) {
	t.Helper()
	previous := taskPollInterval
	taskPollInterval = time.Millisecond
	defer func() { taskPollInterval = previous }()

	opts, rest, err := ParseGlobalOptions(args)
	if err != nil {
		t.Fatalf("ParseGlobalOptions: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := RunWithClient(rest, opts, proxmox.Client{Runner: runner, Node: "pve"}, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

const (
	running  = `{"status":"running","type":"vzdump"}`
	finished = `{"status":"stopped","exitstatus":"OK","type":"vzdump"}`
	failed   = `{"status":"stopped","exitstatus":"job errors","type":"vzdump"}`
)

var backupRunCall = "pvesh create /nodes/pve/vzdump -vmid 300 -mode snapshot"

func backupRunner(statuses ...string) *taskRunner {
	return &taskRunner{
		fixed:    map[string]command.Result{backupRunCall: ok(`"` + testUPID + `"`)},
		statuses: statuses,
	}
}

func TestParseGlobalOptionsWait(t *testing.T) {
	opts, rest, err := ParseGlobalOptions([]string{"--wait", "backup", "run", "300"})
	if err != nil || !opts.Wait || len(rest) != 3 {
		t.Fatalf("opts=%+v rest=%v err=%v", opts, rest, err)
	}
	if opts, _, _ := ParseGlobalOptions([]string{"backup", "run", "300"}); opts.Wait {
		t.Fatal("--wait must be off by default")
	}
}

func TestWithoutWaitTheTaskIdIsPrinted(t *testing.T) {
	runner := backupRunner(finished)
	code, stdout, _ := execTask(t, runner, "backup", "run", "300")
	if code != 0 || !strings.Contains(stdout, "Task: "+testUPID) || strings.Contains(stdout, "finished") {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	if runner.polls != 0 {
		t.Fatalf("the task must not be polled without --wait, polls=%d", runner.polls)
	}
}

func TestWaitFollowsTheTaskToItsEnd(t *testing.T) {
	runner := backupRunner(running, running, finished)
	code, stdout, stderr := execTask(t, runner, "--wait", "backup", "run", "300")
	if code != 0 || !strings.Contains(stdout, "Backup completed for guest 300") || !strings.Contains(stdout, "finished: OK") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if runner.polls != 3 {
		t.Fatalf("polls=%d", runner.polls)
	}
}

func TestWaitJSONIncludesTheTask(t *testing.T) {
	code, stdout, _ := execTask(t, backupRunner(running, finished), "--wait", "--json", "backup", "run", "300")
	var payload struct {
		UPID string       `json:"upid"`
		Task proxmox.Task `json:"task"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil || code != 0 {
		t.Fatalf("code=%d err=%v stdout=%q", code, err, stdout)
	}
	if payload.UPID != testUPID || !payload.Task.Succeeded() {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestWaitReportsAFailedTask(t *testing.T) {
	runner := backupRunner(running, failed)
	runner.fixed["pvesh get /nodes/pve/tasks/"+testUPID+"/log -limit 500 --output-format json"] =
		ok(`[{"n":1,"t":"INFO: starting"},{"n":2,"t":"ERROR: no space left"}]`)

	code, stdout, stderr := execTask(t, runner, "--wait", "backup", "run", "300")
	if code != 1 {
		t.Fatalf("code=%d", code)
	}
	if strings.Contains(stdout, "completed") {
		t.Fatalf("a failed task must not be reported as completed: %q", stdout)
	}
	for _, want := range []string{"Task failed: job errors", "Task: " + testUPID, "ERROR: no space left"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr is missing %q: %q", want, stderr)
		}
	}
}

func TestWaitGivesUpAtTheTimeout(t *testing.T) {
	code, stdout, stderr := execTask(t, backupRunner(running), "--wait", "--timeout", "30ms", "backup", "run", "300")
	if code != 1 || !strings.Contains(stderr, "still running") || !strings.Contains(stderr, "Task: "+testUPID) {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if strings.Contains(stdout, "completed") {
		t.Fatalf("stdout=%q", stdout)
	}
}

func TestWaitIsIgnoredWhenThereIsNoTask(t *testing.T) {
	runner := &taskRunner{fixed: map[string]command.Result{"pvesh create /nodes/pve/qemu/300/status/start": {}}, statuses: []string{running}}
	code, stdout, _ := execTask(t, runner, "--wait", "vm", "start", "300")
	if code != 0 || !strings.Contains(stdout, "start requested") || runner.polls != 0 {
		t.Fatalf("code=%d stdout=%q polls=%d", code, stdout, runner.polls)
	}
}

func TestWaitAppliesToGuestSnapshotAndBackupActions(t *testing.T) {
	tests := []struct {
		args []string
		call string
		out  string
	}{
		{[]string{"vm", "start", "300"}, "pvesh create /nodes/pve/qemu/300/status/start", "start completed"},
		{[]string{"vm", "stop", "300", "--yes"}, "pvesh create /nodes/pve/qemu/300/status/stop", "stop completed"},
		{[]string{"snapshot", "create", "300", "s1"}, "pvesh create /nodes/pve/qemu/300/snapshot -snapname s1", `Snapshot "s1" created`},
		{[]string{"snapshot", "delete", "300", "s1", "--yes"}, "pvesh delete /nodes/pve/qemu/300/snapshot/s1", `Snapshot "s1" deleted`},
		{[]string{"snapshot", "rollback", "300", "s1", "--yes"}, "pvesh create /nodes/pve/qemu/300/snapshot/s1/rollback", "rolled back"},
		{[]string{"backup", "delete", volQEMU, "--yes"}, "pvesh delete /nodes/pve/storage/backup-nas/content/" + volQEMU, "deleted"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args[:2], " "), func(t *testing.T) {
			runner := &taskRunner{fixed: map[string]command.Result{tt.call: ok(`"` + testUPID + `"`)}, statuses: []string{running, finished}}
			code, stdout, stderr := execTask(t, runner, append([]string{"--wait"}, tt.args...)...)
			if code != 0 || !strings.Contains(stdout, tt.out) || !strings.Contains(stdout, "finished: OK") || runner.polls != 2 {
				t.Fatalf("code=%d stdout=%q stderr=%q polls=%d", code, stdout, stderr, runner.polls)
			}
		})
	}
}

func TestTaskStatusCommand(t *testing.T) {
	runner := &taskRunner{statuses: []string{finished}}
	code, stdout, stderr := execTask(t, runner, "task", "status", testUPID)
	if code != 0 || !strings.Contains(stdout, "STATUS\tstopped") || !strings.Contains(stdout, "RESULT\tOK") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	code, stdout, _ = execTask(t, &taskRunner{statuses: []string{finished}}, "--json", "task", "status", testUPID)
	var task proxmox.Task
	if err := json.Unmarshal([]byte(stdout), &task); err != nil || code != 0 || task.UPID != testUPID || !task.Succeeded() {
		t.Fatalf("code=%d task=%+v err=%v", code, task, err)
	}
}

func TestTaskStatusExitCodes(t *testing.T) {
	tests := []struct {
		name     string
		statuses []string
		args     []string
		code     int
	}{
		{"failed task", []string{failed}, []string{"task", "status", testUPID}, 1},
		{"running task", []string{running}, []string{"task", "status", testUPID}, 0},
		{"wait until ok", []string{running, finished}, []string{"--wait", "task", "status", testUPID}, 0},
		{"wait until failed", []string{running, failed}, []string{"--wait", "task", "status", testUPID}, 1},
		{"invalid upid", []string{finished}, []string{"task", "status", "nope"}, 1},
		{"missing upid", []string{finished}, []string{"task", "status"}, 2},
		{"bad subcommand", []string{finished}, []string{"task", "bogus", testUPID}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &taskRunner{statuses: tt.statuses}
			if code, _, stderr := execTask(t, runner, tt.args...); code != tt.code {
				t.Fatalf("code=%d want %d stderr=%q", code, tt.code, stderr)
			}
		})
	}
}

func TestTaskStatusRejectsInvalidUPIDBeforeAnyCall(t *testing.T) {
	runner := &taskRunner{statuses: []string{finished}}
	execTask(t, runner, "task", "status", "UPID:pve:../../x")
	if len(runner.calls) != 0 {
		t.Fatalf("no command expected, got %v", runner.calls)
	}
}
