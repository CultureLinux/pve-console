package ui

import (
	"strings"
	"testing"

	"pvc/internal/proxmox"
)

func TestHeaderShowsTheViewsAndMarksTheActiveOne(t *testing.T) {
	m := richModel()
	top := stripANSI(m.header(120))
	lines := strings.Split(top, "\n")
	if len(lines) != headerRows {
		t.Fatalf("%d header rows, want %d", len(lines), headerRows)
	}
	for _, want := range []string{"pve-console", "pve", "Proxmox VE 9.0.3", "2 running", "2 stopped"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("row 1 is missing %q: %q", want, lines[0])
		}
	}
	for _, want := range []string{"g Guests 4", "b Backups 1", "u Users 1", "p Updates 1"} {
		if !strings.Contains(lines[1], want) {
			t.Errorf("row 2 is missing %q: %q", want, lines[1])
		}
	}
}

func TestHeaderTabsShowCountsOnlyWhenThereIsSomething(t *testing.T) {
	m := NewWithStartup(proxmox.Client{}, false)
	row := stripANSI(m.headerTabs(120))
	for _, want := range []string{"g Guests", "b Backups", "u Users", "p Updates"} {
		if !strings.Contains(row, want) {
			t.Errorf("missing %q in %q", want, row)
		}
	}
	for _, digit := range "0123456789" {
		if strings.ContainsRune(row, digit) {
			t.Fatalf("no count expected before anything is loaded: %q", row)
		}
	}
}

func TestHeaderTabsFollowTheCurrentView(t *testing.T) {
	m := richModel()
	for _, view := range []string{viewGuests, viewBackups, viewUsers, viewUpdates} {
		m.viewMode = view
		for _, tb := range m.tabs() {
			rendered := m.renderTab(tb)
			active := tb.view == view
			// The active tab is the only one with the key inside its own pill.
			if active && !strings.Contains(stripANSI(rendered), tb.key+" "+tb.label) {
				t.Errorf("active tab %s not rendered", tb.label)
			}
		}
	}
	if len(m.tabs()) != 5 {
		t.Fatalf("%d tabs", len(m.tabs()))
	}
}

func TestHeaderCountersAdaptToTheWidth(t *testing.T) {
	m := richModel()
	full := stripANSI(m.headerTop(120))
	if !strings.Contains(full, "running") || !strings.Contains(full, "stopped") {
		t.Fatalf("wide header: %q", full)
	}
	short := stripANSI(m.headerTop(60))
	if strings.Contains(short, "running") || !strings.Contains(short, "● 2") {
		t.Fatalf("a narrower header uses the short counters: %q", short)
	}
	tiny := stripANSI(m.headerTop(30))
	if textWidth(tiny) > 30 {
		t.Fatalf("the header must never overflow: %q", tiny)
	}
}

func TestHeaderShowsActivity(t *testing.T) {
	m := richModel()
	if got := stripANSI(m.headerTop(120)); strings.Contains(got, "refreshing") || strings.Contains(got, "task") {
		t.Fatalf("an idle header shows no activity: %q", got)
	}
	m.loading = true
	if got := stripANSI(m.headerTop(120)); !strings.Contains(got, "refreshing") {
		t.Fatalf("loading: %q", got)
	}
	m.tasks = []trackedTask{{upid: "a"}}
	if got := stripANSI(m.headerTop(120)); !strings.Contains(got, "1 task") || strings.Contains(got, "tasks") {
		t.Fatalf("one task: %q", got)
	}
	m.tasks = append(m.tasks, trackedTask{upid: "b"})
	if got := stripANSI(m.headerTop(120)); !strings.Contains(got, "2 tasks") {
		t.Fatalf("two tasks: %q", got)
	}
}

func TestFiltersAreShownOnlyWhenActive(t *testing.T) {
	m := richModel()
	if got := m.activeFilters(); got != "" {
		t.Fatalf("defaults show nothing, got %q", got)
	}
	m.filter, m.guestTypeFilter, m.sortBy, m.query = "running", "qemu", "name", "wo"
	got := stripANSI(m.activeFilters())
	for _, want := range []string{"status running", "type qemu", "sort name", "search wo"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	// Guest filters make no sense in the other views: only the search stays.
	m.viewMode = viewUsers
	if got := stripANSI(m.activeFilters()); got != "search wo" {
		t.Fatalf("users view: %q", got)
	}
}

func TestProxmoxVersion(t *testing.T) {
	tests := map[string]string{
		"pve-manager/9.0.3/6e5b4b1a (running kernel: 6.14.8-2-pve)": "9.0.3",
		"pve-manager/9.2.20": "9.2.20",
		"9.0.3":              "9.0.3",
		"":                   "",
	}
	for in, want := range tests {
		if got := proxmoxVersion(in); got != want {
			t.Errorf("proxmoxVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMessageLevels(t *testing.T) {
	tests := []struct {
		message string
		want    level
	}{
		{"backup of 100: OK", levelOK},
		{"start-guest completed", levelOK},
		{"snapshot s1 created for 300", levelOK},
		{"backup started for 100 (running in background) UPID:x", levelOK},
		{"TFA disabled for root@pam", levelOK},
		{"backup of 100: FAILED: job errors", levelError},
		{"pvesh failed: boom", levelError},
		{"lost track of backup of 100: down", levelError},
		{"confirmation mismatch", levelError},
		{"guest 999 was not found as QEMU VM or LXC container", levelError},
		{"invalid snapshot name \"x y\"", levelError},
		{"backup VMIDs are required", levelError},
		{"guest 300 is not running", levelWarn},
		{"loading config for 300", levelInfo},
		{"package updates", levelInfo},
		{"", levelInfo},
	}
	for _, tt := range tests {
		if got := messageLevel(tt.message); got != tt.want {
			t.Errorf("messageLevel(%q) = %d, want %d", tt.message, got, tt.want)
		}
	}
}

func TestStatusRowKeepsItsHeightAndShowsTheLevel(t *testing.T) {
	m := richModel()
	m.message = ""
	empty := m.statusRow(100)
	if textWidth(empty) != 100 || strings.TrimSpace(empty) != "" {
		t.Fatalf("an empty status row keeps the layout stable: %q", empty)
	}

	for message, icon := range map[string]string{
		"backup of 1: OK":        glyphOK,
		"pvesh failed: boom":     glyphFail,
		"guest 1 is not running": glyphWarn,
		"loading config for 1":   glyphInfo,
	} {
		m.message = message
		row := stripANSI(m.statusRow(100))
		if !strings.Contains(row, icon+" "+message) || textWidth(row) != 100 {
			t.Errorf("%q: %q", message, row)
		}
	}

	m.message = strings.Repeat("long ", 80)
	if textWidth(m.statusRow(60)) != 60 {
		t.Fatal("a long message is cut to the width")
	}
}

func TestFitHintsKeepsTheTailAndDropsFromTheEnd(t *testing.T) {
	hints := []hint{{"a", "first"}, {"b", "second"}, {"c", "third"}, {"d", "fourth"}}
	tail := []hint{{"?", "help"}, {"q", "quit"}}

	all := stripANSI(fitHints(hints, tail, 200))
	if all != "a first   b second   c third   d fourth   ? help   q quit" {
		t.Fatalf("everything fits: %q", all)
	}
	narrow := stripANSI(fitHints(hints, tail, 40))
	if !strings.HasSuffix(narrow, "? help   q quit") || !strings.HasPrefix(narrow, "a first") || strings.Contains(narrow, "fourth") {
		t.Fatalf("the least useful hints go first: %q", narrow)
	}
	if textWidth(narrow) > 40 {
		t.Fatalf("width %d > 40: %q", textWidth(narrow), narrow)
	}
	only := stripANSI(fitHints(hints, tail, 15))
	if only != "? help   q quit" {
		t.Fatalf("the tail always stays: %q", only)
	}
	if got := stripANSI(fitHints(hints, nil, 200)); strings.HasSuffix(got, " ") {
		t.Fatalf("no trailing separator: %q", got)
	}
}

func TestHintsDependOnTheContext(t *testing.T) {
	m := richModel()
	text := func() string { return m.footerText() }

	// A stopped guest can be started; a running one stopped, shut down or rebooted.
	m.selected = 0
	m.data.guests[0].Status = "stopped"
	if got := text(); !strings.Contains(got, "+ start") || strings.Contains(got, "- stop") {
		t.Errorf("stopped guest: %q", got)
	}
	m.data.guests[0].Status = "running"
	if got := text(); strings.Contains(got, "+ start") || !strings.Contains(got, "- stop") {
		t.Errorf("running guest: %q", got)
	}
	m.data.guests[0].Status = "stopped"

	m.showSnaps = true
	if got := text(); !strings.Contains(got, "roll back") || !strings.Contains(got, "d delete") {
		t.Errorf("snapshots: %q", got)
	}
	m.showSnaps = false

	m.viewMode = viewBackups
	if got := text(); !strings.Contains(got, "o restore") || !strings.Contains(got, "tab jobs") {
		t.Errorf("backup volumes: %q", got)
	}
	m.backupMode = "jobs"
	if got := text(); !strings.Contains(got, "e edit schedule") || !strings.Contains(got, "tab volumes") {
		t.Errorf("backup jobs: %q", got)
	}

	m.viewMode = viewUsers
	if got := text(); !strings.Contains(got, "x disable TFA") {
		t.Errorf("users: %q", got)
	}
	m.viewMode = viewUpdates
	if got := text(); !strings.Contains(got, "a upgrade") {
		t.Errorf("updates: %q", got)
	}
	m.data.updates = nil
	if got := text(); !strings.Contains(got, "a upgrade") {
		t.Errorf("a refreshes the index first, so it is offered with an empty list: %q", got)
	}

	// Every regular screen ends with help and quit.
	for _, view := range []string{viewGuests, viewBackups, viewUsers, viewUpdates} {
		m.viewMode = view
		if got := text(); !strings.HasSuffix(got, "? help  q quit") {
			t.Errorf("%s: %q", view, got)
		}
	}
}

func TestModalsOwnTheFooter(t *testing.T) {
	m := richModel()
	tests := map[modalMode]string{
		modalStartup:  "enter continue",
		modalHelp:     "esc close",
		modalConfirm:  "enter confirm",
		modalSnapshot: "tab next",
		modalBackup:   "storage",
		modalSchedule: "toggle guest",
	}
	for mode, want := range tests {
		m.modalMode = mode
		got := m.footerText()
		if !strings.Contains(got, want) {
			t.Errorf("%s: %q", mode, got)
		}
		if strings.Contains(got, "q quit") {
			t.Errorf("%s: a modal must not advertise the main keys: %q", mode, got)
		}
	}
	m.modalMode = modalUpgrade
	m.upgradeRunning = true
	if got := m.footerText(); !strings.Contains(got, "upgrade running") {
		t.Errorf("running upgrade: %q", got)
	}
}

func TestSearchPromptReplacesTheHints(t *testing.T) {
	m := richModel()
	m.inputMode, m.input = inputSearch, "wo"
	row := stripANSI(m.hintRow(100))
	if !strings.Contains(row, "/ wo") || !strings.Contains(row, "enter apply") || !strings.Contains(row, "esc cancel") {
		t.Fatalf("search prompt: %q", row)
	}
	if strings.Contains(row, "q quit") || textWidth(row) != 100 {
		t.Fatalf("the prompt takes the whole row: %q", row)
	}
	m.width, m.height = 120, 34
	if strings.Contains(stripANSI(m.View()), "Disable two-factor") {
		t.Fatal("unexpected modal")
	}
}

func TestSpinnerTicks(t *testing.T) {
	m := richModel()
	if m.busy() {
		t.Fatal("an idle model is not busy")
	}
	m.loading = true
	if !m.busy() {
		t.Fatal("loading is busy")
	}
	m.loading = false
	m.tasks = []trackedTask{{upid: "a"}}
	if !m.busy() {
		t.Fatal("a task is busy")
	}
	m.tasks = nil
	m.upgradeRunning = true
	if !m.busy() {
		t.Fatal("an upgrade is busy")
	}

	// The tick keeps itself alive, and Init starts it.
	m2, cmd := deliver(t, richModel(), spinMsg{})
	if cmd == nil || m2.message != richModel().message {
		t.Fatal("a spin tick reschedules itself and changes nothing else")
	}
	if cmd := richModel().Init(); cmd == nil {
		t.Fatal("Init must start the ticks")
	}
}

func TestConfirmModalAdaptsToTheAction(t *testing.T) {
	m := richModel()
	m.width, m.height = 120, 34
	tests := []struct {
		action, title, button string
		danger                bool
	}{
		{"stop-guest", "STOP GUEST", "Stop guest", true},
		{"start-guest", "START GUEST", "Start guest", false},
		{"reboot-guest", "REBOOT GUEST", "Reboot guest", false},
		{"shutdown-guest", "SHUTDOWN GUEST", "Shut down guest", false},
		{"delete-snapshot", "DELETE SNAPSHOT", "Delete snapshot", true},
		{"restore-snapshot", "RESTORE SNAPSHOT", "Roll back", true},
		{"delete-backup", "DELETE BACKUP", "Delete backup", true},
		{"restore-backup", "RESTORE BACKUP", "Restore backup", true},
	}
	for _, tt := range tests {
		m.openConfirm(tt.action, tt.title, "1", 1, "Do it ?")
		out := stripANSI(m.confirmModal(100))
		if !strings.Contains(out, tt.button) || !strings.Contains(out, "esc Cancel") || !strings.Contains(out, sentenceCase(tt.title)) {
			t.Errorf("%s: %s", tt.action, out)
		}
		if danger := strings.Contains(out, "This cannot be undone."); danger != tt.danger {
			t.Errorf("%s: danger warning = %t, want %t", tt.action, danger, tt.danger)
		}
	}
	if got := sentenceCase("  STOP GUEST "); got != "Stop guest" {
		t.Fatalf("sentenceCase = %q", got)
	}
	if got := sentenceCase(""); got != "" {
		t.Fatalf("sentenceCase(\"\") = %q", got)
	}
}

func TestHelpUsesAsFewColumnsAsTheHeightAllows(t *testing.T) {
	m := richModel()
	m.modalMode = modalHelp

	widths := map[string]int{}
	for _, size := range [][2]int{{120, 60}, {120, 30}, {80, 60}, {60, 20}} {
		m.width, m.height = size[0], size[1]
		out := stripANSI(m.helpModal(size[0]))
		lines := strings.Split(out, "\n")
		if len(lines) > size[1] && size[1] >= 30 {
			t.Errorf("%v: the help (%d lines) must fit the screen", size, len(lines))
		}
		for _, line := range lines {
			if textWidth(line) > size[0] {
				t.Errorf("%v: line too wide: %q", size, line)
			}
		}
		widths[strings.Join([]string{string(rune('0' + size[0]/10)), string(rune('0' + size[1]/10))}, "x")] = len(lines)
	}
	m.width, m.height = 120, 60
	tall := len(strings.Split(stripANSI(m.helpModal(120)), "\n"))
	m.height = 30
	short := len(strings.Split(stripANSI(m.helpModal(120)), "\n"))
	if short > tall {
		t.Fatalf("a short terminal must not give a taller help: %d vs %d", short, tall)
	}
}

func TestNothingIsLostWhenTheTerminalIsMissing(t *testing.T) {
	// Before the first resize message the size is 0x0: use sane defaults.
	m := richModel()
	m.width, m.height = 0, 0
	w, h := m.size()
	if w < minWidth || h < minHeight {
		t.Fatalf("default size %dx%d is too small", w, h)
	}
	if lines := strings.Split(m.View(), "\n"); len(lines) != h {
		t.Fatalf("%d lines, want %d", len(lines), h)
	}
}
