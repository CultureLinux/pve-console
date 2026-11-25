package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"pvc/internal/command"
)

// errorsModel has one error of each kind, at known times.
func errorsModel(t *testing.T) Model {
	t.Helper()
	m := richModel()
	m.message = ""
	at(t, time.Date(2026, 11, 24, 9, 15, 2, 0, time.UTC))
	m = m.logError("create snapshot before-upgrade for guest 100", pveshFailure("400 Parameter verification failed.\nsnapname: snapshot 'before-upgrade' already exists"))
	at(t, time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC))
	m = m.logConsole("invalid input", "create snapshot", "invalid snapshot name \"bad name\"")
	at(t, time.Date(2026, 11, 24, 11, 45, 30, 0, time.UTC))
	m = m.logProblem(sourceProxmox, "task failed", "backup of 103", "FAILED: job errors",
		[]string{"task: " + testUPID, "task log:", "  ERROR: no such logical volume pve/vm-103-disk-0"})
	m.width, m.height = 120, 30
	return m
}

func TestErrorsTabShowsTheCountAndTheUnreadOnes(t *testing.T) {
	m := errorsModel(t)
	row := stripANSI(m.headerTabs(120))
	if !strings.Contains(row, "L Log 3") {
		t.Fatalf("unread errors are counted on the tab: %q", row)
	}

	m, _ = press(t, m, "L")
	if m.viewMode != viewLog || m.unreadErrors != 0 {
		t.Fatalf("view=%s unread=%d", m.viewMode, m.unreadErrors)
	}
	if row := stripANSI(m.headerTabs(120)); !strings.Contains(row, "L Log 3") {
		t.Fatalf("the active tab shows the total: %q", row)
	}

	m, _ = press(t, m, "g")
	if row := stripANSI(m.headerTabs(120)); !strings.Contains(row, "L Log 3") {
		t.Fatalf("once read, the tab keeps the total: %q", row)
	}

	empty := richModel()
	if row := stripANSI(empty.headerTabs(120)); !strings.Contains(row, "L Log") || strings.Contains(row, "L Log 0") {
		t.Fatalf("no count without errors: %q", row)
	}
}

func TestErrorsViewKeys(t *testing.T) {
	m := errorsModel(t)
	m, _ = press(t, m, "L")

	// Newest first: the failed backup is on top.
	entry, ok := m.selectedLogEntry()
	if !ok || entry.context != "backup of 103" {
		t.Fatalf("selected %+v", entry)
	}
	m, _ = press(t, m, "j")
	if entry, _ := m.selectedLogEntry(); entry.context != "create snapshot" {
		t.Fatalf("selected %+v", entry)
	}
	m, _ = press(t, m, "j", "j", "j")
	if m.selectedLog != 2 {
		t.Fatalf("the cursor stops at the last entry, got %d", m.selectedLog)
	}
	m, _ = press(t, m, "k", "k", "k")
	if m.selectedLog != 0 {
		t.Fatalf("the cursor stops at the first entry, got %d", m.selectedLog)
	}

	m, _ = press(t, m, "tab")
	if m.logSource != sourceProxmox || len(m.filteredLog()) != 2 {
		t.Fatalf("source=%v shown=%d", m.logSource, len(m.filteredLog()))
	}
	m, _ = press(t, m, "tab")
	if m.logSource != sourceConsole || len(m.filteredLog()) != 1 {
		t.Fatalf("source=%v shown=%d", m.logSource, len(m.filteredLog()))
	}
	m, _ = press(t, m, "tab")
	if m.logSource != 0 || len(m.filteredLog()) != 3 {
		t.Fatalf("source=%v shown=%d", m.logSource, len(m.filteredLog()))
	}

	m, _ = press(t, m, "/", "d", "i", "s", "k")
	m, _ = press(t, m, "enter")
	if got := m.filteredLog(); len(got) != 0 {
		// Nothing says "disk": the filter message is shown instead of a list.
		t.Fatalf("search matched %v", got)
	}
	if out := stripANSI(m.View()); !strings.Contains(out, "No entry matches") {
		t.Fatalf("an empty result explains itself:\n%s", out)
	}
	m, _ = press(t, m, "c")
	if len(m.filteredLog()) != 3 {
		t.Fatal("c clears the search")
	}

	m, _ = press(t, m, "x")
	if len(m.entries) != 0 || !strings.Contains(m.message, "cleared (3 entries)") {
		t.Fatalf("errors=%d message=%q", len(m.entries), m.message)
	}
	if out := stripANSI(m.View()); !strings.Contains(out, "Nothing logged since pve-console started") {
		t.Fatalf("an empty journal says so:\n%s", out)
	}

	m, _ = press(t, m, "esc")
	if m.viewMode != viewGuests {
		t.Fatalf("esc goes back to the guests, got %s", m.viewMode)
	}
}

func TestOtherViewsDoNotClearTheLog(t *testing.T) {
	m := errorsModel(t)
	for _, view := range []string{viewGuests, viewUsers, viewUpdates, viewBackups} {
		m.viewMode = view
		m, _ = press(t, m, "x")
		if len(m.entries) != 3 {
			t.Fatalf("x in %s must not clear the log", view)
		}
	}
}

func TestErrorsListShowsDateSourceAndWhat(t *testing.T) {
	m := errorsModel(t)
	m.viewMode = viewLog
	out := stripANSI(m.View())

	for _, want := range []string{
		"Log", "all 3", "proxmox 2", "pve-console 1",
		"TIME", "SOURCE", "WHAT",
		"2026-11-24 11:45:30", "2026-11-24 10:00:00", "2026-11-24 09:15:02",
		"backup of 103: FAILED: job errors",
		"create snapshot: invalid snapshot name",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the list is missing %q:\n%s", want, out)
		}
	}
	// Both sources are told apart on every line.
	lines := strings.Split(out, "\n")
	var proxmox, console int
	for _, line := range lines {
		if strings.Contains(line, "2026-11-24") {
			if strings.Contains(line, " proxmox ") {
				proxmox++
			}
			if strings.Contains(line, " pve-console ") {
				console++
			}
		}
	}
	if proxmox != 2 || console != 1 {
		t.Fatalf("%d proxmox and %d pve-console lines:\n%s", proxmox, console, out)
	}
	// Newest first.
	if strings.Index(out, "11:45:30") > strings.Index(out, "09:15:02") {
		t.Fatal("the most recent error comes first")
	}
}

func TestErrorsListShowsRepetitionsAndShortTimesWhenNarrow(t *testing.T) {
	at(t, noon)
	m := richModel()
	for i := 0; i < 4; i++ {
		m = m.logError("refresh guests and storages", errors.New("pvesh is down"))
	}
	m.viewMode = viewLog

	wide := stripANSI(m.logPanel(100, 12))
	if !strings.Contains(wide, "×4") || !strings.Contains(wide, "2026-11-24 12:30:05") {
		t.Fatalf("wide:\n%s", wide)
	}
	narrow := stripANSI(m.logPanel(50, 12))
	if strings.Contains(narrow, "2026-11-24") || !strings.Contains(narrow, "12:30:05") {
		t.Fatalf("a narrow list shows the time only:\n%s", narrow)
	}
}

func TestErrorDetailExplainsWhoReportedIt(t *testing.T) {
	m := errorsModel(t)
	m.viewMode = viewLog

	tests := []struct {
		selected int
		want     []string
	}{
		{0, []string{"proxmox", "task failed", "A Proxmox task ended in error.", "ERROR: no such logical volume", "task: " + testUPID, "DETAILS", "MESSAGE"}},
		{1, []string{"pve-console", "invalid input", "Raised by pve-console itself, not by Proxmox.", "invalid snapshot name"}},
		{2, []string{"proxmox", "command failed", "Reported by Proxmox: a command run on the node failed.", "command: pvesh create", "exit code: 255", "snapname: snapshot 'before-upgrade' already exists", "2026-11-24 09:15:02"}},
	}
	for _, tt := range tests {
		m.selectedLog = tt.selected
		out := stripANSI(m.logDetailPanel(70, 28))
		for _, want := range tt.want {
			if !strings.Contains(out, want) {
				t.Errorf("entry %d: missing %q:\n%s", tt.selected, want, out)
			}
		}
	}
	// A console error has no command details.
	m.selectedLog = 1
	if out := stripANSI(m.logDetailPanel(70, 28)); strings.Contains(out, "DETAILS") {
		t.Fatalf("no details for an input error:\n%s", out)
	}
}

func TestErrorDetailShowsRepetitionsAndCutsLongOnes(t *testing.T) {
	at(t, noon)
	m := richModel()
	var detail []string
	for i := 0; i < 80; i++ {
		detail = append(detail, fmt.Sprintf("line %02d", i))
	}
	m = m.logProblem(sourceProxmox, "command failed", "a very long failure", "message", detail)
	m = m.logProblem(sourceProxmox, "command failed", "a very long failure", "message", detail)
	m.viewMode = viewLog

	out := stripANSI(m.logDetailPanel(60, 16))
	if !strings.Contains(out, "(2 times, last one)") {
		t.Fatalf("repetitions are shown:\n%s", out)
	}
	if !strings.Contains(out, "more lines") || strings.Contains(out, "line 79") {
		t.Fatalf("a long detail is cut and says so:\n%s", out)
	}
	if lines := strings.Split(out, "\n"); len(lines) != 16 {
		t.Fatalf("the panel keeps its height, got %d", len(lines))
	}
}

func TestErrorDetailWithoutSelection(t *testing.T) {
	m := richModel()
	m.viewMode = viewLog
	if out := stripANSI(m.logDetailPanel(60, 10)); !strings.Contains(out, "Select an entry") {
		t.Fatalf("%s", out)
	}
}

func TestStatusRowPointsToTheErrorViewOnlyForErrors(t *testing.T) {
	m := errorsModel(t)
	m.viewMode = viewGuests

	m.message = "pvesh failed: boom"
	if row := stripANSI(m.statusRow(100)); !strings.Contains(row, "L details") || !strings.HasSuffix(strings.TrimSpace(row), "L details") {
		t.Fatalf("an error points to the log: %q", row)
	}
	m.message = "backup of 100: OK"
	if row := stripANSI(m.statusRow(100)); strings.Contains(row, "L details") {
		t.Fatalf("a success does not: %q", row)
	}
	m.message = "pvesh failed: boom"
	m.viewMode = viewLog
	if row := stripANSI(m.statusRow(100)); strings.Contains(row, "L details") {
		t.Fatalf("already in the log: %q", row)
	}
	m.viewMode = viewGuests
	m.entries = nil
	if row := stripANSI(m.statusRow(100)); strings.Contains(row, "L details") {
		t.Fatalf("an empty log has nothing to show: %q", row)
	}
}

// Messages from stderr can hold line breaks: they must not break the screen.
func TestMultilineMessagesStayOnOneRow(t *testing.T) {
	m := errorsModel(t)
	m.viewMode = viewGuests
	m.message = "pvesh failed:\n400 Parameter verification failed.\nsnapname: invalid\n"
	row := m.statusRow(100)
	if strings.Contains(row, "\n") || textWidth(row) != 100 {
		t.Fatalf("row=%q", row)
	}
	if lines := strings.Split(m.View(), "\n"); len(lines) != 30 {
		t.Fatalf("the screen keeps its height, got %d", len(lines))
	}
}

func TestErrorsViewStacksTheListAboveTheDetail(t *testing.T) {
	m := errorsModel(t)
	m.viewMode = viewLog

	for _, size := range [][2]int{{80, 30}, {120, 34}, {200, 50}} {
		m.width, m.height = size[0], size[1]
		out := stripANSI(m.View())
		for _, want := range []string{"Log", "TIME", "MESSAGE", "A Proxmox task ended in error."} {
			if !strings.Contains(out, want) {
				t.Errorf("%v: missing %q:\n%s", size, want, out)
			}
		}
		// The list is above the detail, and both use the full width (the task id fits on one line).
		if strings.Index(out, "TIME") > strings.Index(out, "MESSAGE") {
			t.Errorf("%v: the list must come first", size)
		}
	}
	m.width, m.height = 120, 34
	if out := stripANSI(m.View()); !strings.Contains(out, "task: "+testUPID) {
		t.Fatalf("the task id is not cut at full width:\n%s", out)
	}

	// Too short to stack: only the list, never a broken layout.
	m.width, m.height = 80, 14
	short := stripANSI(m.View())
	if strings.Contains(short, "MESSAGE") || !strings.Contains(short, "TIME") {
		t.Fatalf("a short terminal shows the list only:\n%s", short)
	}
}

func TestErrorsViewHintsAndHelp(t *testing.T) {
	m := errorsModel(t)
	m.viewMode = viewLog
	hints := m.footerText()
	for _, want := range []string{"tab source: all", "f level: all", "x clear log", "/ search", "? help"} {
		if !strings.Contains(hints, want) {
			t.Errorf("missing %q in %q", want, hints)
		}
	}
	m.logSource = sourceProxmox
	if got := m.footerText(); !strings.Contains(got, "source: proxmox") {
		t.Errorf("the hint shows the active filter: %q", got)
	}

	m.modalMode, m.width, m.height = modalHelp, 140, 60
	help := stripANSI(m.View())
	for _, want := range []string{"log (errors, results)", "filter by source", "errors / successes", "clear the log"} {
		if !strings.Contains(help, want) {
			t.Errorf("help is missing %q:\n%s", want, help)
		}
	}
}

func TestDemoErrorsCanBeReproduced(t *testing.T) {
	m := NewWithStartup(DemoClient(), false)
	m.demo = true
	m.width, m.height = 120, 34
	m, _ = execute(t, m, m.load)

	// Proxmox refuses to start guest 103 (locked): a command error.
	m.selected = 3
	m, _ = press(t, m, "+")
	m, cmd := press(t, m, "enter")
	m, _ = execute(t, m, cmd)

	// The snapshot name already exists: Proxmox refuses the VM, the container path does not exist.
	m.selected = 0
	m, _ = press(t, m, "n")
	for _, r := range "before-upgrade" {
		m, _ = press(t, m, string(r))
	}
	m, _ = press(t, m, "enter")
	m, cmd = press(t, m, "enter")
	m, _ = execute(t, m, cmd)

	// The backup of guest 103 is accepted, then its task fails.
	m.selected = 3
	m, _ = press(t, m, "m")
	m, _ = press(t, m, "enter")
	m, cmd = press(t, m, "enter")
	m, _ = execute(t, m, cmd)
	for i := 0; i < 3 && len(m.tasks) > 0; i++ {
		m, cmd = deliver(t, m, taskPollMsg{upid: m.tasks[0].upid})
		m, _ = execute(t, m, cmd)
	}

	// An invalid name is pve-console's own error.
	m.selected = 0
	m, _ = press(t, m, "n")
	m, _ = press(t, m, "a", " ", "b", "enter", "enter")

	var proxmox, console int
	for _, e := range m.entries {
		if e.source == sourceProxmox {
			proxmox++
		} else {
			console++
		}
	}
	if proxmox != 3 || console != 1 {
		t.Fatalf("%d proxmox and %d pve-console errors: %+v", proxmox, console, m.entries)
	}
	joined := ""
	for _, e := range m.entries {
		joined += e.message + "\n" + strings.Join(e.detail, "\n") + "\n"
	}
	for _, want := range []string{"got timeout", "already exists", "no such logical volume", "invalid snapshot name"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the demo errors should mention %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "not available") {
		t.Fatalf("the guest type that does not match must say what Proxmox says, not a demo message:\n%s", joined)
	}
	if strings.Contains(joined, "was not found") {
		t.Fatalf("the real Proxmox error must not be replaced by 'not found':\n%s", joined)
	}

	m.viewMode = viewLog
	for _, line := range strings.Split(m.View(), "\n") {
		if textWidth(line) > 120 {
			t.Fatalf("line too wide: %q", line)
		}
	}
}

func TestErrorsFromTimeoutsAreNotBlamedOnProxmox(t *testing.T) {
	m := richModel()
	m = m.logError("stop guest 300", command.CommandError{
		Result: command.Result{Command: "pvesh", Args: []string{"create", "/nodes/pve/qemu/300/status/stop"}, ExitCode: -1},
		Err:    context.DeadlineExceeded,
	})
	if e := m.entries[0]; e.source != sourceConsole || e.kind != "timeout" {
		t.Fatalf("entry = %+v", e)
	}
	// The command line is still shown: it tells what was running when time ran out.
	if !strings.Contains(strings.Join(m.entries[0].detail, "\n"), "command: pvesh create /nodes/pve/qemu/300/status/stop") {
		t.Fatalf("details = %v", m.entries[0].detail)
	}
}

// The header of the table lines up with its rows and never overflows.
func TestErrorsTableHeaderLinesUpWithTheRows(t *testing.T) {
	m := errorsModel(t)
	m.viewMode = viewLog
	lines := strings.Split(stripANSI(m.logPanel(116, 12)), "\n")
	header, first := lines[1], lines[3]
	if strings.Contains(header, "…") {
		t.Fatalf("the header must not be cut: %q", header)
	}
	if hi, ri := columnOf(header, "SOURCE"), columnOf(first, "proxmox"); hi != ri {
		t.Fatalf("SOURCE and the sources are not aligned (%d vs %d):\n%s\n%s", hi, ri, header, first)
	}
	if hi, ri := columnOf(header, "TIME"), columnOf(first, "2026"); hi != ri {
		t.Fatalf("TIME and the dates are not aligned (%d vs %d):\n%s\n%s", hi, ri, header, first)
	}
}

// columnOf is the terminal column (not the byte offset) where needle starts.
func columnOf(line string, needle string) int {
	i := strings.Index(line, needle)
	if i < 0 {
		return -1
	}
	return textWidth(line[:i])
}
