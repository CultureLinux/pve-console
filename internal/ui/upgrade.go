package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"pvc/internal/proxmox"
)

// upgradePhase is the step of the package upgrade that is running.
type upgradePhase string

const (
	// upgradeIndex refreshes the package index (apt-get update), step 1 of 2.
	upgradeIndex upgradePhase = "index"
	// upgradeApply runs apt dist-upgrade, step 2 of 2, after the user confirmed.
	upgradeApply upgradePhase = "upgrade"
)

const (
	// maxUpgradeLines is the number of output lines kept in memory.
	maxUpgradeLines = 500
	// shownUpgradeLines is the number of output lines the modal shows.
	shownUpgradeLines = 14
	// listedUpgrades is the number of package names the confirmation lists.
	listedUpgrades = 8
	// upgradeLogLines is the number of output lines kept in the log entry.
	upgradeLogLines = 20
)

// errUpgradeInterrupted is reported when the feed of a step ends without a result.
var errUpgradeInterrupted = errors.New("the upgrade stopped without a result")

// upgradeLineMsg is one line of the output of the running step.
type upgradeLineMsg struct {
	line string
}

// upgradeStepDoneMsg ends a step. reboot says why a reboot is needed, if it is.
type upgradeStepDoneMsg struct {
	phase  upgradePhase
	err    error
	reboot string
}

// upgradeListMsg carries the package list read after the index was refreshed.
type upgradeListMsg struct {
	updates []proxmox.PackageUpdate
	err     error
}

func (p upgradePhase) title() string {
	if p == upgradeApply {
		return "2/2 apt dist-upgrade"
	}
	return "1/2 package index"
}

// runningLabel says what the spinner is waiting for.
func (p upgradePhase) runningLabel() string {
	if p == upgradeApply {
		return "apt dist-upgrade, non-interactive"
	}
	return "refreshing the package index (apt-get update)"
}

// startUpgrade begins the procedure: refresh the index first. Nothing is
// upgraded before the user confirmed the list that follows.
func (m Model) startUpgrade() (Model, tea.Cmd) {
	m.modalMode = modalUpgrade
	m.upgradeOutput = ""
	m.upgradeErr = nil
	m.upgradeReboot = ""
	m.upgradeNote = ""
	m.message = "refreshing the package index"
	return m.startUpgradeStep(upgradeIndex)
}

// startUpgradeStep runs one step in the background; its output reaches the
// model line by line through the feed.
func (m Model) startUpgradeStep(phase upgradePhase) (Model, tea.Cmd) {
	feed := make(chan tea.Msg, 64)
	m.upgradeRunning = true
	m.upgradePhase = phase
	m.upgradeFeed = feed
	go runUpgradeStep(m.client, phase, feed)
	return m, waitUpgrade(feed, phase)
}

// runUpgradeStep runs a step and sends its lines, then its result, on feed. It
// ends with the command, or at the latest when the timeout expires, and closes
// the feed.
func runUpgradeStep(client proxmox.Client, phase upgradePhase, feed chan<- tea.Msg) {
	defer close(feed)
	ctx, cancel := context.WithTimeout(context.Background(), longActionTimeout)
	defer cancel()

	send := func(msg tea.Msg) {
		select {
		case feed <- msg:
		case <-ctx.Done():
		}
	}
	onLine := func(line string) { send(upgradeLineMsg{line: line}) }

	done := upgradeStepDoneMsg{phase: phase}
	switch phase {
	case upgradeIndex:
		_, done.err = client.UpdateIndex(ctx, onLine)
	case upgradeApply:
		var output string
		output, done.err = client.DistUpgradeWith(ctx, onLine)
		if done.err == nil {
			done.reboot = client.RebootNeeded(ctx, output)
		}
	}
	send(done)
}

// waitUpgrade reads the next message of the running step.
func waitUpgrade(feed <-chan tea.Msg, phase upgradePhase) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-feed
		if !ok {
			return upgradeStepDoneMsg{phase: phase, err: errUpgradeInterrupted}
		}
		return msg
	}
}

// loadUpgradeList reads the packages that can be upgraded, once the index is fresh.
func (m Model) loadUpgradeList() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	updates, err := m.client.PackageUpdates(ctx)
	return upgradeListMsg{updates: updates, err: err}
}

// appendUpgradeLine adds a line to the output, keeping only the last lines.
func (m Model) appendUpgradeLine(line string) Model {
	output := m.upgradeOutput
	if output != "" {
		output += "\n"
	}
	output += line
	if strings.Count(output, "\n") >= maxUpgradeLines {
		lines := strings.Split(output, "\n")
		output = strings.Join(lines[len(lines)-maxUpgradeLines:], "\n")
	}
	m.upgradeOutput = output
	return m
}

// handleUpgradeLine shows a new line and waits for the next message.
func (m Model) handleUpgradeLine(msg upgradeLineMsg) (Model, tea.Cmd) {
	if !m.upgradeRunning {
		return m, nil
	}
	m = m.appendUpgradeLine(msg.line)
	return m, waitUpgrade(m.upgradeFeed, m.upgradePhase)
}

// handleUpgradeStepDone moves the procedure to its next step.
func (m Model) handleUpgradeStepDone(msg upgradeStepDoneMsg) (Model, tea.Cmd) {
	if !m.upgradeRunning || msg.phase != m.upgradePhase {
		return m, nil
	}

	if msg.phase == upgradeIndex {
		if msg.err != nil {
			m.upgradeRunning = false
			m.upgradeErr = msg.err
			return m.logError("refresh package index", msg.err), nil
		}
		return m, m.loadUpgradeList
	}

	m.upgradeRunning = false
	if msg.err != nil {
		m.upgradeErr = msg.err
		return m.logError("apt dist-upgrade", msg.err), m.loadUpdates
	}

	m.upgradeReboot = msg.reboot
	message, result := "apt dist-upgrade completed", "completed"
	if msg.reboot != "" {
		message += ": reboot required"
		result += ", reboot required"
	}
	m.message = message
	m = m.logOK(clock(), sourceConsole, "action done", "apt dist-upgrade", result, m.upgradeLogDetail())
	return m, m.loadUpdates
}

// upgradeLogDetail describes the end of an upgrade for the log.
func (m Model) upgradeLogDetail() []string {
	var detail []string
	if m.upgradeReboot != "" {
		detail = append(detail, "reboot required: "+m.upgradeReboot, "")
	}
	lines := strings.Split(strings.TrimSpace(m.upgradeOutput), "\n")
	if len(lines) > upgradeLogLines {
		lines = lines[len(lines)-upgradeLogLines:]
	}
	if len(lines) > 0 && lines[0] != "" {
		detail = append(detail, "output (end):")
		for _, line := range lines {
			detail = append(detail, "  "+strings.TrimRight(line, "\r "))
		}
	}
	return detail
}

// handleUpgradeList decides what follows a fresh index: stop on an error or
// when there is nothing to do, else ask for the confirmation.
func (m Model) handleUpgradeList(msg upgradeListMsg) Model {
	m.data.updates = msg.updates
	m.data.updateErr = msg.err
	m.upgradeRunning = false

	if msg.err != nil {
		m.upgradeErr = msg.err
		return m.logError("list package updates", msg.err)
	}
	if len(msg.updates) == 0 {
		m.upgradeNote = "Everything is up to date."
		m.message = "package index refreshed: nothing to upgrade"
		return m.logDone("refresh package index", "refreshed, nothing to upgrade")
	}

	m.openConfirm("dist-upgrade", "UPGRADE PACKAGES", "", 0, upgradeConfirmBody(msg.updates))
	m.message = fmt.Sprintf("package index refreshed: %d to upgrade", len(msg.updates))
	return m
}

// upgradeConfirmBody is the text of the confirmation: how many packages, which
// ones, and whether a kernel is among them.
func upgradeConfirmBody(updates []proxmox.PackageUpdate) string {
	names := make([]string, 0, listedUpgrades)
	kernel := false
	for i, update := range updates {
		if strings.Contains(update.Name, "kernel") {
			kernel = true
		}
		if i < listedUpgrades {
			names = append(names, update.Name)
		}
	}

	body := fmt.Sprintf("Run apt dist-upgrade for %d package", len(updates))
	if len(updates) != 1 {
		body += "s"
	}
	body += " ?\n\n" + strings.Join(names, ", ")
	if more := len(updates) - len(names); more > 0 {
		body += fmt.Sprintf(" (+%d more)", more)
	}
	if kernel {
		body += "\n\nA kernel is included: a reboot will be needed."
	} else {
		body += "\n\nA reboot may be needed afterwards."
	}
	return body
}

// startConfirmedUpgrade runs step 2, after the user confirmed.
func (m Model) startConfirmedUpgrade() (Model, tea.Cmd) {
	m.modalMode = modalUpgrade
	m.upgradeOutput = ""
	m.upgradeErr = nil
	m.upgradeReboot = ""
	m.upgradeNote = ""
	m.message = "running apt dist-upgrade"
	return m.startUpgradeStep(upgradeApply)
}

// upgradeModal shows the step that runs or ended: its output as it comes, and at
// the end the result, with the reboot warning last.
func (m Model) upgradeModal(width int) string {
	w := modalWidth(width, 90, 54)
	kind := modalKindInfo
	body := m.upgradeOutputLines(w)

	var footer []string
	switch {
	case m.upgradeRunning:
		body = append([]string{loading(m.upgradePhase.runningLabel())}, append([]string{""}, body...)...)
		footer = []string{styleMuted.Render("Please wait, this can take a few minutes.")}
	default:
		if m.upgradeErr != nil {
			kind = modalKindDanger
			body = append(body, "", styleDanger.Render(m.upgradeErr.Error()))
		} else if m.upgradeNote != "" {
			body = append(body, "", styleOK.Render(glyphOK+" "+m.upgradeNote))
		} else if m.upgradePhase == upgradeApply {
			body = append(body, "", styleOK.Render(glyphOK+" Upgrade completed."))
		}
		if m.upgradeReboot != "" {
			kind = modalKindWarn
			body = append(body, "", styleWarn.Bold(true).Render(glyphWarn+" Reboot required"))
			for _, part := range wrapText(m.upgradeReboot, w-6) {
				body = append(body, styleWarn.Render(part))
			}
			body = append(body, styleMuted.Render("Reboot the node when the guests can be stopped."))
		}
		footer = []string{hintKey("enter", "close")}
	}
	return modalFrame(kind, "Upgrade", m.upgradePhase.title(), body, footer, w)
}

// upgradeOutputLines returns the end of the output, one line each, cut to the
// width of the modal.
func (m Model) upgradeOutputLines(width int) []string {
	output := strings.TrimSpace(m.upgradeOutput)
	if output == "" {
		if m.upgradeRunning {
			return nil
		}
		return []string{styleMuted.Render("no output")}
	}
	lines := strings.Split(output, "\n")
	var out []string
	if hidden := len(lines) - shownUpgradeLines; hidden > 0 {
		out = append(out, styleMuted.Render(fmt.Sprintf("… %d earlier lines", hidden)))
		lines = lines[hidden:]
	}
	for _, line := range lines {
		out = append(out, styleMuted.Render(ellipsis(line, width-6)))
	}
	return out
}
