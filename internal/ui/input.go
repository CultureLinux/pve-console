package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"pvc/internal/proxmox"
	"pvc/internal/validate"
)

func (m *Model) openConfirm(action string, title string, target string, vmid int, body string) {
	m.modalMode = modalConfirm
	m.modalAction = action
	m.modalTitle = title
	m.modalBody = body
	m.modalInput = target
	m.modalTargetVMID = vmid
	m.inputMode = inputNone
	m.input = ""
}

func (m *Model) openModal(mode modalMode, vmid int, jobID string, value string) {
	m.modalMode = mode
	m.modalField = 0
	m.modalInput = value
	m.modalVMIDs = ""
	m.modalStorage = m.storageIndex(value)
	m.modalGuest = 0
	m.modalTargetVMID = vmid
	m.modalJobID = jobID
	m.inputMode = inputNone
	m.input = ""
}

func (m *Model) closeModal() {
	m.modalMode = modalNone
	m.modalField = 0
	m.modalInput = ""
	m.modalVMIDs = ""
	m.modalStorage = 0
	m.modalGuest = 0
	m.modalTargetVMID = 0
	m.modalJobID = ""
	m.modalAction = ""
	m.modalTitle = ""
	m.modalBody = ""
}

func (m Model) handleConfirmSubmit() (tea.Model, tea.Cmd) {
	action := m.modalAction
	vmid := m.modalTargetVMID
	name := m.modalInput
	m.closeModal()
	m.message = "running " + action
	switch action {
	case "delete-backup":
		return m, m.deleteBackupVolume(name)
	case "restore-backup":
		volume, ok := m.selectedBackupVolumeByVolID(name)
		if !ok {
			m = m.logConsole("internal error", "restore backup", "backup selection not found")
			return m, nil
		}
		return m, m.restoreBackupVolume(volume)
	case "delete-snapshot":
		return m, m.deleteSnapshot(vmid, name)
	case "restore-snapshot":
		return m, m.rollbackSnapshot(vmid, name)
	case "start-guest":
		return m, m.startGuest(vmid)
	case "stop-guest":
		return m, m.stopGuest(vmid)
	case "reboot-guest":
		return m, m.rebootGuest(vmid)
	case "shutdown-guest":
		return m, m.shutdownGuest(vmid)
	case "dist-upgrade":
		return m.startConfirmedUpgrade()
	case "disable-tfa":
		m.message = fmt.Sprintf("disabling TFA for %s", name)
		return m, m.disableUserTFA(name)
	}
	return m, nil
}

func (m Model) handleModalSubmit() (tea.Model, tea.Cmd) {
	lastField := m.modalFieldCount() - 1
	confirmField := lastField - 1
	if m.modalField == lastField {
		m.closeModal()
		return m, nil
	}
	if m.modalField != confirmField {
		m.modalField = confirmField
		return m, nil
	}

	value := strings.TrimSpace(m.modalInput)
	vmids := strings.TrimSpace(m.modalVMIDs)
	mode := m.modalMode
	vmid := m.modalTargetVMID
	jobID := m.modalJobID
	storage := m.selectedModalStorage()
	m.closeModal()

	switch mode {
	case modalSnapshot:
		if value == "" {
			value = "manual-" + time.Now().Format("20060102-150405")
		}
		if err := validate.SnapshotName(value); err != nil {
			m = m.logConsole("invalid input", "create snapshot", err.Error())
			return m, nil
		}
		m.message = fmt.Sprintf("creating snapshot %s for %d", value, vmid)
		return m, m.createSnapshot(vmid, value)
	case modalBackup:
		m.message = fmt.Sprintf("starting backup for %d", vmid)
		return m, m.runBackup(vmid, storage)
	case modalSchedule:
		if value == "" {
			m = m.logConsole("invalid input", "update backup job "+jobID, "backup schedule is required")
			return m, nil
		}
		if vmids == "" {
			m = m.logConsole("invalid input", "update backup job "+jobID, "backup VMIDs are required")
			return m, nil
		}
		m.message = fmt.Sprintf("updating backup job %s", jobID)
		return m, m.updateBackupJob(jobID, storage, value, vmids)
	}

	return m, nil
}

func (m Model) modalFieldCount() int {
	if m.modalMode == modalSchedule {
		return 5
	}
	return 3
}

func (m Model) selectedModalStorage() string {
	storages := m.backupStorages()
	if len(storages) == 0 {
		return strings.TrimSpace(m.modalInput)
	}
	if m.modalStorage < 0 || m.modalStorage >= len(storages) {
		return storages[0]
	}
	return storages[m.modalStorage]
}

func (m *Model) nextModalStorage(delta int) {
	storages := m.backupStorages()
	if len(storages) == 0 {
		return
	}
	m.modalStorage = (m.modalStorage + delta + len(storages)) % len(storages)
	m.modalInput = storages[m.modalStorage]
}

func (m *Model) nextModalGuest(delta int) {
	if len(m.data.guests) == 0 {
		return
	}
	m.modalGuest = (m.modalGuest + delta + len(m.data.guests)) % len(m.data.guests)
}

func (m *Model) toggleModalGuest() {
	if len(m.data.guests) == 0 || m.modalGuest >= len(m.data.guests) {
		return
	}
	vmid := fmt.Sprintf("%d", m.data.guests[m.modalGuest].VMID)
	parts := vmidParts(m.modalVMIDs)
	for i, part := range parts {
		if part == vmid {
			parts = append(parts[:i], parts[i+1:]...)
			m.modalVMIDs = strings.Join(parts, ",")
			return
		}
	}
	parts = append(parts, vmid)
	m.modalVMIDs = strings.Join(parts, ",")
}

func (m Model) modalGuestLabel() string {
	if len(m.data.guests) == 0 || m.modalGuest >= len(m.data.guests) {
		return "-"
	}
	guest := m.data.guests[m.modalGuest]
	mark := " "
	for _, part := range vmidParts(m.modalVMIDs) {
		if part == fmt.Sprintf("%d", guest.VMID) {
			mark = "x"
			break
		}
	}
	return fmt.Sprintf("[%s] %d %s", mark, guest.VMID, guest.Name)
}

func (m Model) handleInputSubmit() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.input)
	mode := m.inputMode
	m.inputMode = inputNone
	m.input = ""

	if mode == inputSearch {
		m.query = input
		m.resetActiveSelection()
	}

	return m, nil
}

// confirmGuestRestart opens the confirmation for a clean reboot or shutdown. Both
// only make sense on a running guest.
func (m *Model) confirmGuestRestart(guest proxmox.Guest, reboot bool) {
	if guest.Status != "running" {
		m.message = fmt.Sprintf("guest %d is not running", guest.VMID)
		return
	}
	kind := strings.ToUpper(string(guest.Type))
	if reboot {
		m.openConfirm("reboot-guest", "REBOOT GUEST", fmt.Sprintf("%d", guest.VMID), guest.VMID, fmt.Sprintf("Reboot %s %d (%s) ?", kind, guest.VMID, guest.Name))
		return
	}
	m.openConfirm("shutdown-guest", "SHUTDOWN GUEST", fmt.Sprintf("%d", guest.VMID), guest.VMID, fmt.Sprintf("Shut down %s %d (%s) cleanly ?", kind, guest.VMID, guest.Name))
}
