package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"pvc/internal/proxmox"
)

const (
	aptListOne = "Listing...\nlibssl3/stable 3.0.16-1 amd64 [upgradable from: 3.0.15-1]\nqemu-server/stable 9.0.4 amd64 [upgradable from: 9.0.3]\n"
	kernelLine = "Setting up proxmox-kernel-6.14.8-3-pve-signed (6.14.8-3) ..."
)

// upgradeRunner answers the commands of the upgrade procedure. flag says
// whether /var/run/reboot-required exists.
func upgradeRunner(list string, upgradeOutput string, flag bool) *fakeRunner {
	runner := &fakeRunner{
		stdout: map[string]string{
			"apt-get update": "Hit:1 http://deb.debian.org bookworm InRelease\nReading package lists... Done",
			"apt list":       list,
			"apt-get -y":     upgradeOutput,
			"uname -r":       "6.14.8-2-pve\n",
		},
		failing: map[string]error{},
	}
	if !flag {
		runner.failing["reboot-required"] = errors.New("exit status 1")
	}
	return runner
}

// drive runs a command and every command the answers lead to, delivering each
// message, until nothing is left to do.
func drive(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for i := 0; cmd != nil; i++ {
		if i > 500 {
			t.Fatal("the procedure does not end")
		}
		m, cmd = deliver(t, m, cmd())
	}
	return m
}

func callIndex(runner *fakeRunner, part string) int {
	for i, call := range runner.calls {
		if strings.Contains(call, part) {
			return i
		}
	}
	return -1
}

func updatesModel(runner *fakeRunner) Model {
	m := newTestModel(runner)
	m.viewMode = viewUpdates
	return m
}

func TestUpgradeRefreshesTheIndexThenAsksBeforeUpgrading(t *testing.T) {
	runner := upgradeRunner(aptListOne, "Setting up libssl3 (3.0.16-1) ...", false)
	m := updatesModel(runner) // the list is empty: a still refreshes first

	m, cmd := press(t, m, "a")
	if m.modalMode != modalUpgrade || !m.upgradeRunning || m.upgradePhase != upgradeIndex || cmd == nil {
		t.Fatalf("step 1 starts at once: mode=%q running=%t phase=%q", m.modalMode, m.upgradeRunning, m.upgradePhase)
	}
	m = drive(t, m, cmd)

	if m.modalMode != modalConfirm || m.modalAction != "dist-upgrade" || m.upgradeRunning {
		t.Fatalf("the confirmation is mandatory: mode=%q action=%q", m.modalMode, m.modalAction)
	}
	for _, want := range []string{"2 packages", "libssl3", "qemu-server"} {
		if !strings.Contains(m.modalBody, want) {
			t.Errorf("the confirmation lacks %q: %q", want, m.modalBody)
		}
	}
	if len(m.data.updates) != 2 {
		t.Fatalf("the list of the Updates view is fresh: %+v", m.data.updates)
	}
	if runner.called("dist-upgrade") {
		t.Fatalf("nothing is upgraded before the user confirms: %v", runner.calls)
	}
	if callIndex(runner, "apt-get update") > callIndex(runner, "apt list") {
		t.Fatalf("the index comes before the list: %v", runner.calls)
	}

	// Cancelling leaves the packages alone.
	m, cmd = press(t, m, "esc")
	if m.modalMode != modalNone || cmd != nil || runner.called("dist-upgrade") {
		t.Fatalf("esc cancels: mode=%q", m.modalMode)
	}
}

func TestConfirmedUpgradeShowsItsOutputAndEnds(t *testing.T) {
	runner := upgradeRunner(aptListOne, "Unpacking libssl3 ...\nSetting up libssl3 (3.0.16-1) ...", false)
	m := updatesModel(runner)
	m, cmd := press(t, m, "a")
	m = drive(t, m, cmd)

	m, cmd = press(t, m, "enter")
	if m.modalMode != modalUpgrade || !m.upgradeRunning || m.upgradePhase != upgradeApply || cmd == nil || m.upgradeOutput != "" {
		t.Fatalf("step 2 starts on confirmation: mode=%q running=%t phase=%q output=%q", m.modalMode, m.upgradeRunning, m.upgradePhase, m.upgradeOutput)
	}
	// While it runs the modal stays, whatever is pressed.
	for _, key := range []string{"esc", "enter", "a"} {
		var next tea.Cmd
		m, next = press(t, m, key)
		if m.modalMode != modalUpgrade || next != nil {
			t.Fatalf("%s must not close or restart a running upgrade", key)
		}
	}

	m = drive(t, m, cmd)
	if m.upgradeRunning || m.upgradeErr != nil || m.upgradeReboot != "" {
		t.Fatalf("running=%t err=%v reboot=%q", m.upgradeRunning, m.upgradeErr, m.upgradeReboot)
	}
	if !strings.Contains(m.upgradeOutput, "Unpacking libssl3") || !strings.Contains(m.upgradeOutput, "Setting up libssl3") {
		t.Fatalf("every line is kept: %q", m.upgradeOutput)
	}
	if m.message != "apt dist-upgrade completed" {
		t.Fatalf("message = %q", m.message)
	}
	if out := stripANSI(m.modal(100)); !strings.Contains(out, "Setting up libssl3") || !strings.Contains(out, "Upgrade completed") || strings.Contains(out, "Reboot required") {
		t.Fatalf("the result is shown, without a reboot warning:\n%s", out)
	}
	if callIndex(runner, "apt-get update") > callIndex(runner, "apt-get -y") {
		t.Fatalf("index, then upgrade: %v", runner.calls)
	}

	last := m.entries[len(m.entries)-1]
	if !last.ok || last.context != "apt dist-upgrade" || last.message != "completed" || last.source != sourceConsole {
		t.Fatalf("the log keeps the result: %+v", last)
	}
	if !strings.Contains(strings.Join(last.detail, "\n"), "Setting up libssl3") {
		t.Fatalf("with the end of the output: %v", last.detail)
	}

	m, _ = press(t, m, "enter")
	if m.modalMode != modalNone {
		t.Fatal("enter closes the finished upgrade")
	}
}

func TestUpgradeWarnsWhenANewKernelNeedsAReboot(t *testing.T) {
	m := updatesModel(upgradeRunner(aptListOne, "Unpacking ...\n"+kernelLine+"\nSetting up libssl3 (3.0.16-1) ...", false))
	m, cmd := press(t, m, "a")
	m = drive(t, m, cmd)
	m, cmd = press(t, m, "enter")
	m = drive(t, m, cmd)

	if !strings.Contains(m.upgradeReboot, "kernel 6.14.8-3-pve installed, kernel 6.14.8-2-pve running") {
		t.Fatalf("reboot = %q", m.upgradeReboot)
	}
	out := stripANSI(m.modal(100))
	for _, want := range []string{"Reboot required", "6.14.8-3-pve", "Setting up libssl3"} {
		if !strings.Contains(out, want) {
			t.Errorf("the modal lacks %q:\n%s", want, out)
		}
	}
	// The warning is the last thing before the footer.
	if strings.Index(out, "Reboot required") < strings.Index(out, "Setting up libssl3") || strings.Index(out, "Reboot required") > strings.Index(out, "close") {
		t.Fatalf("the warning comes at the end:\n%s", out)
	}
	if !strings.Contains(m.message, "reboot required") || messageLevel(m.message) != levelWarn {
		t.Fatalf("the status row warns: %q (%v)", m.message, messageLevel(m.message))
	}
	last := m.entries[len(m.entries)-1]
	if !last.ok || last.message != "completed, reboot required" || !strings.Contains(strings.Join(last.detail, "\n"), "reboot required: kernel 6.14.8-3-pve installed") {
		t.Fatalf("the log keeps the warning: %+v", last)
	}
}

func TestUpgradeWarnsWhenTheSystemAsksForAReboot(t *testing.T) {
	m := updatesModel(upgradeRunner(aptListOne, "Setting up libc6 ...", true))
	m, cmd := press(t, m, "a")
	m = drive(t, m, cmd)
	m, cmd = press(t, m, "enter")
	m = drive(t, m, cmd)
	if !strings.Contains(m.upgradeReboot, "/var/run/reboot-required") || !strings.Contains(stripANSI(m.modal(100)), "Reboot required") {
		t.Fatalf("the flag file is enough: %q", m.upgradeReboot)
	}
}

func TestUpgradeWithNothingToDoSaysSoAndDoesNotAsk(t *testing.T) {
	runner := upgradeRunner("Listing...\n", "", false)
	m := updatesModel(runner)
	m, cmd := press(t, m, "a")
	m = drive(t, m, cmd)

	if m.modalMode != modalUpgrade || m.upgradeRunning || m.upgradeNote == "" {
		t.Fatalf("mode=%q running=%t note=%q", m.modalMode, m.upgradeRunning, m.upgradeNote)
	}
	if !strings.Contains(stripANSI(m.modal(100)), "Everything is up to date") || runner.called("dist-upgrade") {
		t.Fatal("it says so, and upgrades nothing")
	}
	m, _ = press(t, m, "enter")
	if m.modalMode != modalNone {
		t.Fatal("enter closes")
	}
}

func TestFailedIndexRefreshStopsBeforeAnyUpgrade(t *testing.T) {
	runner := upgradeRunner(aptListOne, "x", false)
	runner.failing["apt-get update"] = pveshFailure("E: Failed to fetch https://enterprise.proxmox.com 401  Unauthorized")
	m := updatesModel(runner)
	m, cmd := press(t, m, "a")
	m = drive(t, m, cmd)

	if m.upgradeRunning || m.upgradeErr == nil || m.modalMode != modalUpgrade {
		t.Fatalf("the error stays on screen: running=%t err=%v mode=%q", m.upgradeRunning, m.upgradeErr, m.modalMode)
	}
	if runner.called("apt list") || runner.called("dist-upgrade") {
		t.Fatalf("no list and no upgrade after a failed refresh: %v", runner.calls)
	}
	if last := m.entries[len(m.entries)-1]; last.ok || last.context != "refresh package index" {
		t.Fatalf("the error is logged: %+v", last)
	}
	if !strings.Contains(stripANSI(m.modal(100)), "refresh the package index") {
		t.Fatalf("the modal says what failed:\n%s", stripANSI(m.modal(100)))
	}
	m, _ = press(t, m, "enter")
	if m.modalMode != modalNone {
		t.Fatal("enter closes the error")
	}
}

func TestFailedUpgradeIsReportedAndTheListReloaded(t *testing.T) {
	runner := upgradeRunner(aptListOne, "x", false)
	runner.failing["apt-get -y"] = pveshFailure("E: Sub-process /usr/bin/dpkg returned an error code (1)")
	m := updatesModel(runner)
	m, cmd := press(t, m, "a")
	m = drive(t, m, cmd)
	m, cmd = press(t, m, "enter")
	m = drive(t, m, cmd)

	if m.upgradeRunning || m.upgradeErr == nil || m.upgradeReboot != "" {
		t.Fatalf("running=%t err=%v reboot=%q", m.upgradeRunning, m.upgradeErr, m.upgradeReboot)
	}
	if last := m.entries[len(m.entries)-1]; last.ok || last.context != "apt dist-upgrade" {
		t.Fatalf("the error is logged: %+v", last)
	}
	if runner.called("reboot-required") {
		t.Fatal("no reboot check after a failure")
	}
	out := stripANSI(m.modal(100))
	if strings.Contains(out, "Upgrade completed") || strings.Contains(out, "Reboot required") {
		t.Fatalf("a failure is not a success:\n%s", out)
	}
}

func TestUpgradeModalIsBoundedAndShowsTheEndOfTheOutput(t *testing.T) {
	m := NewWithStartup(proxmox.Client{}, false)
	m.modalMode, m.upgradePhase, m.upgradeRunning = modalUpgrade, upgradeApply, true
	for i := 1; i <= 2*maxUpgradeLines; i++ {
		m = m.appendUpgradeLine(fmt.Sprintf("line %d", i))
	}
	if got := strings.Count(m.upgradeOutput, "\n") + 1; got != maxUpgradeLines {
		t.Fatalf("memory is bounded: %d lines", got)
	}
	out := stripANSI(m.modal(100))
	if !strings.Contains(out, fmt.Sprintf("line %d", 2*maxUpgradeLines)) || strings.Contains(out, "line 1\n") || !strings.Contains(out, "earlier lines") {
		t.Fatalf("the modal follows the end of the output:\n%s", out)
	}
	if lines := strings.Count(out, "\n") + 1; lines > shownUpgradeLines+12 {
		t.Fatalf("the modal keeps its height: %d lines", lines)
	}
}

func TestUpgradeConfirmationText(t *testing.T) {
	few := upgradeConfirmBody([]proxmox.PackageUpdate{{Name: "libssl3"}})
	if !strings.Contains(few, "1 package ?") || strings.Contains(few, "more") || !strings.Contains(few, "may be needed") {
		t.Fatalf("%q", few)
	}

	var many []proxmox.PackageUpdate
	for i := 0; i < listedUpgrades+5; i++ {
		many = append(many, proxmox.PackageUpdate{Name: fmt.Sprintf("pkg%d", i)})
	}
	many = append(many, proxmox.PackageUpdate{Name: "proxmox-kernel-6.14"})
	body := upgradeConfirmBody(many)
	if !strings.Contains(body, fmt.Sprintf("%d packages", len(many))) || !strings.Contains(body, "(+6 more)") || strings.Contains(body, "pkg8") || !strings.Contains(body, "reboot will be needed") {
		t.Fatalf("a kernel far down the list is still announced: %q", body)
	}
}

func TestLateUpgradeMessagesAreIgnored(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	before := m
	m, cmd := deliver(t, m, upgradeLineMsg{line: "stray"})
	if cmd != nil || m.upgradeOutput != "" {
		t.Fatal("a line without a running upgrade is dropped")
	}
	m, cmd = deliver(t, m, upgradeStepDoneMsg{phase: upgradeApply})
	if cmd != nil || m.message != before.message || len(m.entries) != 0 {
		t.Fatal("a result without a running upgrade is dropped")
	}

	m.upgradeRunning, m.upgradePhase = true, upgradeApply
	m, cmd = deliver(t, m, upgradeStepDoneMsg{phase: upgradeIndex})
	if cmd != nil || !m.upgradeRunning {
		t.Fatal("the result of another step is dropped")
	}
}

func TestUpgradeFeedThatEndsWithoutAResultIsReported(t *testing.T) {
	feed := make(chan tea.Msg)
	close(feed)
	msg := waitUpgrade(feed, upgradeApply)()
	done, ok := msg.(upgradeStepDoneMsg)
	if !ok || !errors.Is(done.err, errUpgradeInterrupted) || done.phase != upgradeApply {
		t.Fatalf("got %#v", msg)
	}
}

func TestDemoUpgradeStreamsAndWarnsAboutTheNewKernel(t *testing.T) {
	previous := demoLineDelay
	demoLineDelay = 0
	t.Cleanup(func() { demoLineDelay = previous })

	m := NewWithStartup(DemoClient(), false)
	m.demo = true
	m.width, m.height = 120, 40
	m.viewMode = viewUpdates

	m, cmd := press(t, m, "a")
	m = drive(t, m, cmd)
	if m.modalMode != modalConfirm || !strings.Contains(m.modalBody, "3 packages") || !strings.Contains(m.modalBody, "kernel") {
		t.Fatalf("the demo asks first, and announces the kernel: mode=%q body=%q", m.modalMode, m.modalBody)
	}
	m, cmd = press(t, m, "enter")
	m = drive(t, m, cmd)

	out := stripANSI(m.View())
	for _, want := range []string{"Reboot required", "kernel 6.14.8-3-pve installed", "Setting up qemu-server"} {
		if !strings.Contains(out, want) {
			t.Errorf("the demo result lacks %q:\n%s", want, out)
		}
	}
	if len(m.data.updates) != 0 {
		t.Fatalf("nothing is left to upgrade in the demo: %+v", m.data.updates)
	}
}
