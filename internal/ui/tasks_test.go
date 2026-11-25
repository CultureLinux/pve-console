package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"pvc/internal/proxmox"
)

func statusJSON(status, exit string) string {
	return `{"status":"` + status + `","exitstatus":"` + exit + `","type":"vzdump"}`
}

func TestBackupTaskIsTrackedUntilItEnds(t *testing.T) {
	runner := &fakeRunner{stdout: map[string]string{"vzdump": `"` + testUPID + `"`}}
	m := newTestModel(runner)

	m, cmd := deliver(t, m, runBackupMsg{vmid: 300, upid: testUPID})
	if len(m.tasks) != 1 || cmd == nil {
		t.Fatalf("tasks=%v cmd=%t", m.tasks, cmd != nil)
	}
	m.width, m.height = 140, 30
	if !strings.Contains(stripANSI(m.header(130)), "1 task") {
		t.Fatal("the header should show the running task count")
	}

	// Still running: the task stays tracked and is polled again.
	m, cmd = deliver(t, m, taskResultMsg{upid: testUPID, task: proxmox.Task{Status: "running"}})
	if len(m.tasks) != 1 || cmd == nil {
		t.Fatalf("a running task must keep being polled, tasks=%v", m.tasks)
	}

	// Finished: dropped, result shown, backups reloaded.
	m, cmd = deliver(t, m, taskResultMsg{upid: testUPID, task: proxmox.Task{Status: "stopped", ExitStatus: "OK"}})
	if len(m.tasks) != 0 || m.message != "backup of 300: OK" || cmd == nil {
		t.Fatalf("tasks=%v message=%q cmd=%t", m.tasks, m.message, cmd != nil)
	}
	if strings.Contains(stripANSI(m.header(130)), "task") {
		t.Fatal("the header must not show tasks once they are done")
	}
	cmd()
	if !runner.called("/cluster/backup") {
		t.Fatalf("backups should be reloaded: %v", runner.calls)
	}
}

func TestFailedTaskIsReported(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m, _ = deliver(t, m, runBackupMsg{vmid: 300, upid: testUPID})

	m, cmd := deliver(t, m, taskResultMsg{upid: testUPID, task: proxmox.Task{Status: "stopped", ExitStatus: "job errors"}})
	if len(m.tasks) != 0 || m.message != "backup of 300: FAILED: job errors" {
		t.Fatalf("tasks=%v message=%q", m.tasks, m.message)
	}
	if cmd == nil {
		t.Fatal("data should be reloaded after a failure too")
	}
}

func TestTaskWithWarningsCountsAsOK(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m, _ = deliver(t, m, runBackupMsg{vmid: 300, upid: testUPID})
	m, _ = deliver(t, m, taskResultMsg{upid: testUPID, task: proxmox.Task{Status: "stopped", ExitStatus: "WARNINGS: 1"}})
	if m.message != "backup of 300: OK" {
		t.Fatalf("message=%q", m.message)
	}
}

func TestTaskIsGivenUpAfterRepeatedPollErrors(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m, _ = deliver(t, m, runBackupMsg{vmid: 300, upid: testUPID})
	boom := errors.New("pvesh down")

	var cmd tea.Cmd
	for i := 1; i < maxTaskErrors; i++ {
		m, cmd = deliver(t, m, taskResultMsg{upid: testUPID, err: boom})
		if len(m.tasks) != 1 || cmd == nil {
			t.Fatalf("poll error %d should be retried, tasks=%v", i, m.tasks)
		}
	}
	m, cmd = deliver(t, m, taskResultMsg{upid: testUPID, err: boom})
	if len(m.tasks) != 0 || cmd == nil || !strings.Contains(m.message, "lost track of backup of 300") {
		t.Fatalf("tasks=%v message=%q", m.tasks, m.message)
	}
}

func TestPollErrorCounterResetsOnSuccess(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m, _ = deliver(t, m, runBackupMsg{vmid: 300, upid: testUPID})
	boom := errors.New("pvesh down")
	for i := 0; i < maxTaskErrors-1; i++ {
		m, _ = deliver(t, m, taskResultMsg{upid: testUPID, err: boom})
	}
	m, _ = deliver(t, m, taskResultMsg{upid: testUPID, task: proxmox.Task{Status: "running"}})
	if m.tasks[0].errors != 0 {
		t.Fatalf("errors=%d", m.tasks[0].errors)
	}
}

func TestUnknownTaskMessagesAreIgnored(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m, cmd := deliver(t, m, taskPollMsg{upid: testUPID})
	if cmd != nil {
		t.Fatal("polling an untracked task must do nothing")
	}
	m, cmd = deliver(t, m, taskResultMsg{upid: testUPID, task: proxmox.Task{Status: "stopped", ExitStatus: "OK"}})
	if cmd != nil || m.message != "" {
		t.Fatalf("message=%q", m.message)
	}
}

func TestPollTaskReadsStatus(t *testing.T) {
	runner := &fakeRunner{stdout: map[string]string{"/status": statusJSON("stopped", "OK")}}
	m := newTestModel(runner)
	m, _ = deliver(t, m, runBackupMsg{vmid: 300, upid: testUPID})

	m, cmd := deliver(t, m, taskPollMsg{upid: testUPID})
	if cmd == nil {
		t.Fatal("expected a poll command")
	}
	m, _ = execute(t, m, cmd)
	if len(m.tasks) != 0 || m.message != "backup of 300: OK" {
		t.Fatalf("tasks=%v message=%q", m.tasks, m.message)
	}
	if !runner.called("tasks/" + testUPID + "/status") {
		t.Fatalf("status not read: %v", runner.calls)
	}
}

func TestGuestActionWithTaskDefersTheRefresh(t *testing.T) {
	runner := &fakeRunner{stdout: map[string]string{"status/start": `"` + testUPID + `"`}}
	m := newTestModel(runner)

	m, _ = press(t, m, "+")
	m, cmd := press(t, m, "enter")
	m, next := execute(t, m, cmd)
	if len(m.tasks) != 1 || next == nil || !strings.Contains(m.message, "start-guest started (task running)") {
		t.Fatalf("tasks=%v message=%q", m.tasks, m.message)
	}
	if runner.called("pvesh get /nodes/pve/qemu --output-format json") {
		t.Fatal("guests must not be reloaded before the task ends")
	}

	m, cmd = deliver(t, m, taskResultMsg{upid: testUPID, task: proxmox.Task{Status: "stopped", ExitStatus: "OK"}})
	if cmd == nil || !strings.Contains(m.message, "start-guest 300: OK") {
		t.Fatalf("message=%q", m.message)
	}
}

func TestSnapshotTasksReloadSnapshotsWhenDone(t *testing.T) {
	runner := &fakeRunner{stdout: map[string]string{"snapshot": `[{"name":"snap1"}]`}}
	m := newTestModel(runner)

	m, next := deliver(t, m, createSnapshotMsg{vmid: 300, name: "snap1", upid: testUPID})
	if len(m.tasks) != 1 || next == nil || !m.showSnaps || !strings.Contains(m.message, "started") {
		t.Fatalf("tasks=%v message=%q", m.tasks, m.message)
	}
	m, cmd := deliver(t, m, taskResultMsg{upid: testUPID, task: proxmox.Task{Status: "stopped", ExitStatus: "OK"}})
	if cmd == nil {
		t.Fatal("snapshots should be reloaded")
	}
	m, _ = execute(t, m, cmd)
	if len(m.snapshots) != 1 {
		t.Fatalf("snapshots=%v", m.snapshots)
	}
}

func TestTrackingTheSameTaskTwiceKeepsOneEntry(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m = m.track(testUPID, "a", refreshBackups, 1)
	m = m.track(testUPID, "b", refreshBackups, 1)
	if len(m.tasks) != 1 || m.tasks[0].label != "a" {
		t.Fatalf("tasks=%v", m.tasks)
	}
}

func TestTrackingDoesNotMutateEarlierCopies(t *testing.T) {
	before := newTestModel(&fakeRunner{})
	after := before.track(testUPID, "a", refreshBackups, 1)
	if len(before.tasks) != 0 || len(after.tasks) != 1 {
		t.Fatalf("before=%v after=%v", before.tasks, after.tasks)
	}
	again := after.untrack(testUPID)
	if len(after.tasks) != 1 || len(again.tasks) != 0 {
		t.Fatalf("after=%v again=%v", after.tasks, again.tasks)
	}
}

// The snapshot panel opens right after a snapshot is created: it must show the
// list at once, not stay empty until the task ends (or forever if the task can
// no longer be followed).
func TestSnapshotPanelLoadsAtOnceAfterCreation(t *testing.T) {
	runner := &fakeRunner{stdout: map[string]string{"/snapshot": `[{"name":"snap1"},{"name":"current"}]`}}
	m := newTestModel(runner)

	m, cmd := deliver(t, m, createSnapshotMsg{vmid: 300, name: "snap1", upid: testUPID})
	if !m.showSnaps || cmd == nil {
		t.Fatalf("showSnaps=%t cmd=%t", m.showSnaps, cmd != nil)
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("expected the list load and the task poll, got %T", cmd())
	}
	for _, c := range batch {
		if c == nil {
			continue
		}
		if msg, ok := c().(snapshotsMsg); ok {
			m, _ = deliver(t, m, msg)
		}
	}
	if len(m.snapshots) != 2 {
		t.Fatalf("the list must be loaded without waiting for the task, got %v", m.snapshots)
	}
}

func TestLostTaskStillReloadsTheSnapshots(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m, _ = deliver(t, m, createSnapshotMsg{vmid: 300, name: "snap1", upid: testUPID})
	var cmd tea.Cmd
	for i := 0; i < maxTaskErrors; i++ {
		m, cmd = deliver(t, m, taskResultMsg{upid: testUPID, err: errors.New("pvesh down")})
	}
	if len(m.tasks) != 0 || cmd == nil || !strings.Contains(m.message, "lost track") {
		t.Fatalf("tasks=%v message=%q cmd=%t", m.tasks, m.message, cmd != nil)
	}
	if _, ok := cmd().(snapshotsMsg); !ok {
		t.Fatal("giving up must reload the snapshot list")
	}
}
