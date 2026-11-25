package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"pvc/internal/proxmox"
)

const aptUPID = "UPID:pve:0001A2B3:00C4D5E6:67890ABC:aptupdate::root@pam:"

// clusterRow is one row of /cluster/tasks, as pvesh prints it.
func clusterRow(upid, typ, id string, end time.Time, status string) string {
	row := fmt.Sprintf(`{"upid":"%s","node":"pve","type":"%s","id":"%s","user":"root@pam","starttime":%d`, upid, typ, id, end.Add(-time.Minute).Unix())
	if !end.IsZero() {
		row += fmt.Sprintf(`,"endtime":%d,"status":"%s"`, end.Unix(), status)
	}
	return row + "}"
}

func clusterRunner(rows ...string) *fakeRunner {
	return &fakeRunner{stdout: map[string]string{
		"/cluster/tasks": "[" + strings.Join(rows, ",") + "]",
		"/log":           `[{"n":1,"t":"Err:1 http://download.proxmox.com/debian trixie InRelease"},{"n":2,"t":"TASK ERROR: apt-get update failed"}]`,
	}}
}

// poll runs one cluster task poll and delivers its result.
func poll(t *testing.T, m Model) Model {
	t.Helper()
	m.clusterTasks.loading = true
	m, _ = deliver(t, m, m.loadClusterTasks())
	return m
}

func TestClusterTaskFailureIsJournaled(t *testing.T) {
	at(t, noon)
	ended := noon.Add(-2 * time.Hour)
	m := newTestModel(clusterRunner(clusterRow(aptUPID, "aptupdate", "", ended, "apt-get update failed")))
	m.message = "stop guest 300: OK"

	m = poll(t, m)
	if len(m.entries) != 1 {
		t.Fatalf("one failure expected, got %+v", m.entries)
	}
	entry := m.entries[0]
	if entry.source != sourceProxmox || entry.kind != "task failed" || entry.context != "task aptupdate" || entry.message != "FAILED: apt-get update failed" {
		t.Fatalf("unexpected entry: %+v", entry)
	}
	if !entry.time.Equal(ended) {
		t.Fatalf("the entry is dated when the task ended (%v), got %v", ended, entry.time)
	}
	detail := strings.Join(entry.detail, "\n")
	for _, want := range []string{"task: " + aptUPID, "type: aptupdate, node: pve, user: root@pam", "exit status: apt-get update failed", "task log:", "  TASK ERROR: apt-get update failed"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail lacks %q:\n%s", want, detail)
		}
	}
	if m.message != "stop guest 300: OK" {
		t.Fatalf("a background failure must not replace the status row, got %q", m.message)
	}
	if m.unreadErrors != 1 {
		t.Fatalf("the failure counts as unread, got %d", m.unreadErrors)
	}
}

func TestClusterTaskFailureIsJournaledOnce(t *testing.T) {
	at(t, noon)
	m := newTestModel(clusterRunner(clusterRow(aptUPID, "aptupdate", "", noon.Add(-time.Hour), "boom")))

	m = poll(t, m)
	m = poll(t, m)
	m = poll(t, m)
	if len(m.entries) != 1 || m.entries[0].count != 1 {
		t.Fatalf("a task already seen must not be journaled again: %+v", m.entries)
	}
}

func TestClusterTasksLogSuccessesAndWarningsWithoutRunningOnes(t *testing.T) {
	at(t, noon)
	end := noon.Add(-time.Hour)
	runner := clusterRunner(
		clusterRow("UPID:pve:0001A2B3:00C4D5E6:67890ABC:vzdump:100:root@pam:", "vzdump", "100", end, "OK"),
		clusterRow("UPID:pve:0001A2B3:00C4D5E6:67890ABC:vzdump:101:root@pam:", "vzdump", "101", end.Add(time.Minute), "WARNINGS: 1"),
		clusterRow("UPID:pve:0001A2B3:00C4D5E6:67890ABC:vncproxy:102:root@pam:", "vncproxy", "102", time.Time{}, ""),
	)
	m := poll(t, newTestModel(runner))
	if len(m.entries) != 2 || m.unreadErrors != 0 {
		t.Fatalf("two results expected, none unread, running tasks are not logged yet: %+v", m.entries)
	}
	first, second := m.entries[0], m.entries[1]
	if !first.ok || first.context != "task vzdump 100" || first.message != "OK" || first.kind != "task ok" || first.source != sourceProxmox {
		t.Fatalf("unexpected success: %+v", first)
	}
	if !second.ok || second.message != "WARNINGS: 1" {
		t.Fatalf("warnings are a success with their text: %+v", second)
	}
	if !first.time.Equal(end) {
		t.Fatalf("dated when the task ended, got %v", first.time)
	}
	if runner.called("/log") {
		t.Fatal("the log of a task that succeeded is not read")
	}
}

func TestClusterTasksKeepOrderAndSameSuccessesApart(t *testing.T) {
	at(t, noon)
	m := newTestModel(clusterRunner(
		clusterRow("UPID:pve:0001A2B3:00C4D5E6:67890ABC:vzdump:100:root@pam:", "vzdump", "100", noon.Add(-2*time.Hour), "OK"),
		clusterRow("UPID:pve:0001A2B3:00C4D5E6:67890ABD:vzdump:100:root@pam:", "vzdump", "100", noon.Add(-time.Hour), "OK"),
	))
	m = poll(t, m)
	if len(m.entries) != 2 || m.entries[0].count != 1 || m.entries[1].count != 1 {
		t.Fatalf("two successes are two entries, never grouped: %+v", m.entries)
	}
	if !m.entries[0].time.Before(m.entries[1].time) {
		t.Fatalf("oldest first: %+v", m.entries)
	}
}

func TestClusterTaskFirstPollIgnoresOldFailures(t *testing.T) {
	at(t, noon)
	runner := clusterRunner(
		clusterRow("UPID:pve:0001A2B3:00C4D5E6:67890ABC:vzdump:100:root@pam:", "vzdump", "100", noon.Add(-30*time.Hour), "old failure"),
		clusterRow(aptUPID, "aptupdate", "", noon.Add(-3*time.Hour), "recent failure"),
	)
	m := poll(t, newTestModel(runner))
	if len(m.entries) != 1 || m.entries[0].message != "FAILED: recent failure" {
		t.Fatalf("only the last 24 hours are imported at start: %+v", m.entries)
	}
	if runner.called("vzdump:100") {
		t.Fatal("the log of an ignored failure must not be read")
	}

	// Once started, a failure is reported whatever its age: it just ended.
	runner.stdout["/cluster/tasks"] = "[" + clusterRow("UPID:pve:0001A2B3:00C4D5E6:67890ABC:vzdump:200:root@pam:", "vzdump", "200", noon.Add(-48*time.Hour), "late") + "]"
	m = poll(t, m)
	if len(m.entries) != 2 {
		t.Fatalf("a failure that appears after the first poll is reported: %+v", m.entries)
	}
}

func TestClusterTaskFollowedByTheTUIIsNotDuplicated(t *testing.T) {
	at(t, noon)
	end := noon.Add(-time.Minute)
	m := newTestModel(clusterRunner(clusterRow(testUPID, "vzdump", "", end, "job errors")))
	m, _ = deliver(t, m, runBackupMsg{vmid: 300, upid: testUPID})

	// The poll sees the failure while the TUI still follows the task: it leaves it alone.
	m = poll(t, m)
	if len(m.entries) != 0 {
		t.Fatalf("a followed task is reported by its own tracking: %+v", m.entries)
	}

	m, _ = deliver(t, m, taskResultMsg{upid: testUPID, task: proxmox.Task{Status: "stopped", ExitStatus: "job errors"}})
	m = poll(t, m)
	if len(m.entries) != 1 || m.entries[0].context != "backup of 300" {
		t.Fatalf("one entry, from the tracking: %+v", m.entries)
	}
}

func TestClusterPollSeesTheFailureOfALostTask(t *testing.T) {
	at(t, noon)
	m := newTestModel(clusterRunner(clusterRow(testUPID, "vzdump", "", noon.Add(-time.Minute), "job errors")))
	m, _ = deliver(t, m, runBackupMsg{vmid: 300, upid: testUPID})
	for i := 0; i < maxTaskErrors; i++ {
		m, _ = deliver(t, m, taskResultMsg{upid: testUPID, err: errors.New("pvesh gone")})
	}
	if len(m.tasks) != 0 {
		t.Fatalf("the task should have been given up: %+v", m.tasks)
	}

	m = poll(t, m)
	found := false
	for _, entry := range m.entries {
		found = found || (entry.kind == "task failed" && entry.message == "FAILED: job errors")
	}
	if !found {
		t.Fatalf("the real failure of a lost task must come from the cluster list: %+v", m.entries)
	}
}

func TestClusterTaskReadErrorIsJournaledQuietly(t *testing.T) {
	at(t, noon)
	runner := &fakeRunner{failing: map[string]error{"/cluster/tasks": pveshFailure("permission denied")}}
	m := newTestModel(runner)
	m.message = "stop guest 300: OK"

	m = poll(t, m)
	m = poll(t, m)
	if len(m.entries) != 1 || m.entries[0].count != 2 || m.entries[0].context != "read cluster tasks" || m.entries[0].source != sourceProxmox {
		t.Fatalf("a read error is journaled, and counted when it repeats: %+v", m.entries)
	}
	if m.message != "stop guest 300: OK" {
		t.Fatalf("the status row must stay, got %q", m.message)
	}
	if m.clusterTasks.primed {
		t.Fatal("a failed poll must not end the first-poll window")
	}
}

func TestTickStartsOnlyOneClusterPoll(t *testing.T) {
	m := newTestModel(clusterRunner())
	m, cmd := deliver(t, m, tickMsg(noon))
	if cmd == nil || !m.clusterTasks.loading {
		t.Fatalf("the tick must start a poll, loading=%t", m.clusterTasks.loading)
	}
	runner := m.client.Runner.(*fakeRunner)
	before := len(runner.calls)
	m, _ = deliver(t, m, tickMsg(noon))
	if len(runner.calls) != before {
		t.Fatal("no poll may start while one is running")
	}

	m, _ = deliver(t, m, clusterTasksMsg{})
	if m.clusterTasks.loading || !m.clusterTasks.primed {
		t.Fatalf("a finished poll frees the next one: %+v", m.clusterTasks)
	}
}

func TestMarkTaskSeenDoesNotMutateEarlierCopies(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	first := m.markTaskSeen("a")
	second := first.markTaskSeen("b")
	if first.clusterTasks.has("b") || !second.clusterTasks.has("a") || m.clusterTasks.has("a") {
		t.Fatal("markTaskSeen must copy the set")
	}
}

func TestDemoShowsTheFailedTaskOfTheTaskPanel(t *testing.T) {
	m := NewWithStartup(DemoClient(), false)
	m.demo = true
	m.width, m.height = 120, 60

	m, _ = execute(t, m, m.loadClusterTasks)
	if len(m.entries) != 2 || m.entries[0].context != "task aptupdate" || m.entries[0].source != sourceProxmox {
		t.Fatalf("the demo has a failed task and a successful one in its panel: %+v", m.entries)
	}

	m.viewMode = viewLog
	// Newest first: the successful backup is on top, the failed update below it.
	m.selectedLog = 1
	out := stripANSI(m.View())
	for _, want := range []string{"task aptupdate", "FAILED", "TASK ERROR: command"} {
		if !strings.Contains(out, want) {
			t.Errorf("the errors view is missing %q:\n%s", want, out)
		}
	}
}
