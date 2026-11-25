package ui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"pvc/internal/update"
	"pvc/internal/version"
)

// fakeUpdater answers the checks and the installations of a test.
type fakeUpdater struct {
	mu         sync.Mutex
	info       update.Info
	checkErr   error
	installErr error
	result     update.Result
	steps      []string
	checks     int
	installs   int
}

func (f *fakeUpdater) Check(context.Context) (update.Info, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks++
	return f.info, f.checkErr
}

func (f *fakeUpdater) Install(_ context.Context, _ update.Info, progress func(string)) (update.Result, error) {
	f.mu.Lock()
	f.installs++
	steps := f.steps
	f.mu.Unlock()
	for _, step := range steps {
		progress(step)
	}
	return f.result, f.installErr
}

func (f *fakeUpdater) counts() (checks int, installs int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.checks, f.installs
}

func newerVersion() update.Info {
	return update.Info{
		Kind: version.KindStable, Current: "v0.2.0", Latest: "v0.2.1", Tag: "v0.2.1",
		Available: true, Notes: "### Fixes\n\n- Corrige une fuite",
	}
}

func upToDate() update.Info {
	return update.Info{Kind: version.KindStable, Current: "v0.2.0", Latest: "v0.2.0", Tag: "v0.2.0"}
}

func updatableModel(f *fakeUpdater) Model {
	m := newTestModel(&fakeRunner{})
	m.updater = f
	m.versionText = "v0.2.0"
	m.width, m.height = 120, 34
	return m
}

// checked is a model whose first check has answered.
func checked(t *testing.T, f *fakeUpdater) Model {
	t.Helper()
	m := updatableModel(f)
	m, cmd := deliver(t, m, updateStartMsg{})
	if cmd == nil {
		t.Fatal("the first check must start")
	}
	m, _ = execute(t, m, cmd)
	return m
}

// afterGuard checks that the update modal is open on its own, then lets the
// guard that follows its opening end.
func afterGuard(t *testing.T, m Model) Model {
	t.Helper()
	if m.modalMode != modalUpdate || !m.upd.guard {
		t.Fatalf("the modal should be open and guarded: mode=%q guard=%t", m.modalMode, m.upd.guard)
	}
	m, _ = deliver(t, m, updateGuardMsg{})
	if m.upd.guard {
		t.Fatal("the guard must end")
	}
	return m
}

func TestHeaderShowsTheVersion(t *testing.T) {
	m := updatableModel(&fakeUpdater{})
	top := stripANSI(m.headerTop(110))
	if !strings.Contains(top, "v0.2.0") || !strings.Contains(top, "running") {
		t.Fatalf("version and counters on one row: %q", top)
	}

	// Narrow: the version goes first, the counters stay.
	narrow := 0
	for width := 100; width >= 40; width-- {
		row := stripANSI(m.headerTop(width))
		if textWidth(row) > width {
			t.Fatalf("width %d: the header row is %d wide: %q", width, textWidth(row), row)
		}
		if !strings.Contains(row, "v0.2.0") && narrow == 0 {
			narrow = width
			if !strings.Contains(row, "running") && !strings.Contains(row, "●") {
				t.Fatalf("width %d: the version is dropped before the counters: %q", width, row)
			}
		}
	}
	if narrow == 0 {
		t.Fatal("the version must be dropped on a narrow screen")
	}
}

func TestHeaderShowsWhatEachBuildIs(t *testing.T) {
	for _, tt := range []struct{ text, want string }{{"edge 3f9a2c1", "edge 3f9a2c1"}, {"dev", "dev"}} {
		m := updatableModel(&fakeUpdater{})
		m.versionText = tt.text
		if got := stripANSI(m.headerTop(110)); !strings.Contains(got, tt.want) {
			t.Errorf("%q not in %q", tt.want, got)
		}
	}
	// The version of this binary is what a new model shows.
	if m := newTestModel(&fakeRunner{}); m.versionText != version.Display() {
		t.Fatalf("versionText = %q, want %q", m.versionText, version.Display())
	}
}

func TestStartupChecksOnceAndOnlyWithAnUpdater(t *testing.T) {
	f := &fakeUpdater{info: newerVersion()}
	m := updatableModel(f)
	m, cmd := deliver(t, m, updateStartMsg{})
	if m.upd.phase != updateChecking || cmd == nil {
		t.Fatalf("phase=%v", m.upd.phase)
	}
	// A second start while checking, or once known, does not ask again.
	if _, again := deliver(t, m, updateStartMsg{}); again != nil {
		t.Fatal("a check is already running")
	}

	none := newTestModel(&fakeRunner{})
	if _, cmd := deliver(t, none, updateStartMsg{}); cmd != nil {
		t.Fatal("no updater, no check")
	}
}

func TestAnAvailableUpdateIsAnnouncedEverywhere(t *testing.T) {
	f := &fakeUpdater{info: newerVersion()}
	m := checked(t, f)

	if !m.updateAvailable() {
		t.Fatalf("phase=%v err=%v", m.upd.phase, m.upd.err)
	}
	// The modal opened on its own; "later" leaves the marks of the screen.
	m, _ = press(t, m, "esc")
	if m.modalMode != modalNone {
		t.Fatalf("mode = %q", m.modalMode)
	}
	if !strings.Contains(stripANSI(m.headerTop(110)), "⬆ v0.2.1") {
		t.Fatalf("the header marks it: %q", stripANSI(m.headerTop(110)))
	}
	if m.message != "update available: v0.2.1 (U)" || messageLevel(m.message) != levelWarn {
		t.Fatalf("the status row says so: %q", m.message)
	}
	if !strings.Contains(m.footerText(), "U update") {
		t.Fatalf("the footer offers it: %q", m.footerText())
	}

	// The mark survives a narrow screen longer than the counters.
	narrow := stripANSI(m.headerTop(60))
	if !strings.Contains(narrow, "⬆ v0.2.1") || textWidth(narrow) > 60 {
		t.Fatalf("at 60 columns: %q", narrow)
	}

	// Announced once: another check does not overwrite what the user was told.
	m.message = "backup of 100: OK"
	m, _ = deliver(t, m, updateCheckMsg{info: newerVersion()})
	if m.message != "backup of 100: OK" {
		t.Fatalf("a message of the user must stay: %q", m.message)
	}
}

func TestTheAnnouncementNeverReplacesAMessage(t *testing.T) {
	m := updatableModel(&fakeUpdater{})
	m.message = "stop guest 300: OK"
	m, _ = deliver(t, m, updateCheckMsg{info: newerVersion()})
	if m.message != "stop guest 300: OK" || !m.updateAvailable() {
		t.Fatalf("message=%q available=%t", m.message, m.updateAvailable())
	}
}

func TestNoUpdateNoMark(t *testing.T) {
	m := checked(t, &fakeUpdater{info: upToDate()})
	if m.updateAvailable() || m.updateIndicator() != "" || strings.Contains(m.footerText(), "U update") || m.message != "" {
		t.Fatalf("nothing to say: available=%t message=%q", m.updateAvailable(), m.message)
	}
}

func TestBackgroundCheckFailuresAreSilent(t *testing.T) {
	for _, err := range []error{errors.New("unable to reach GitHub: no route to host"), update.ErrDisabled, update.ErrDev} {
		f := &fakeUpdater{checkErr: err}
		m := checked(t, f)
		if len(m.entries) != 0 || m.message != "" || m.unreadErrors != 0 || m.updateAvailable() {
			t.Fatalf("%v: a node without Internet is normal, nothing to log: %+v %q", err, m.entries, m.message)
		}
	}
}

func TestUOpensTheModalAndChecksAtOnce(t *testing.T) {
	f := &fakeUpdater{info: newerVersion()}
	m := updatableModel(f)
	m, cmd := press(t, m, "U")
	if m.modalMode != modalUpdate || m.upd.phase != updateChecking || cmd == nil {
		t.Fatalf("mode=%q phase=%v", m.modalMode, m.upd.phase)
	}
	if out := stripANSI(m.View()); !strings.Contains(out, "checking GitHub") {
		t.Fatalf("it says it is checking:\n%s", out)
	}
	m, _ = execute(t, m, cmd)
	out := stripANSI(m.View())
	for _, want := range []string{"Update available", "v0.2.0", "v0.2.1", "stable: the latest release", "Corrige une fuite", "SHA-256", "Update"} {
		if !strings.Contains(out, want) {
			t.Errorf("the modal lacks %q:\n%s", want, out)
		}
	}
	if m.footerText() != "enter update  r check again  esc later" {
		t.Fatalf("footer = %q", m.footerText())
	}
}

func TestUWithoutAnUpdater(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m, cmd := press(t, m, "U")
	if m.modalMode != modalNone || cmd != nil || !strings.Contains(m.message, "not available") {
		t.Fatalf("mode=%q message=%q", m.modalMode, m.message)
	}
}

func TestUOnAnAlreadyKnownUpdateDoesNotAskAgain(t *testing.T) {
	f := &fakeUpdater{info: newerVersion()}
	m := checked(t, f)
	m, cmd := press(t, m, "U")
	if cmd != nil || m.modalMode != modalUpdate {
		t.Fatal("the answer is already known")
	}
	if checks, _ := f.counts(); checks != 1 {
		t.Fatalf("checks = %d", checks)
	}
	// r asks again.
	m, cmd = press(t, m, "r")
	if m.upd.phase != updateChecking || cmd == nil {
		t.Fatal("r checks again")
	}
	execute(t, m, cmd)
	if checks, _ := f.counts(); checks != 2 {
		t.Fatalf("checks = %d", checks)
	}
}

func TestUpToDateModalAndClose(t *testing.T) {
	m := checked(t, &fakeUpdater{info: upToDate()})
	m, _ = press(t, m, "U")
	if out := stripANSI(m.View()); !strings.Contains(out, "pve-console is up to date") {
		t.Fatalf("%s", out)
	}
	m, _ = press(t, m, "enter")
	if m.modalMode != modalNone {
		t.Fatal("enter closes")
	}
}

func TestAManualCheckThatFailsIsShownAndLoggedQuietly(t *testing.T) {
	f := &fakeUpdater{checkErr: errors.New("unable to read the latest release: GitHub answered 502 Bad Gateway")}
	m := updatableModel(f)
	m.message = "stop guest 300: OK"
	m, cmd := press(t, m, "U")
	m, _ = execute(t, m, cmd)

	if m.upd.err == nil || m.modalMode != modalUpdate {
		t.Fatal("the error is shown in the modal")
	}
	if out := stripANSI(m.View()); !strings.Contains(out, "502 Bad Gateway") {
		t.Fatalf("%s", out)
	}
	if len(m.entries) != 1 || m.entries[0].context != "check for updates" || m.entries[0].ok {
		t.Fatalf("it is logged: %+v", m.entries)
	}
	if m.message != "stop guest 300: OK" {
		t.Fatalf("a failed check must not replace the status row: %q", m.message)
	}

	// Opening the modal again retries, and r too.
	m, _ = press(t, m, "esc")
	_, cmd = press(t, m, "U")
	if cmd == nil {
		t.Fatal("a failed check is tried again when the modal is opened")
	}
}

func TestDisabledSaysWhyAndDoesNotRetry(t *testing.T) {
	f := &fakeUpdater{checkErr: update.ErrDisabled}
	m := checked(t, f)
	m, cmd := press(t, m, "U")
	if cmd != nil {
		t.Fatal("a disabled check is not repeated")
	}
	if out := stripANSI(m.View()); !strings.Contains(out, "--no-update-check") {
		t.Fatalf("it says how it was turned off:\n%s", out)
	}
	if len(m.entries) != 0 {
		t.Fatal("not an error")
	}
	m, _ = press(t, m, "enter")
	if m.modalMode != modalNone {
		t.Fatal("enter closes")
	}
}

func TestNothingIsInstalledWithoutConfirmation(t *testing.T) {
	f := &fakeUpdater{info: newerVersion()}
	m := checked(t, f)
	m, _ = press(t, m, "U")
	// Looking, closing, checking again, cancelling: never an installation.
	for _, key := range []string{"r", "esc"} {
		var cmd tea.Cmd
		m, cmd = press(t, m, key)
		if cmd != nil {
			m, _ = execute(t, m, cmd)
		}
	}
	if m.upd.phase == updateInstalling || m.upd.phase == updateInstalled {
		t.Fatalf("phase=%v", m.upd.phase)
	}
	if _, installs := f.counts(); installs != 0 {
		t.Fatalf("installs = %d", installs)
	}
	// enter outside the modal is not a confirmation of anything.
	m, _ = press(t, m, "enter")
	if _, installs := f.counts(); installs != 0 {
		t.Fatal("enter on the main screen must not install")
	}
}

func TestInstallationRunsAfterConfirmationAndEndsInstalled(t *testing.T) {
	f := &fakeUpdater{
		info:   newerVersion(),
		steps:  []string{"downloading", "checking the SHA-256 sum", "installing"},
		result: update.Result{Path: "/usr/local/sbin/pve-console", Previous: "/usr/local/sbin/pve-console.prev", Version: "pve-console 0.2.1 (abc, today)"},
	}
	m := checked(t, f)
	m = afterGuard(t, m)
	m, cmd := press(t, m, "enter")
	if m.upd.phase != updateInstalling || cmd == nil || m.message != "updating pve-console to v0.2.1" {
		t.Fatalf("phase=%v message=%q", m.upd.phase, m.message)
	}
	if !m.busy() {
		t.Fatal("the spinner must move while installing")
	}

	// While it runs: nothing closes the modal or starts another one.
	for _, key := range []string{"esc", "enter", "r", "U"} {
		var next tea.Cmd
		m, next = press(t, m, key)
		if m.upd.phase != updateInstalling || m.modalMode != modalUpdate || next != nil {
			t.Fatalf("%s must be ignored while installing", key)
		}
	}

	// Steps arrive one by one.
	seen := 0
	for cmd != nil {
		msg := cmd()
		m, cmd = deliver(t, m, msg)
		if _, ok := msg.(updateStepMsg); ok {
			seen++
			if out := stripANSI(m.View()); !strings.Contains(out, "downloading") {
				t.Fatalf("a step is shown as it comes:\n%s", out)
			}
		}
	}
	if seen != 3 {
		t.Fatalf("steps seen = %d", seen)
	}

	if m.upd.phase != updateInstalled || m.upd.err != nil {
		t.Fatalf("phase=%v err=%v", m.upd.phase, m.upd.err)
	}
	if _, installs := f.counts(); installs != 1 {
		t.Fatalf("installs = %d", installs)
	}
	if m.message != "pve-console updated to v0.2.1: restart to use it" || messageLevel(m.message) != levelOK {
		t.Fatalf("message = %q", m.message)
	}
	out := stripANSI(m.View())
	for _, want := range []string{"Updated to v0.2.1", "/usr/local/sbin/pve-console", "pve-console.prev", "keeps running the old version", "restart now"} {
		if !strings.Contains(out, want) {
			t.Errorf("the modal lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(stripANSI(m.headerTop(110)), "⟳ restart") || m.updateAvailable() {
		t.Fatalf("the header asks for a restart: %q", stripANSI(m.headerTop(110)))
	}

	last := m.entries[len(m.entries)-1]
	if !last.ok || last.source != sourceConsole || last.context != "update pve-console" || last.message != "v0.2.0 → v0.2.1" {
		t.Fatalf("the log keeps it: %+v", last)
	}
	if detail := strings.Join(last.detail, "\n"); !strings.Contains(detail, "replaced: /usr/local/sbin/pve-console") || !strings.Contains(detail, "pve-console 0.2.1") {
		t.Fatalf("detail = %q", detail)
	}
	if m.unreadErrors != 0 {
		t.Fatal("a success is not an unread error")
	}

	// A later check must not undo what was installed.
	m, _ = deliver(t, m, updateCheckMsg{info: newerVersion()})
	if m.upd.phase != updateInstalled {
		t.Fatal("a late check answer is ignored once installed")
	}
}

func TestRestartAfterAnUpdateQuitsAndAsksTheCallerToRestart(t *testing.T) {
	m := updatableModel(&fakeUpdater{})
	m.modalMode = modalUpdate
	m.upd = updateState{phase: updateInstalled, info: newerVersion()}

	// esc: later, nothing else.
	closed, cmd := press(t, m, "esc")
	if closed.restart || cmd != nil || closed.modalMode != modalNone {
		t.Fatal("esc only closes")
	}

	m, cmd = press(t, m, "enter")
	if !m.restart || cmd == nil {
		t.Fatal("enter asks for a restart")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("the program must quit, so that the terminal is restored before the restart")
	}
}

func TestInstallationFailureLeavesTheErrorAndLogsIt(t *testing.T) {
	f := &fakeUpdater{info: newerVersion(), installErr: errors.New("checksum mismatch for pve-console_linux_amd64: nothing installed")}
	m := checked(t, f)
	m = afterGuard(t, m)
	m, cmd := press(t, m, "enter")
	m = drive(t, m, cmd)

	if m.upd.phase != updateKnown || m.upd.err == nil || m.restart {
		t.Fatalf("phase=%v err=%v", m.upd.phase, m.upd.err)
	}
	if out := stripANSI(m.View()); !strings.Contains(out, "checksum mismatch") {
		t.Fatalf("the modal shows why:\n%s", out)
	}
	last := m.entries[len(m.entries)-1]
	if last.ok || last.context != "update pve-console" {
		t.Fatalf("logged as an error: %+v", last)
	}
	if strings.Contains(stripANSI(m.headerTop(110)), "⟳") {
		t.Fatal("nothing was installed: no restart to ask for")
	}
	// r checks again; enter does not retry the installation by itself.
	_, cmd = press(t, m, "r")
	if cmd == nil {
		t.Fatal("r checks again")
	}
	m, cmd = press(t, m, "enter")
	if m.modalMode != modalNone || cmd != nil {
		t.Fatal("enter closes the error")
	}
	if _, installs := f.counts(); installs != 1 {
		t.Fatalf("installs = %d", installs)
	}
}

func TestLateUpdateMessagesAreIgnored(t *testing.T) {
	m := updatableModel(&fakeUpdater{})
	before := m
	m, cmd := deliver(t, m, updateStepMsg{text: "stray"})
	if cmd != nil || len(m.upd.steps) != 0 {
		t.Fatal("a step without an installation is dropped")
	}
	m, cmd = deliver(t, m, updateInstallDoneMsg{result: update.Result{Path: "/x"}})
	if cmd != nil || m.upd.phase != updateIdle || len(m.entries) != 0 || m.message != before.message {
		t.Fatal("a result without an installation is dropped")
	}
}

func TestInstallationFeedThatEndsWithoutAResultIsReported(t *testing.T) {
	feed := make(chan tea.Msg)
	close(feed)
	done, ok := waitUpdate(feed)().(updateInstallDoneMsg)
	if !ok || done.err == nil {
		t.Fatalf("got %#v", done)
	}
}

func TestUpdateStepsDoNotMutateEarlierCopies(t *testing.T) {
	m := updatableModel(&fakeUpdater{})
	m.upd.phase = updateInstalling
	m.upd.feed = make(chan tea.Msg, 1)
	first, _ := m.handleUpdateStep(updateStepMsg{text: "a"})
	second, _ := first.handleUpdateStep(updateStepMsg{text: "b"})
	if len(m.upd.steps) != 0 || len(first.upd.steps) != 1 || len(second.upd.steps) != 2 {
		t.Fatalf("steps: %d %d %d", len(m.upd.steps), len(first.upd.steps), len(second.upd.steps))
	}
}

func TestDemoUpdateIsPlayedWithoutInstallingAnything(t *testing.T) {
	previous := demoStepDelay
	demoStepDelay = 0
	t.Cleanup(func() { demoStepDelay = previous })

	m := NewWithStartup(DemoClient(), false)
	m.demo = true
	m.updater = DemoUpdater()
	m.versionText = "v0.2.0"
	m.width, m.height = 120, 34

	m, cmd := deliver(t, m, updateStartMsg{})
	m, _ = execute(t, m, cmd)
	if !m.updateAvailable() {
		t.Fatal("the demo always has an update to offer")
	}
	m = afterGuard(t, m)
	m, cmd = press(t, m, "enter")
	m = drive(t, m, cmd)
	if m.upd.phase != updateInstalled {
		t.Fatalf("phase=%v err=%v", m.upd.phase, m.upd.err)
	}
	// The same screen as a real update, plus a line that says nothing was installed.
	if out := stripANSI(m.View()); !strings.Contains(out, "nothing was really installed") || !strings.Contains(out, "restart now") || !strings.Contains(out, "pve-console.prev") {
		t.Fatalf("the demo shows the end of an update, and says it was not real:\n%s", out)
	}
	m, cmd = press(t, m, "enter")
	if m.restart || m.modalMode != modalNone || cmd != nil || !strings.Contains(m.message, "demo") {
		t.Fatalf("the demo never restarts: restart=%t mode=%q message=%q", m.restart, m.modalMode, m.message)
	}
}

func TestDemoNextVersion(t *testing.T) {
	for in, want := range map[string]string{"0.2.0": "v0.2.1", "1.9.9": "v1.9.10", "0.3.0-rc1": "v0.3.1", "latest": "v9.9.9", "ab12cd3": "v9.9.9", "": "v9.9.9"} {
		if got := demoNextVersion(in); got != want {
			t.Errorf("demoNextVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHelpMentionsTheUpdateKey(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m.modalMode, m.width, m.height = modalHelp, 140, 60
	if out := stripANSI(m.View()); !strings.Contains(out, "version and updates") {
		t.Fatalf("help lacks the update key:\n%s", out)
	}
}

func TestReleaseNotesAreShownAsText(t *testing.T) {
	notes := "### Features\n\n- Menu Log\n* Mise à jour guidée\n\n### Fixes\r\n\r\n- Corrige une fuite\n"
	var plain []string
	for _, line := range releaseNotes(notes, 60, 20) {
		plain = append(plain, stripANSI(line))
	}
	want := []string{"Features", "• Menu Log", "• Mise à jour guidée", "Fixes", "• Corrige une fuite"}
	if strings.Join(plain, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q (no Markdown marks, no blank lines)", plain, want)
	}

	long := releaseNotes(strings.Repeat("- a line\n", 30), 60, 8)
	if len(long) != 8 || stripANSI(long[7]) != "…" {
		t.Fatalf("the notes are cut with a mark: %d lines", len(long))
	}
	if got := releaseNotes("- "+strings.Repeat("x", 200), 30, 8); textWidth(got[0]) > 30 {
		t.Fatalf("a long line is cut to the width: %d", textWidth(got[0]))
	}
	if len(releaseNotes("", 60, 8)) != 0 || len(releaseNotes("###", 60, 8)) != 0 {
		t.Fatal("nothing to show")
	}
}

func TestTheModalLabelsDoNotRunIntoTheValues(t *testing.T) {
	m := checked(t, &fakeUpdater{info: newerVersion()})
	m, _ = press(t, m, "U")
	out := stripANSI(m.View())
	for _, want := range []string{"Current  v0.2.0", "Latest   v0.2.1", "Channel  stable"} {
		if !strings.Contains(out, want) {
			t.Errorf("a label is followed by its value, separated: %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "###") || strings.Contains(out, "- Corrige") {
		t.Fatalf("the notes must not show Markdown marks:\n%s", out)
	}
}

// The modal announces an update on its own, once, and never in the way.
func TestAnUpdateOpensTheModalOnItsOwn(t *testing.T) {
	f := &fakeUpdater{info: newerVersion()}
	m := updatableModel(f)
	m, cmd := deliver(t, m, updateStartMsg{})
	m, cmd = execute(t, m, cmd)
	if m.modalMode != modalUpdate || !m.upd.offered || m.upd.offer {
		t.Fatalf("mode=%q offered=%t offer=%t", m.modalMode, m.upd.offered, m.upd.offer)
	}
	if cmd == nil {
		t.Fatal("the guard needs its timer")
	}
	for _, want := range []string{"Update available", "v0.2.1", "later"} {
		if out := stripANSI(m.View()); !strings.Contains(out, want) {
			t.Fatalf("the modal lacks %q:\n%s", want, out)
		}
	}

	// A habitual enter, right when the modal appears, installs nothing.
	m, c := press(t, m, "enter", "enter")
	if c != nil || m.upd.phase != updateKnown {
		t.Fatalf("phase=%v: enter is ignored during the guard", m.upd.phase)
	}
	if _, installs := f.counts(); installs != 0 {
		t.Fatalf("installs = %d", installs)
	}
	// esc still works during the guard, and the modal does not come back.
	m, _ = press(t, m, "esc")
	if m.modalMode != modalNone {
		t.Fatalf("mode = %q", m.modalMode)
	}
	m, _ = deliver(t, m, updateGuardMsg{})
	m, _ = deliver(t, m, spinMsg{})
	m, _ = deliver(t, m, updateCheckMsg{info: newerVersion()})
	if m.modalMode != modalNone {
		t.Fatal("once per session")
	}
	// U opens it again, with no guard: the user asked.
	m, _ = press(t, m, "U")
	if m.modalMode != modalUpdate || m.upd.guard {
		t.Fatalf("mode=%q guard=%t", m.modalMode, m.upd.guard)
	}
}

func TestTheOfferedModalInstallsOnlyAfterTheGuard(t *testing.T) {
	f := &fakeUpdater{info: newerVersion(), steps: []string{"installing"}, result: update.Result{Path: "/x"}}
	m := afterGuard(t, checked(t, f))
	m, cmd := press(t, m, "enter")
	if m.upd.phase != updateInstalling || cmd == nil {
		t.Fatalf("phase = %v", m.upd.phase)
	}
}

func TestTheModalWaitsForAFreeScreen(t *testing.T) {
	busy := map[string]func(*Model){
		"another modal": func(m *Model) { m.openConfirm("delete-backup", "DELETE", "vol", 0, "Delete?") },
		"help":          func(m *Model) { m.openModal(modalHelp, 0, "", "") },
		"search":        func(m *Model) { m.inputMode = inputSearch },
		"config":        func(m *Model) { m.showConfig = true },
		"welcome":       func(m *Model) { m.modalMode = modalStartup },
		"password":      func(m *Model) { m.modalMode = modalPasswd },
	}
	for name, setup := range busy {
		f := &fakeUpdater{info: newerVersion()}
		m := updatableModel(f)
		setup(&m)
		before := m.modalMode
		m, cmd := deliver(t, m, updateStartMsg{})
		m, _ = execute(t, m, cmd)
		if m.modalMode != before || m.upd.offered || !m.upd.offer {
			t.Errorf("%s: mode=%q offered=%t offer=%t: the modal must wait", name, m.modalMode, m.upd.offered, m.upd.offer)
			continue
		}
		// A key press, even the one that frees the screen, does not open it...
		m.modalMode, m.inputMode, m.showConfig = modalNone, inputNone, false
		m, _ = press(t, m, "j")
		if m.modalMode != modalNone || !m.upd.offer {
			t.Errorf("%s: a key press must not open the modal (mode=%q)", name, m.modalMode)
			continue
		}
		// ...the next tick or result does.
		m, _ = deliver(t, m, spinMsg{})
		if m.modalMode != modalUpdate || !m.upd.guard {
			t.Errorf("%s: mode=%q: the modal opens once the screen is free", name, m.modalMode)
		}
	}
}

func TestNoModalWhenThereIsNothingToOffer(t *testing.T) {
	for name, f := range map[string]*fakeUpdater{
		"up to date": {info: upToDate()},
		"error":      {checkErr: errors.New("no route to host")},
		"dev build":  {checkErr: update.ErrDev},
		"disabled":   {checkErr: update.ErrDisabled},
	} {
		m := checked(t, f)
		if m.modalMode != modalNone || m.upd.offer || m.upd.offered {
			t.Errorf("%s: mode=%q offer=%t", name, m.modalMode, m.upd.offer)
		}
	}
	// A check the user asked for is shown in the modal that is already open: no offer.
	m := updatableModel(&fakeUpdater{info: newerVersion()})
	m, _ = deliver(t, m, updateCheckMsg{info: newerVersion(), manual: true})
	if m.upd.offer || m.modalMode != modalNone {
		t.Fatalf("a manual check must not offer: offer=%t mode=%q", m.upd.offer, m.modalMode)
	}
}

func TestAnOfferThatGoesStaleIsDropped(t *testing.T) {
	m := updatableModel(&fakeUpdater{info: newerVersion()})
	m.modalMode = modalHelp
	m, _ = deliver(t, m, updateCheckMsg{info: newerVersion()})
	if !m.upd.offer {
		t.Fatal("the offer waits")
	}
	// Meanwhile an installation started by hand: nothing may pop over it.
	m.upd.phase = updateInstalling
	m.modalMode = modalNone
	m, _ = deliver(t, m, spinMsg{})
	if m.modalMode != modalNone || m.upd.offer {
		t.Fatalf("mode=%q offer=%t", m.modalMode, m.upd.offer)
	}
}
