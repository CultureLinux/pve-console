package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"pvc/internal/proxmox"
	"pvc/internal/version"
)

// modalKind picks the color of a modal frame.
type modalKind int

const (
	modalKindInfo modalKind = iota
	modalKindWarn
	modalKindDanger
)

func (k modalKind) color() lipgloss.AdaptiveColor {
	switch k {
	case modalKindWarn:
		return colWarn
	case modalKindDanger:
		return colDanger
	}
	return colAccent
}

// modalWidth is the width of a modal: preferred, but never wider than the screen.
func modalWidth(screen int, preferred int, minimum int) int {
	return min(preferred, max(minimum, screen-6))
}

// modalFrame draws the frame shared by all modals: a colored border, a title
// with an optional subtitle, the body, and a footer line (buttons or hints).
func modalFrame(kind modalKind, title string, subtitle string, body []string, footer []string, width int) string {
	color := kind.color()
	head := lipgloss.NewStyle().Bold(true).Foreground(color).Render(title)
	if subtitle != "" {
		head += styleMuted.Render("  " + glyphSep + "  " + subtitle)
	}
	lines := []string{head, ""}
	lines = append(lines, body...)
	if len(footer) > 0 {
		lines = append(lines, "")
		lines = append(lines, footer...)
	}
	return lipgloss.NewStyle().
		Width(width).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(color).
		Padding(1, 2).
		Render(strings.Join(lines, "\n"))
}

// buttons lays out the confirm and cancel buttons of a modal.
func buttons(confirm string, kind buttonKind, confirmFocused bool, cancelFocused bool) string {
	return button(confirm, kind, confirmFocused) + "  " + button("Cancel", buttonNeutral, cancelFocused)
}

func (m Model) modal(width int) string {
	switch m.modalMode {
	case modalStartup:
		return m.startupModal(width)
	case modalHelp:
		return m.helpModal(width)
	case modalSchedule:
		return m.scheduleModal(width)
	case modalUpgrade:
		return m.upgradeModal(width)
	case modalUpdate:
		return m.updateModal(width)
	case modalConfirm:
		return m.confirmModal(width)
	case modalPasswd:
		return m.passwdModal(width)
	case modalEnroll:
		_, height := m.size()
		return m.enrollModal(width, height)
	}

	w := modalWidth(width, 56, 40)
	fieldW := max(10, w-4-10)
	lastField := m.modalFieldCount() - 1
	confirmField := lastField - 1

	title, label, confirm := "New snapshot", "Name", "Create"
	subtitle := fmt.Sprintf("VMID %d", m.modalTargetVMID)
	value := m.modalInput
	if m.modalMode == modalBackup {
		title, label, confirm = "Run a backup", "Storage", "Start backup"
		value = "‹ " + m.selectedModalStorage() + " ›"
	}
	if name := m.knownGuestName(m.modalTargetVMID); name != "" {
		subtitle += " " + name
	}

	body := []string{field(label, value, m.modalField == 0, fieldW)}
	if m.modalMode == modalSnapshot {
		body = append(body, "", styleMuted.Render("Leave the name empty for an automatic one."))
		if m.guestIsQEMU(m.modalTargetVMID) && m.snapshotVMState {
			body = append(body, styleMuted.Render("The RAM state of the VM is saved too."))
		}
	}
	if m.modalMode == modalBackup {
		body = append(body, "", styleMuted.Render("Snapshot mode, runs in the background."))
	}
	footer := []string{buttons(confirm, buttonPrimary, m.modalField == confirmField, m.modalField == lastField)}
	return modalFrame(modalKindInfo, title, subtitle, body, footer, w)
}

// guestIsQEMU reports whether vmid is a known QEMU VM.
func (m Model) guestIsQEMU(vmid int) bool {
	for _, guest := range m.data.guests {
		if guest.VMID == vmid {
			return guest.Type == proxmox.GuestTypeQEMU
		}
	}
	return false
}

func (m Model) scheduleModal(width int) string {
	w := modalWidth(width, 64, 44)
	fieldW := max(10, w-4-10)
	lastField := m.modalFieldCount() - 1
	confirmField := lastField - 1

	body := []string{
		field("Storage", "‹ "+m.selectedModalStorage()+" ›", m.modalField == 0, fieldW),
		field("Schedule", m.modalInput, m.modalField == 1, fieldW),
		"",
		styleSection.Render("GUESTS"),
		m.modalGuestChecklist(w-4, 8),
	}
	footer := []string{buttons("Save", buttonPrimary, m.modalField == confirmField, m.modalField == lastField)}
	return modalFrame(modalKindInfo, "Backup schedule", "job "+m.modalJobID, body, footer, w)
}

func (m Model) modalGuestChecklist(width int, rows int) string {
	if len(m.data.guests) == 0 {
		return styleMuted.Render("no guests loaded")
	}
	start, end := window(m.modalGuest, len(m.data.guests), rows)
	selected := map[string]bool{}
	for _, part := range vmidParts(m.modalVMIDs) {
		selected[part] = true
	}
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		guest := m.data.guests[i]
		box, boxStyle := "[ ]", styleMuted
		if selected[fmt.Sprintf("%d", guest.VMID)] {
			box, boxStyle = "[x]", styleOK
		}
		focused := m.modalField == 2 && i == m.modalGuest
		lines = append(lines, row([]seg{
			{box + " ", boxStyle},
			{col(fmt.Sprintf("%d", guest.VMID), 5) + " ", styleMuted},
			{guest.Name, plain},
		}, width, focused))
	}
	return strings.Join(lines, "\n")
}

// confirmKind and confirmLabel describe an action of the confirmation modal.
func confirmKind(action string) (modalKind, buttonKind) {
	switch action {
	case "start-guest":
		return modalKindInfo, buttonPrimary
	case "reboot-guest", "shutdown-guest", "dist-upgrade", "enable-agent", "disable-agent":
		return modalKindWarn, buttonWarn
	}
	return modalKindDanger, buttonDanger
}

func confirmLabel(action string) string {
	switch action {
	case "start-guest":
		return "Start guest"
	case "stop-guest":
		return "Stop guest"
	case "reboot-guest":
		return "Reboot guest"
	case "shutdown-guest":
		return "Shut down guest"
	case "delete-snapshot":
		return "Delete snapshot"
	case "restore-snapshot":
		return "Roll back"
	case "delete-backup":
		return "Delete backup"
	case "restore-backup":
		return "Restore backup"
	case "enable-agent":
		return "Enable agent"
	case "disable-agent":
		return "Disable agent"
	case "dist-upgrade":
		return "Upgrade"
	case "disable-tfa":
		return "Disable 2FA"
	}
	return "Confirm"
}

// sentenceCase turns "STOP GUEST" into "Stop guest".
func sentenceCase(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return text
	}
	return strings.ToUpper(text[:1]) + text[1:]
}

func (m Model) confirmModal(width int) string {
	kind, buttonColor := confirmKind(m.modalAction)
	w := modalWidth(width, 64, 44)
	body := strings.Split(m.modalBody, "\n")
	if kind == modalKindDanger {
		body = append(body, "", styleMuted.Render("This cannot be undone."))
	}
	primary := button(glyphEnter+" "+confirmLabel(m.modalAction), buttonColor, true)
	footer := []string{primary + "  " + button("esc Cancel", buttonNeutral, false)}
	return modalFrame(kind, sentenceCase(m.modalTitle), "", body, footer, w)
}

func (m Model) startupModal(width int) string {
	w := modalWidth(width, 66, 42)
	body := []string{
		styleWarn.Render("This tool is experimental."),
		"",
		"Every action runs as root on this node. Check the guest, the",
		"storage and the job before you confirm.",
		"",
		styleMuted.Render(version.String()),
	}
	footer := []string{hintKey("enter", "continue") + "   " + hintKey("esc", "close")}
	return modalFrame(modalKindWarn, "Welcome", "pve-console", body, footer, w)
}

// helpSection is a titled list of key bindings of the help.
type helpSection struct {
	title string
	items [][2]string
}

var helpSections = []helpSection{
	{"NAVIGATION", [][2]string{
		{"↑ ↓  k j", "move"},
		{"enter", "guest configuration"},
		{"esc", "back / close"},
		{"r", "refresh"},
		{"U", "version and updates"},
		{"q", "quit"},
	}},
	{"VIEWS", [][2]string{
		{"g", "guests"},
		{"b", "backups"},
		{"u", "users"},
		{"p", "package updates"},
		{"L", "log (errors, results)"},
		{"tab", "switch type / mode"},
	}},
	{"FILTERS", [][2]string{
		{"/", "search"},
		{"c", "clear the search"},
		{"f", "status filter"},
		{"t", "sort order"},
	}},
	{"GUESTS", [][2]string{
		{"+", "start"},
		{"-", "stop (forced)"},
		{"S", "shut down (clean)"},
		{"R", "reboot (clean)"},
		{"A", "guest agent on / off"},
		{"m", "run a backup"},
	}},
	{"SNAPSHOTS", [][2]string{
		{"s", "list snapshots"},
		{"n", "new snapshot"},
		{"o", "roll back"},
		{"d", "delete"},
	}},
	{"BACKUPS", [][2]string{
		{"m", "run a backup"},
		{"e", "edit a job schedule"},
		{"o", "restore the volume"},
		{"d", "delete the volume"},
	}},
	{"LOG", [][2]string{
		{"L", "list errors, results"},
		{"tab", "filter by source"},
		{"f", "errors / successes"},
		{"x", "clear the log"},
	}},
	{"USERS AND UPDATES", [][2]string{
		{"s", "show TFA entries"},
		{"n", "enroll a TOTP (QR code)"},
		{"P", "change the password"},
		{"x", "disable TFA"},
		{"a", "refresh, confirm, upgrade"},
	}},
}

func renderHelpSection(section helpSection, width int) []string {
	lines := []string{styleSection.Render(section.title)}
	for _, item := range section.items {
		lines = append(lines, styleKey.Render(col(item[0], 9))+" "+ellipsis(item[1], max(4, width-10)))
	}
	return lines
}

// helpColumns spreads the help sections over n columns, keeping them balanced.
func helpColumns(sections []helpSection, n int, width int) [][]string {
	columns := make([][]string, n)
	for _, section := range sections {
		shortest := 0
		for i := range columns {
			if len(columns[i]) < len(columns[shortest]) {
				shortest = i
			}
		}
		if len(columns[shortest]) > 0 {
			columns[shortest] = append(columns[shortest], "")
		}
		columns[shortest] = append(columns[shortest], renderHelpSection(section, width)...)
	}
	return columns
}

func (m Model) helpModal(width int) string {
	_, height := m.size()
	const gap = 3
	const minColumn = 30

	// Use as few columns as the height allows, as many as the width allows.
	columns := 1
	for n := 1; n <= 3; n++ {
		w := modalWidth(width, n*(minColumn+gap)+4, 38)
		colW := (w - 4 - gap*(n-1)) / n
		if colW < minColumn && n > 1 {
			break
		}
		columns = n
		tallest := 0
		for _, c := range helpColumns(helpSections, n, colW) {
			tallest = max(tallest, len(c))
		}
		if tallest+8 <= height {
			break
		}
	}

	w := modalWidth(width, columns*(minColumn+gap)+4, 38)
	colW := (w - 4 - gap*(columns-1)) / columns
	cols := helpColumns(helpSections, columns, colW)
	rows := 0
	for _, c := range cols {
		rows = max(rows, len(c))
	}
	var body []string
	for i := 0; i < rows; i++ {
		parts := make([]string, columns)
		for j, c := range cols {
			text := ""
			if i < len(c) {
				text = c[i]
			}
			parts[j] = fit(text, colW)
		}
		body = append(body, strings.TrimRight(strings.Join(parts, strings.Repeat(" ", gap)), " "))
	}
	footer := []string{hintKey("esc", "close") + "   " + styleMuted.Render("dev by @clinux")}
	return modalFrame(modalKindInfo, "Help", "pve-console", body, footer, w)
}
