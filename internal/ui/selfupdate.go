package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"pvc/internal/update"
	"pvc/internal/version"
)

// Updater is what the UI needs to look for a newer pve-console and to install it.
type Updater interface {
	Check(ctx context.Context) (update.Info, error)
	Install(ctx context.Context, info update.Info, progress func(string)) (update.Result, error)
}

// updatePhase is where the update stands.
type updatePhase int

const (
	// updateIdle: nothing was checked yet.
	updateIdle updatePhase = iota
	// updateChecking: a check is running.
	updateChecking
	// updateKnown: a check answered (see info) or failed (see err), or an installation failed.
	updateKnown
	// updateInstalling: the new version is being downloaded and checked.
	updateInstalling
	// updateInstalled: the new version replaced the program; it runs after a restart.
	updateInstalled
)

// updateState is everything the UI knows about updates. It holds no pointer to
// shared data except the feed of a running installation.
type updateState struct {
	phase   updatePhase
	info    update.Info
	hasInfo bool
	err     error
	steps   []string
	result  update.Result
	feed    chan tea.Msg
	// announced is true once the status row told about this update.
	announced bool
	// offer is true while the modal is waiting for a free screen to open on its own.
	offer bool
	// offered is true once the modal opened on its own: it does so once per session.
	offered bool
	// guard is true just after that: enter is ignored, so that a habitual key does not install.
	guard bool
}

const (
	// startupUpdateDelay lets the screen load before the first check.
	startupUpdateDelay = 10 * time.Second
	updateCheckTimeout = 15 * time.Second
	// updateGuardDelay is how long enter is ignored after the modal opened on its own.
	updateGuardDelay     = 1500 * time.Millisecond
	updateInstallTimeout = 5 * time.Minute
)

// updateStartMsg starts the check that follows the launch.
type updateStartMsg struct{}

// updateCheckMsg carries the answer of a check. manual says the user asked for it.
type updateCheckMsg struct {
	info   update.Info
	err    error
	manual bool
}

// updateGuardMsg ends the guard that follows the automatic opening of the modal.
type updateGuardMsg struct{}

// updateStepMsg is a step of the running installation.
type updateStepMsg struct{ text string }

// updateInstallDoneMsg ends an installation.
type updateInstallDoneMsg struct {
	result update.Result
	err    error
}

// scheduleUpdateCheck waits a little, then asks for the first check.
func scheduleUpdateCheck() tea.Cmd {
	return tea.Tick(startupUpdateDelay, func(time.Time) tea.Msg { return updateStartMsg{} })
}

// checkUpdate asks GitHub, in the background.
func (m Model) checkUpdate(manual bool) tea.Cmd {
	updater := m.updater
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
		defer cancel()
		info, err := updater.Check(ctx)
		return updateCheckMsg{info: info, err: err, manual: manual}
	}
}

// quietUpdateError reports an error that only says why nothing was checked: it is
// not a problem to log.
func quietUpdateError(err error) bool {
	return errors.Is(err, update.ErrDisabled) || errors.Is(err, update.ErrDev)
}

// updateAvailable reports whether a newer version is waiting.
func (m Model) updateAvailable() bool {
	return m.upd.phase == updateKnown && m.upd.err == nil && m.upd.hasInfo && m.upd.info.Available
}

// updateIndicator is the mark of the header: a newer version, or a restart to do.
func (m Model) updateIndicator() string {
	switch {
	case m.upd.phase == updateInstalled:
		return styleWarn.Bold(true).Render("⟳ restart")
	case m.updateAvailable():
		return styleWarn.Bold(true).Render("⬆ " + m.upd.info.Latest)
	}
	return ""
}

// openUpdate opens the update modal, and checks if nothing is known yet.
func (m Model) openUpdate() (Model, tea.Cmd) {
	if m.updater == nil {
		m.message = "updates are not available in this session"
		return m, nil
	}
	m.modalMode = modalUpdate
	m.upd.guard = false
	retry := m.upd.phase == updateKnown && m.upd.err != nil && !quietUpdateError(m.upd.err)
	if m.upd.phase == updateIdle || retry {
		m.upd.phase = updateChecking
		m.upd.err = nil
		return m, m.checkUpdate(true)
	}
	return m, nil
}

// handleUpdateKey handles the keys of the update modal.
func (m Model) handleUpdateKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	u := m.upd
	switch msg.String() {
	case "esc":
		if u.phase != updateInstalling {
			m.closeModal()
		}
	case "r":
		if u.phase == updateKnown {
			m.upd.phase = updateChecking
			m.upd.err = nil
			return m, m.checkUpdate(true)
		}
	case "enter":
		if u.guard {
			return m, nil
		}
		switch {
		case u.phase == updateInstalled:
			if m.demo {
				// Nothing was installed: nothing to start again.
				m.closeModal()
				m.message = "demo: the program would restart now"
				return m, nil
			}
			// The program closes the terminal first, then starts again (see Run).
			m.restart = true
			return m, tea.Quit
		case m.updateAvailable():
			return m.startInstall()
		case u.phase == updateKnown:
			m.closeModal()
		}
	}
	return m, nil
}

// startInstall runs the installation in the background. This is only reached
// from the confirmation: enter on an available update.
func (m Model) startInstall() (Model, tea.Cmd) {
	feed := make(chan tea.Msg, 16)
	m.upd.phase = updateInstalling
	m.upd.err = nil
	m.upd.steps = nil
	m.upd.feed = feed
	m.message = "updating pve-console to " + m.upd.info.Latest
	go runInstall(m.updater, m.upd.info, feed)
	return m, waitUpdate(feed)
}

// runInstall installs and sends its steps, then its result, on feed. It ends
// with the installation, or when the timeout expires, and closes the feed.
func runInstall(updater Updater, info update.Info, feed chan<- tea.Msg) {
	defer close(feed)
	ctx, cancel := context.WithTimeout(context.Background(), updateInstallTimeout)
	defer cancel()
	send := func(msg tea.Msg) {
		select {
		case feed <- msg:
		case <-ctx.Done():
		}
	}
	result, err := updater.Install(ctx, info, func(text string) { send(updateStepMsg{text: text}) })
	send(updateInstallDoneMsg{result: result, err: err})
}

// waitUpdate reads the next message of the running installation.
func waitUpdate(feed <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-feed
		if !ok {
			return updateInstallDoneMsg{err: errors.New("the update stopped without a result")}
		}
		return msg
	}
}

// handleUpdateCheck stores the answer of a check.
func (m Model) handleUpdateCheck(msg updateCheckMsg) Model {
	if m.upd.phase == updateInstalling || m.upd.phase == updateInstalled {
		return m
	}
	m.upd.phase = updateKnown
	m.upd.err = msg.err
	if msg.err != nil {
		// A node without Internet access is normal: only a check the user asked for is logged.
		if msg.manual && !quietUpdateError(msg.err) {
			m = m.logErrorQuiet("check for updates", msg.err)
		}
		return m
	}
	m.upd.info, m.upd.hasInfo = msg.info, true
	// A check in the background that finds an update opens the modal, once, when the screen is free.
	if msg.info.Available && !msg.manual && !m.upd.offered {
		m.upd.offer = true
	}
	if msg.info.Available && !m.upd.announced {
		m.upd.announced = true
		if m.message == "" {
			m.message = fmt.Sprintf("update available: %s (U)", msg.info.Latest)
		}
	}
	return m
}

// offerUpdate opens the update modal on its own, once, as soon as the screen is
// free: no other modal, no search being typed, no configuration panel.
func (m Model) offerUpdate() (Model, tea.Cmd) {
	if !m.upd.offer {
		return m, nil
	}
	if !m.updateAvailable() {
		m.upd.offer = false
		return m, nil
	}
	if m.modalMode != modalNone || m.inputMode != inputNone || m.showConfig {
		return m, nil
	}
	m.upd.offer, m.upd.offered, m.upd.guard = false, true, true
	m.modalMode = modalUpdate
	return m, tea.Tick(updateGuardDelay, func(time.Time) tea.Msg { return updateGuardMsg{} })
}

// handleUpdateStep shows a step of the installation and waits for the next message.
func (m Model) handleUpdateStep(msg updateStepMsg) (Model, tea.Cmd) {
	if m.upd.phase != updateInstalling {
		return m, nil
	}
	steps := make([]string, 0, len(m.upd.steps)+1)
	steps = append(steps, m.upd.steps...)
	m.upd.steps = append(steps, msg.text)
	return m, waitUpdate(m.upd.feed)
}

// handleUpdateDone ends an installation: logged, and shown in the modal.
func (m Model) handleUpdateDone(msg updateInstallDoneMsg) Model {
	if m.upd.phase != updateInstalling {
		return m
	}
	if msg.err != nil {
		m.upd.phase = updateKnown
		m.upd.err = msg.err
		return m.logError("update pve-console", msg.err)
	}
	m.upd.phase = updateInstalled
	m.upd.err = nil
	m.upd.result = msg.result
	m.message = fmt.Sprintf("pve-console updated to %s: restart to use it", m.upd.info.Latest)

	detail := []string{"replaced: " + msg.result.Path}
	if msg.result.Previous != "" {
		detail = append(detail, "previous version kept as: "+msg.result.Previous)
	}
	if msg.result.Version != "" {
		detail = append(detail, "the new binary says: "+msg.result.Version)
	}
	return m.logOK(clock(), sourceConsole, "action done", "update pve-console", m.upd.info.Current+" → "+m.upd.info.Latest, detail)
}

// channelText says which releases a check follows.
func channelText(kind version.Kind) string {
	if kind == version.KindEdge {
		return "edge: the newest build of main"
	}
	return "stable: the latest release"
}

// updateModal shows what is known about updates and what can be done.
func (m Model) updateModal(width int) string {
	w := modalWidth(width, 76, 50)
	u := m.upd
	kind := modalKindInfo
	subtitle := m.versionText
	var body, footer []string

	switch {
	case u.phase == updateChecking:
		body = []string{loading("checking GitHub for a new version")}
		footer = []string{styleMuted.Render("Please wait.")}

	case u.phase == updateInstalling:
		body = []string{loading("updating to " + u.info.Latest), ""}
		for _, step := range u.steps {
			body = append(body, styleMuted.Render(ellipsis(glyphInfo+" "+step, w-6)))
		}
		footer = []string{styleMuted.Render("Please wait, and keep the terminal open.")}

	case u.phase == updateInstalled:
		kind = modalKindWarn
		body = []string{styleOK.Render(glyphOK + " Updated to " + u.info.Latest + "."), "", kv("Replaced", ellipsis(u.result.Path, max(8, w-16)))}
		if u.result.Previous != "" {
			body = append(body, kv("Kept as", ellipsis(u.result.Previous, max(8, w-16))), styleMuted.Render("To go back: replace the program with that file."))
		}
		body = append(body, "", styleWarn.Render(glyphWarn+" The program keeps running the old version until it restarts."))
		if m.demo {
			body = append(body, styleMuted.Render("(demo: nothing was really installed or replaced)"))
		}
		footer = []string{hintKey("enter", "restart now") + "   " + hintKey("esc", "later")}

	case u.err != nil && quietUpdateError(u.err):
		body = []string{styleMuted.Render(u.err.Error())}
		footer = []string{hintKey("enter", "close")}

	case u.err != nil:
		kind = modalKindDanger
		for _, line := range wrapText(oneLine(u.err.Error()), w-6) {
			body = append(body, styleDanger.Render(line))
		}
		footer = []string{hintKey("r", "check again") + "   " + hintKey("esc", "close")}

	case u.hasInfo && u.info.Available:
		kind = modalKindWarn
		body = []string{
			styleWarn.Bold(true).Render(glyphWarn + " Update available"),
			"",
			kv("Current", u.info.Current),
			kv("Latest", styleBold.Render(u.info.Latest)),
			kv("Channel", channelText(u.info.Kind)),
		}
		if notes := releaseNotes(u.info.Notes, w-6, 8); len(notes) > 0 {
			body = append(body, "", styleSection.Render("WHAT'S NEW"))
			body = append(body, notes...)
		}
		body = append(body, "", styleMuted.Render("The new version is downloaded from GitHub, its SHA-256 sum is"),
			styleMuted.Render("checked, then it replaces this program. The old one is kept."))
		footer = []string{button(glyphEnter+" Update", buttonWarn, true) + "  " + hintKey("r", "check again") + "   " + hintKey("esc", "later")}

	case u.hasInfo:
		body = []string{
			styleOK.Render(glyphOK + " pve-console is up to date."),
			"",
			kv("Current", u.info.Current),
			kv("Channel", channelText(u.info.Kind)),
		}
		footer = []string{hintKey("r", "check again") + "   " + hintKey("esc", "close")}

	default:
		body = []string{styleMuted.Render("Press r to look for a new version.")}
		footer = []string{hintKey("r", "check") + "   " + hintKey("esc", "close")}
	}
	return modalFrame(kind, "Update", subtitle, body, footer, w)
}

// releaseNotes turns the Markdown notes of a release into at most max lines of
// the modal: headings as they are, list items with a bullet, no blank lines.
func releaseNotes(notes string, width int, max int) []string {
	var lines []string
	for _, raw := range strings.Split(strings.ReplaceAll(notes, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "#"):
			line = strings.TrimSpace(strings.TrimLeft(line, "#"))
			if line != "" {
				lines = append(lines, styleBold.Render(ellipsis(line, width)))
			}
			continue
		case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* "):
			line = "• " + strings.TrimSpace(line[2:])
		}
		lines = append(lines, styleMuted.Render(ellipsis(line, width)))
	}
	if len(lines) > max {
		lines = append(lines[:max-1], styleMuted.Render("…"))
	}
	return lines
}

// updateHints are the hints of the footer while the update modal is open.
func (m Model) updateHints() []hint {
	u := m.upd
	switch {
	case u.phase == updateChecking:
		return []hint{{"…", "checking"}}
	case u.phase == updateInstalling:
		return []hint{{"…", "updating"}}
	case u.phase == updateInstalled:
		return []hint{{"enter", "restart"}, {"esc", "later"}}
	case m.updateAvailable():
		return []hint{{"enter", "update"}, {"r", "check again"}, {"esc", "later"}}
	}
	return []hint{{"r", "check again"}, {"esc", "close"}}
}
