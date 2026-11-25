package ui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"pvc/internal/command"
	"pvc/internal/proxmox"
)

// at fixes the clock for a test.
func at(t *testing.T, when time.Time) {
	t.Helper()
	previous := clock
	clock = func() time.Time { return when }
	t.Cleanup(func() { clock = previous })
}

var noon = time.Date(2026, 11, 24, 12, 30, 5, 0, time.UTC)

// pveshFailure is a command that Proxmox refused.
func pveshFailure(stderr string) command.CommandError {
	return command.CommandError{
		Result: command.Result{Command: "pvesh", Args: []string{"create", "/nodes/pve/qemu/300/snapshot", "-snapname", "t1"}, ExitCode: 255, Stderr: []byte(stderr), Duration: 1500 * time.Millisecond},
		Err:    errors.New("exit status 255"),
	}
}

func TestClassifyTellsProxmoxFromPveConsole(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		source errSource
		kind   string
	}{
		{"command refused by Proxmox", pveshFailure("boom"), sourceProxmox, "command failed"},
		{"wrapped command error", fmt.Errorf("unable to do it: %w", pveshFailure("boom")), sourceProxmox, "command failed"},
		{"two failed commands", errors.Join(pveshFailure("a"), pveshFailure("b")), sourceProxmox, "command failed"},
		{"timeout of a command", command.CommandError{Err: context.DeadlineExceeded}, sourceConsole, "timeout"},
		{"plain timeout", fmt.Errorf("wait: %w", context.DeadlineExceeded), sourceConsole, "timeout"},
		{"canceled", context.Canceled, sourceConsole, "canceled"},
		{"program missing", command.CommandError{Err: &exec.Error{Name: "pvesh", Err: exec.ErrNotFound}}, sourceConsole, "program not installed"},
		{"internal", errors.New("invalid character 'x'"), sourceConsole, "internal error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, kind := classify(tt.err)
			if source != tt.source || kind != tt.kind {
				t.Fatalf("classify = %s/%q, want %s/%q", source.label(), kind, tt.source.label(), tt.kind)
			}
		})
	}
	if sourceProxmox.label() != "proxmox" || sourceConsole.label() != "pve-console" {
		t.Fatal("the labels are shown to the user")
	}
}

func TestErrorDetailsDescribeTheCommand(t *testing.T) {
	got := strings.Join(errorDetails(pveshFailure("400 Parameter verification failed.\nsnapname: invalid format\n")), "\n")
	for _, want := range []string{
		"command: pvesh create /nodes/pve/qemu/300/snapshot -snapname t1",
		"exit code: 255",
		"duration: 1.5s",
		"output:",
		"  400 Parameter verification failed.",
		"  snapname: invalid format",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if details := errorDetails(errors.New("plain")); len(details) != 0 {
		t.Fatalf("a plain error has no command details: %v", details)
	}

	joined := errorDetails(errors.Join(pveshFailure("first"), pveshFailure("second")))
	if text := strings.Join(joined, "\n"); !strings.Contains(text, "first") || !strings.Contains(text, "second") || strings.Count(text, "exit code") != 2 {
		t.Fatalf("both commands are described:\n%s", text)
	}

	long := strings.Repeat("line\n", 50)
	lines := errorDetails(pveshFailure(long))
	if len(lines) > 30 || !strings.Contains(strings.Join(lines, "\n"), "…") {
		t.Fatalf("a long output is cut, got %d lines", len(lines))
	}
}

func TestOneLine(t *testing.T) {
	if got := oneLine("  a\n  b\t\tc \r\n"); got != "a b c" {
		t.Fatalf("oneLine = %q", got)
	}
}

func TestLogErrorRecordsAndShowsTheError(t *testing.T) {
	at(t, noon)
	m := newTestModel(&fakeRunner{})
	m = m.logError("create snapshot t1 for guest 300", pveshFailure("400 Parameter verification failed.\nsnapname: invalid"))

	if len(m.entries) != 1 || m.unreadErrors != 1 {
		t.Fatalf("errors=%d unread=%d", len(m.entries), m.unreadErrors)
	}
	entry := m.entries[0]
	if entry.source != sourceProxmox || entry.kind != "command failed" || entry.context != "create snapshot t1 for guest 300" || entry.count != 1 || !entry.time.Equal(noon) {
		t.Fatalf("entry = %+v", entry)
	}
	if strings.Contains(entry.message, "\n") || !strings.Contains(entry.message, "Parameter verification failed") {
		t.Fatalf("the message is one line: %q", entry.message)
	}
	if m.message != entry.message {
		t.Fatalf("the status row shows the message: %q", m.message)
	}
	if len(entry.detail) == 0 {
		t.Fatal("the details come with the entry")
	}
}

func TestRepeatedErrorsAreCountedNotListed(t *testing.T) {
	at(t, noon)
	m := newTestModel(&fakeRunner{})
	m = m.logError("refresh", errors.New("down"))
	at(t, noon.Add(15*time.Second))
	m = m.logError("refresh", errors.New("down"))
	m = m.logError("refresh", errors.New("down"))

	if len(m.entries) != 1 || m.entries[0].count != 3 {
		t.Fatalf("errors=%+v", m.entries)
	}
	if !m.entries[0].time.Equal(noon.Add(15 * time.Second)) {
		t.Fatalf("the time is the last occurrence: %v", m.entries[0].time)
	}
	if m.unreadErrors != 1 {
		t.Fatalf("a repeat is not a new unread error, unread=%d", m.unreadErrors)
	}

	m = m.logError("refresh", errors.New("another"))
	m = m.logError("refresh", errors.New("down"))
	if len(m.entries) != 3 {
		t.Fatalf("an error that is not the previous one is a new entry: %+v", m.entries)
	}
}

func TestErrorWithAnotherSourceIsANewEntry(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m = m.logProblem(sourceProxmox, "command failed", "ctx", "same text", nil)
	m = m.logProblem(sourceConsole, "invalid input", "ctx", "same text", nil)
	if len(m.entries) != 2 {
		t.Fatalf("source is part of the identity: %+v", m.entries)
	}
}

func TestJournalIsBounded(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	for i := 0; i < maxLogEntries+50; i++ {
		m = m.logError(fmt.Sprintf("e%d", i), errors.New("x"))
	}
	if len(m.entries) != maxLogEntries {
		t.Fatalf("%d entries kept, want %d", len(m.entries), maxLogEntries)
	}
	if m.entries[0].context != "e50" || m.entries[maxLogEntries-1].context != fmt.Sprintf("e%d", maxLogEntries+49) {
		t.Fatalf("the oldest go first: %q ... %q", m.entries[0].context, m.entries[maxLogEntries-1].context)
	}
}

func TestLoggingDoesNotChangeEarlierCopiesOfTheModel(t *testing.T) {
	before := newTestModel(&fakeRunner{})
	before = before.logError("a", errors.New("one"))
	after := before.logError("a", errors.New("one")) // a repeat: same slot, new count
	other := before.logError("b", errors.New("two"))

	if before.entries[0].count != 1 || after.entries[0].count != 2 || len(other.entries) != 2 || len(before.entries) != 1 {
		t.Fatalf("a value copy of the model must keep its own journal: %+v / %+v / %+v", before.entries, after.entries, other.entries)
	}
}

func TestQuietLoggingLeavesTheStatusRowAlone(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m.message = "snapshot s1 created for 300"
	m = m.logErrorQuiet("refresh", errors.New("down"))
	if m.message != "snapshot s1 created for 300" || len(m.entries) != 1 {
		t.Fatalf("message=%q errors=%d", m.message, len(m.entries))
	}
}

func TestUnreadStaysZeroWhileLookingAtTheErrors(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m.viewMode = viewLog
	m = m.logError("a", errors.New("x"))
	if m.unreadErrors != 0 || len(m.entries) != 1 {
		t.Fatalf("unread=%d", m.unreadErrors)
	}
}

func TestFilteredErrorsAreNewestFirst(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m = m.logProblem(sourceProxmox, "command failed", "first", "boom one", nil)
	m = m.logProblem(sourceConsole, "invalid input", "second", "bad name", nil)
	m = m.logProblem(sourceProxmox, "task failed", "third", "job errors", nil)

	contexts := func() []string {
		var out []string
		for _, e := range m.filteredLog() {
			out = append(out, e.context)
		}
		return out
	}
	if got := strings.Join(contexts(), ","); got != "third,second,first" {
		t.Fatalf("order = %s", got)
	}

	m.logSource = sourceProxmox
	if got := strings.Join(contexts(), ","); got != "third,first" {
		t.Fatalf("proxmox only = %s", got)
	}
	m.logSource = sourceConsole
	if got := strings.Join(contexts(), ","); got != "second" {
		t.Fatalf("pve-console only = %s", got)
	}

	m.logSource = 0
	for query, want := range map[string]string{"bad": "second", "JOB": "third", "pve-console": "second", "task failed": "third", "first": "first", "nothing": ""} {
		m.query = query
		if got := strings.Join(contexts(), ","); got != want {
			t.Errorf("search %q = %q, want %q", query, got, want)
		}
	}
}

func TestErrorSourceFilterCycles(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	var seen []string
	for i := 0; i < 4; i++ {
		seen = append(seen, m.logSourceLabel())
		m.nextLogSource()
	}
	if strings.Join(seen, ",") != "all,proxmox,pve-console,all" {
		t.Fatalf("filters = %v", seen)
	}
}

func TestErrorCounts(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m = m.logProblem(sourceProxmox, "k", "a", "1", nil)
	m = m.logProblem(sourceProxmox, "k", "b", "2", nil)
	m = m.logProblem(sourceConsole, "k", "c", "3", nil)
	if p, c := m.logCounts(); p != 2 || c != 1 {
		t.Fatalf("counts = %d, %d", p, c)
	}
}

func TestActionContext(t *testing.T) {
	tests := []struct {
		msg  destructiveActionMsg
		want string
	}{
		{destructiveActionMsg{action: "stop-guest", vmid: 300, name: "300"}, "stop guest 300"},
		{destructiveActionMsg{action: "delete-snapshot", vmid: 300, name: "snap1"}, "delete snapshot snap1 (guest 300)"},
		{destructiveActionMsg{action: "restore-backup", vmid: 300, name: "nas:backup/vzdump-qemu-300.vma"}, "restore backup nas:backup/vzdump-qemu-300.vma"},
		{destructiveActionMsg{action: "delete-backup", name: "nas:backup/x"}, "delete backup nas:backup/x"},
	}
	for _, tt := range tests {
		if got := actionContext(tt.msg); got != tt.want {
			t.Errorf("actionContext(%+v) = %q, want %q", tt.msg, got, tt.want)
		}
	}
}

// Every place that used to show an error only in the status row now keeps it.
func TestEveryErrorOfTheModelEndsInTheJournal(t *testing.T) {
	refused := pveshFailure("refused by proxmox")
	boom := errors.New("boom")
	tests := []struct {
		name    string
		msg     tea.Msg
		prepare func(*Model)
		context string
		source  errSource
	}{
		{"snapshots list", snapshotsMsg{vmid: 0, err: refused}, nil, "list snapshots of guest 0", sourceProxmox},
		{"snapshot creation", createSnapshotMsg{vmid: 300, name: "s1", err: refused}, nil, "create snapshot s1 for guest 300", sourceProxmox},
		{"backup start", runBackupMsg{vmid: 300, err: refused}, nil, "start backup of guest 300", sourceProxmox},
		{"backup job", updateBackupScheduleMsg{id: "daily", err: boom}, nil, "update backup job daily", sourceConsole},
		{"configuration", configMsg{vmid: 0, err: refused}, nil, "read configuration of guest 0", sourceProxmox},
		{"TFA entries", userTFAMsg{userid: "", err: refused}, nil, "read TFA entries of ", sourceProxmox},
		{"TFA disabling", disableTFAMsg{userid: "root@pam", err: refused}, nil, "disable TFA of root@pam", sourceProxmox},
		{"users", usersMsg{err: refused}, nil, "list users", sourceProxmox},
		{"guest action", destructiveActionMsg{action: "stop-guest", vmid: 300, name: "300", err: refused}, nil, "stop guest 300", sourceProxmox},
		{"upgrade", upgradeStepDoneMsg{phase: upgradeApply, err: refused}, func(m *Model) { m.upgradeRunning, m.upgradePhase = true, upgradeApply }, "apt dist-upgrade", sourceProxmox},
		{"package index", upgradeStepDoneMsg{phase: upgradeIndex, err: refused}, func(m *Model) { m.upgradeRunning, m.upgradePhase = true, upgradeIndex }, "refresh package index", sourceProxmox},
		{"package list", upgradeListMsg{err: refused}, nil, "list package updates", sourceProxmox},
		{"timeout", destructiveActionMsg{action: "stop-guest", vmid: 1, name: "1", err: command.CommandError{Err: context.DeadlineExceeded}}, nil, "stop guest 1", sourceConsole},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(&fakeRunner{})
			if tt.prepare != nil {
				tt.prepare(&m)
			}
			m, _ = deliver(t, m, tt.msg)
			if len(m.entries) != 1 || m.entries[0].context != tt.context || m.entries[0].source != tt.source {
				t.Fatalf("errors=%+v", m.entries)
			}
			if m.message == "" || m.message != m.entries[0].message {
				t.Fatalf("the status row shows the logged message: %q", m.message)
			}
		})
	}
}

func TestBackgroundRefreshErrorsAreLoggedQuietly(t *testing.T) {
	refused := pveshFailure("pvesh is down")
	m := newTestModel(&fakeRunner{})
	m.message = "backup of 100: OK"

	m, _ = deliver(t, m, loadMsg(snapshot{err: refused}))
	m, _ = deliver(t, m, updatesMsg{err: refused})
	m, _ = deliver(t, m, backupsMsg{err: refused})

	if m.message != "backup of 100: OK" {
		t.Fatalf("a background refresh must not overwrite the status row: %q", m.message)
	}
	contexts := map[string]bool{}
	for _, e := range m.entries {
		contexts[e.context] = true
	}
	for _, want := range []string{"refresh guests and storages", "list package updates", "list backups"} {
		if !contexts[want] {
			t.Errorf("missing %q in %v", want, contexts)
		}
	}
	if m.data.err == nil && m.data.updateErr == nil {
		t.Fatal("the panels still show their own error")
	}
}

func TestInvalidInputIsAPveConsoleError(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m, _ = press(t, m, "n")
	m, _ = press(t, m, "b", "a", "d", " ", "n")
	m, _ = press(t, m, "enter")
	m, cmd := press(t, m, "enter")
	if cmd != nil || len(m.entries) != 1 {
		t.Fatalf("errors=%+v", m.entries)
	}
	entry := m.entries[0]
	if entry.source != sourceConsole || entry.kind != "invalid input" || entry.context != "create snapshot" || !strings.Contains(entry.message, "invalid snapshot name") {
		t.Fatalf("entry = %+v", entry)
	}

	// The same for a schedule without value and for a wrong TFA confirmation.
	m = newTestModel(&fakeRunner{})
	m.openModal(modalSchedule, 0, "daily", "backup-nas")
	m.modalInput, m.modalField = "", 3
	m, _ = press(t, m, "enter")
	if len(m.entries) != 1 || m.entries[0].context != "update backup job daily" || m.entries[0].source != sourceConsole {
		t.Fatalf("errors=%+v", m.entries)
	}
}

func TestAFailedTaskIsAProxmoxErrorWithItsLog(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m, _ = deliver(t, m, runBackupMsg{vmid: 300, upid: testUPID})

	m, _ = deliver(t, m, taskResultMsg{
		upid: testUPID,
		task: proxmox.Task{Status: "stopped", ExitStatus: "job errors", Type: "vzdump"},
		log:  []string{"INFO: starting", "ERROR: no space left on device"},
	})
	if len(m.entries) != 1 {
		t.Fatalf("errors=%+v", m.entries)
	}
	entry := m.entries[0]
	if entry.source != sourceProxmox || entry.kind != "task failed" || entry.context != "backup of 300" || entry.message != "FAILED: job errors" {
		t.Fatalf("entry = %+v", entry)
	}
	if m.message != "backup of 300: FAILED: job errors" {
		t.Fatalf("the status row keeps the full sentence: %q", m.message)
	}
	if false {
		t.Fatalf("entry = %+v", entry)
	}
	details := strings.Join(entry.detail, "\n")
	for _, want := range []string{"task: " + testUPID, "type: vzdump", "exit status: job errors", "task log:", "  ERROR: no space left on device"} {
		if !strings.Contains(details, want) {
			t.Errorf("missing %q in:\n%s", want, details)
		}
	}
}

func TestASuccessfulTaskIsLoggedAsASuccess(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m, _ = deliver(t, m, runBackupMsg{vmid: 300, upid: testUPID})
	m, _ = deliver(t, m, taskResultMsg{upid: testUPID, task: proxmox.Task{Status: "stopped", ExitStatus: "OK"}})
	m, _ = deliver(t, m, runBackupMsg{vmid: 300, upid: testUPID + "x"})
	if len(m.entries) != 1 {
		t.Fatalf("one success expected: %+v", m.entries)
	}
	entry := m.entries[0]
	if !entry.ok || entry.source != sourceProxmox || entry.kind != "task ok" || entry.context != "backup of 300" || entry.message != "OK" {
		t.Fatalf("unexpected entry: %+v", entry)
	}
	if m.unreadErrors != 0 {
		t.Fatalf("a success is not an unread error: %d", m.unreadErrors)
	}
}

func TestALostTaskIsClassifiedByItsCause(t *testing.T) {
	for name, tt := range map[string]struct {
		cause  error
		source errSource
	}{
		"pvesh refused the status call": {pveshFailure("no such task"), sourceProxmox},
		"the call timed out":            {command.CommandError{Err: context.DeadlineExceeded}, sourceConsole},
		"the answer was unreadable":     {errors.New("invalid character"), sourceConsole},
	} {
		t.Run(name, func(t *testing.T) {
			m := newTestModel(&fakeRunner{})
			m, _ = deliver(t, m, runBackupMsg{vmid: 300, upid: testUPID})
			for i := 0; i < maxTaskErrors; i++ {
				m, _ = deliver(t, m, taskResultMsg{upid: testUPID, err: tt.cause})
			}
			if len(m.entries) != 1 {
				t.Fatalf("errors=%+v", m.entries)
			}
			entry := m.entries[0]
			if entry.source != tt.source || entry.kind != "task not followed" || entry.context != "follow backup of 300" || strings.HasPrefix(entry.message, "lost track") {
				t.Fatalf("entry = %+v", entry)
			}
			if !strings.HasPrefix(m.message, "lost track of backup of 300: ") {
				t.Fatalf("the status row says what was lost: %q", m.message)
			}
			if !strings.Contains(strings.Join(entry.detail, "\n"), "task: "+testUPID) {
				t.Fatalf("the task id is in the details: %v", entry.detail)
			}
		})
	}
}

func TestPollingATaskThatFailedReadsItsLog(t *testing.T) {
	runner := &fakeRunner{stdout: map[string]string{
		"/status": `{"status":"stopped","exitstatus":"job errors","type":"vzdump"}`,
		"/log":    `[{"n":1,"t":"INFO: start"},{"n":2,"t":"ERROR: disk full"}]`,
	}}
	m := newTestModel(runner)
	m, _ = deliver(t, m, runBackupMsg{vmid: 300, upid: testUPID})
	m, cmd := deliver(t, m, taskPollMsg{upid: testUPID})
	m, _ = execute(t, m, cmd)

	if len(m.entries) != 1 || !strings.Contains(strings.Join(m.entries[0].detail, "\n"), "ERROR: disk full") {
		t.Fatalf("the failure explains itself with the task log: %+v", m.entries)
	}
	if !runner.called("/log") {
		t.Fatalf("the log must be read: %v", runner.calls)
	}

	// A task that succeeded costs no extra call.
	ok := &fakeRunner{stdout: map[string]string{"/status": `{"status":"stopped","exitstatus":"OK"}`}}
	m = newTestModel(ok)
	m, _ = deliver(t, m, runBackupMsg{vmid: 300, upid: testUPID})
	m, cmd = deliver(t, m, taskPollMsg{upid: testUPID})
	execute(t, m, cmd)
	if ok.called("/log") {
		t.Fatalf("no log for a task that succeeded: %v", ok.calls)
	}
}
