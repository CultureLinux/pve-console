package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"pvc/internal/proxmox"
)

// mixedLogModel holds two errors and two successes, one of each source.
func mixedLogModel(t *testing.T) Model {
	t.Helper()
	m := richModel()
	m.message = ""
	at(t, time.Date(2026, 11, 25, 8, 0, 0, 0, time.UTC))
	m = m.logError("create snapshot t1 for guest 100", pveshFailure("boom"))
	m = m.logOK(clock(), sourceProxmox, "task ok", "backup of 100", "OK", []string{"task: " + testUPID, "exit status: OK"})
	m = m.logDone("disable TFA of alice@pve", "done")
	m = m.logConsole("invalid input", "create snapshot", "invalid snapshot name")
	m.width, m.height = 120, 34
	return m
}

func TestLogLevelFilterCyclesAllErrorsSuccess(t *testing.T) {
	m := mixedLogModel(t)
	m, _ = press(t, m, "L")
	if len(m.filteredLog()) != 4 {
		t.Fatalf("all entries: %d", len(m.filteredLog()))
	}

	m, _ = press(t, m, "f")
	if m.logLevel != levelErrorsOnly || len(m.filteredLog()) != 2 {
		t.Fatalf("errors only: level=%v shown=%d", m.logLevel, len(m.filteredLog()))
	}
	for _, entry := range m.filteredLog() {
		if entry.ok {
			t.Fatalf("a success in the errors list: %+v", entry)
		}
	}
	if !strings.Contains(m.footerText(), "f level: errors") || !strings.Contains(stripANSI(m.View()), "only errors") {
		t.Fatalf("the level in use is shown: %q", m.footerText())
	}

	m, _ = press(t, m, "f")
	if m.logLevel != levelSuccessOnly || len(m.filteredLog()) != 2 {
		t.Fatalf("success only: level=%v shown=%d", m.logLevel, len(m.filteredLog()))
	}
	for _, entry := range m.filteredLog() {
		if !entry.ok {
			t.Fatalf("an error in the success list: %+v", entry)
		}
	}

	// The level and the source combine.
	m, _ = press(t, m, "tab")
	if got := m.filteredLog(); len(got) != 1 || got[0].source != sourceProxmox || !got[0].ok {
		t.Fatalf("proxmox successes: %+v", got)
	}

	m, _ = press(t, m, "f")
	if m.logLevel != levelAll {
		t.Fatalf("the cycle comes back to all, got %v", m.logLevel)
	}
}

func TestLevelKeyStillFiltersGuestsOutsideTheLog(t *testing.T) {
	m := mixedLogModel(t)
	m, _ = press(t, m, "f")
	if m.filter == "all" || m.logLevel != levelAll {
		t.Fatalf("f is the guest status filter in the guests view: filter=%q level=%v", m.filter, m.logLevel)
	}
}

func TestLogListMarksSuccessesAndErrors(t *testing.T) {
	m := mixedLogModel(t)
	m.viewMode = viewLog
	out := stripANSI(m.View())
	var ok, fail int
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "2026-11-25") {
			continue
		}
		if strings.Contains(line, glyphOK+" ") {
			ok++
		}
		if strings.Contains(line, glyphFail+" ") {
			fail++
		}
	}
	if ok != 2 || fail != 2 {
		t.Fatalf("each line carries its mark: ok=%d fail=%d\n%s", ok, fail, out)
	}
}

func TestLogDetailOfASuccess(t *testing.T) {
	m := mixedLogModel(t)
	m.viewMode = viewLog
	// Newest first: invalid input, disable TFA (pve-console success), backup (proxmox success), error.
	m.selectedLog = 1
	out := stripANSI(m.View())
	for _, want := range []string{glyphOK + " pve-console", "An action of yours ended well", "disable TFA of alice@pve", "done"} {
		if !strings.Contains(out, want) {
			t.Errorf("the detail of a pve-console success lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ended in error") {
		t.Fatalf("a success must not be described as an error:\n%s", out)
	}

	m.selectedLog = 2
	out = stripANSI(m.View())
	for _, want := range []string{glyphOK + " proxmox", "A Proxmox task ended well", "task: " + testUPID} {
		if !strings.Contains(out, want) {
			t.Errorf("the detail of a proxmox success lacks %q:\n%s", want, out)
		}
	}
}

func TestSuccessesNeverRaiseTheUnreadBadge(t *testing.T) {
	m := richModel()
	m.message = "sentinel"
	m = m.logOK(clock(), sourceProxmox, "task ok", "backup of 100", "OK", nil)
	m = m.logDone("disable TFA of alice@pve", "done")
	if m.unreadErrors != 0 || m.message != "sentinel" {
		t.Fatalf("a success is not unread and leaves the status row alone: unread=%d message=%q", m.unreadErrors, m.message)
	}
	m = m.logConsole("invalid input", "create snapshot", "bad")
	if m.unreadErrors != 1 {
		t.Fatalf("an error is unread: %d", m.unreadErrors)
	}
}

func TestSuccessesAreNeverGrouped(t *testing.T) {
	m := richModel()
	m = m.logOK(clock(), sourceProxmox, "task ok", "backup of 100", "OK", nil)
	m = m.logOK(clock(), sourceProxmox, "task ok", "backup of 100", "OK", nil)
	if len(m.entries) != 2 || m.entries[1].count != 1 {
		t.Fatalf("two successes are two entries: %+v", m.entries)
	}

	// An error right after an identical-looking success is not merged into it.
	m = m.logProblem(sourceProxmox, "task failed", "backup of 100", "OK", nil)
	if len(m.entries) != 3 || m.entries[2].ok {
		t.Fatalf("an error is its own entry: %+v", m.entries)
	}
}

func TestFullLogDropsTheOldestSuccessBeforeAnyError(t *testing.T) {
	m := richModel()
	m = m.logConsole("invalid input", "first error", "kept")
	for i := 0; i < maxLogEntries+50; i++ {
		m = m.logOK(clock(), sourceProxmox, "task ok", "task "+string(rune('a'+i%26)), "OK", nil)
	}
	if len(m.entries) != maxLogEntries {
		t.Fatalf("the log is bounded: %d", len(m.entries))
	}
	if m.entries[0].message != "kept" || m.entries[0].ok {
		t.Fatalf("the oldest error survives a burst of successes: %+v", m.entries[0])
	}

	// With only errors left, the oldest entry goes, as before.
	full := richModel()
	for i := 0; i < maxLogEntries+3; i++ {
		full = full.logConsole("invalid input", "ctx", "error "+string(rune('a'+i%26))+string(rune('a'+i/26%26)))
	}
	if len(full.entries) != maxLogEntries || full.entries[0].message == "error aa" {
		t.Fatalf("the oldest error goes when only errors are left: %d, first %q", len(full.entries), full.entries[0].message)
	}
}

func TestAddingToTheLogDoesNotMutateEarlierCopies(t *testing.T) {
	m := richModel()
	m = m.logConsole("invalid input", "a", "one")
	before := m
	after := m.logOK(clock(), sourceConsole, "action done", "b", "done", nil)
	if len(before.entries) != 1 || len(after.entries) != 2 {
		t.Fatalf("before=%d after=%d", len(before.entries), len(after.entries))
	}
}

func TestActionsEndedAtOnceAreLoggedAsPveConsoleSuccesses(t *testing.T) {
	boom := errors.New("boom")
	for name, tt := range map[string]struct {
		msg     any
		context string
	}{
		"backup schedule": {updateBackupScheduleMsg{id: "backup-daily", schedule: "02:30"}, "update backup job backup-daily"},
		"TFA disabled":    {disableTFAMsg{userid: "alice@pve"}, "disable TFA of alice@pve"},
		"snapshot":        {createSnapshotMsg{vmid: 300, name: "t1"}, "create snapshot t1 for guest 300"},
		"guest action":    {destructiveActionMsg{action: "stop-guest", vmid: 300, name: "300"}, "stop guest 300"},
	} {
		t.Run(name, func(t *testing.T) {
			m, _ := deliver(t, newTestModel(&fakeRunner{}), tt.msg)
			if len(m.entries) != 1 {
				t.Fatalf("one entry expected: %+v", m.entries)
			}
			entry := m.entries[0]
			if !entry.ok || entry.source != sourceConsole || entry.kind != "action done" || entry.context != tt.context {
				t.Fatalf("unexpected entry: %+v", entry)
			}
			if m.unreadErrors != 0 {
				t.Fatal("a success is not unread")
			}
		})
	}

	// A failure is still an error, and an action that runs as a task is logged
	// when the task ends, not when it starts.
	m, _ := deliver(t, newTestModel(&fakeRunner{}), disableTFAMsg{userid: "alice@pve", err: boom})
	if len(m.entries) != 1 || m.entries[0].ok {
		t.Fatalf("a failed action is an error: %+v", m.entries)
	}
	m, _ = deliver(t, newTestModel(&fakeRunner{}), createSnapshotMsg{vmid: 300, name: "t1", upid: testUPID})
	if len(m.entries) != 0 {
		t.Fatalf("a task that only started is not a result yet: %+v", m.entries)
	}
	m, _ = deliver(t, m, taskResultMsg{upid: testUPID, task: proxmox.Task{Status: "stopped", ExitStatus: "OK"}})
	if len(m.entries) != 1 || !m.entries[0].ok || m.entries[0].source != sourceProxmox {
		t.Fatalf("the task result is the proxmox success: %+v", m.entries)
	}
}
