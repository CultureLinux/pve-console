package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"pvc/internal/proxmox"
)

// Update handles key presses and the results of asynchronous commands.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.route(msg)
	if routed, ok := next.(Model); ok {
		var read tea.Cmd
		routed, read = routed.wantGuestNet()
		next, cmd = routed, tea.Batch(cmd, read)
	}
	if _, isKey := msg.(tea.KeyMsg); isKey {
		// Never on a key press: the key that closes a modal must not land in the next one.
		return next, cmd
	}
	model, ok := next.(Model)
	if !ok {
		return next, cmd
	}
	model, offer := model.offerUpdate()
	return model, tea.Batch(cmd, offer)
}

// route sends a message to the code that handles it.
func (m Model) route(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	return m.updateResult(msg)
}

// updateKey routes a key press to the open modal, the text input, or the main view.
func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.modalMode != modalNone {
		return m.updateModalKey(msg)
	}
	if m.inputMode != inputNone {
		return m.updateInputKey(msg)
	}
	return m.updateMainKey(msg)
}

// updateModalKey handles key presses while a modal is open.
func (m Model) updateModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.modalMode == modalStartup {
		switch msg.String() {
		case "esc", "enter":
			m.closeModal()
		}
		return m, nil
	}
	if m.modalMode == modalHelp {
		switch msg.String() {
		case "esc", "enter", "?", "h":
			m.closeModal()
		}
		return m, nil
	}
	if m.modalMode == modalUpdate {
		return m.handleUpdateKey(msg)
	}
	if m.modalMode == modalEnroll {
		return m.handleEnrollKey(msg)
	}
	if m.modalMode == modalPasswd {
		return m.handlePasswdKey(msg)
	}
	if m.modalMode == modalUpgrade {
		switch msg.String() {
		case "esc", "enter":
			if !m.upgradeRunning {
				m.closeModal()
			}
		}
		return m, nil
	}
	if m.modalMode == modalConfirm {
		switch msg.String() {
		case "esc":
			m.closeModal()
		case "enter":
			return m.handleConfirmSubmit()
		}
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.closeModal()
	case "enter":
		return m.handleModalSubmit()
	case "tab", "right":
		if (m.modalMode == modalBackup || m.modalMode == modalSchedule) && m.modalField == 0 && msg.String() == "right" {
			m.nextModalStorage(1)
		} else {
			m.modalField = (m.modalField + 1) % m.modalFieldCount()
		}
	case "shift+tab":
		m.modalField--
		if m.modalField < 0 {
			m.modalField = m.modalFieldCount() - 1
		}
	case "left":
		if (m.modalMode == modalBackup || m.modalMode == modalSchedule) && m.modalField == 0 {
			m.nextModalStorage(-1)
		} else {
			m.modalField--
			if m.modalField < 0 {
				m.modalField = m.modalFieldCount() - 1
			}
		}
	case "up":
		if (m.modalMode == modalBackup || m.modalMode == modalSchedule) && m.modalField == 0 {
			m.nextModalStorage(-1)
		} else if m.modalMode == modalSchedule && m.modalField == 2 {
			m.nextModalGuest(-1)
		}
	case "down":
		if (m.modalMode == modalBackup || m.modalMode == modalSchedule) && m.modalField == 0 {
			m.nextModalStorage(1)
		} else if m.modalMode == modalSchedule && m.modalField == 2 {
			m.nextModalGuest(1)
		}
	case " ":
		if m.modalMode == modalSchedule && m.modalField == 2 {
			m.toggleModalGuest()
		} else if m.modalMode == modalSchedule && m.modalField == 1 {
			m.modalInput += msg.String()
		} else if m.modalMode != modalBackup && m.modalField == 0 {
			m.modalInput += msg.String()
		}
	case "backspace":
		if ((m.modalMode == modalSchedule && m.modalField == 1) || (m.modalMode != modalBackup && m.modalMode != modalSchedule && m.modalField == 0)) && len(m.modalInput) > 0 {
			m.modalInput = m.modalInput[:len(m.modalInput)-1]
		}
	default:
		if m.modalMode == modalSchedule && m.modalField == 1 && len(msg.String()) == 1 {
			m.modalInput += msg.String()
		} else if m.modalMode != modalBackup && m.modalMode != modalSchedule && m.modalField == 0 && len(msg.String()) == 1 {
			m.modalInput += msg.String()
		}
	}
	return m, nil
}

// updateInputKey handles key presses while the search or confirmation input is open.
func (m Model) updateInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.inputMode = inputNone
		m.input = ""
	case "enter":
		return m.handleInputSubmit()
	case "tab", " ":
		if len(msg.String()) == 1 {
			m.input += msg.String()
		}
	case "backspace":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
	default:
		if len(msg.String()) == 1 {
			m.input += msg.String()
		}
	}
	return m, nil
}

// updateMainKey handles key presses in the main views.
func (m Model) updateMainKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "?", "h":
		m.openModal(modalHelp, 0, "", "")
	case "U":
		return m.openUpdate()
	case "P":
		if m.viewMode == viewUsers {
			return m.startPasswd(), nil
		}
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		if m.showConfig {
			m.showConfig = false
			m.message = ""
			return m, nil
		}
		if m.showSnaps {
			m.showSnaps = false
			m.message = ""
			return m, nil
		}
		if m.viewMode != viewGuests {
			m.viewMode = viewGuests
			m.message = ""
			return m, nil
		}
		return m, tea.Quit
	case "enter":
		if m.viewMode != viewGuests {
			return m, nil
		}
		guest, ok := m.selectedGuest()
		if ok {
			m.showConfig = true
			m.configScroll = 0
			m.configVMID = guest.VMID
			m.config = nil
			m.configErr = nil
			m.message = fmt.Sprintf("loading config for %d", guest.VMID)
			return m, m.loadConfig(guest.VMID)
		}
	case "/":
		m.inputMode = inputSearch
		m.input = m.query
		m.message = ""
	case "u":
		m.toggleView()
		if m.viewMode == viewUsers && len(m.data.users) == 0 {
			m.loading = true
			return m, m.loadUsers
		}
	case "L":
		m.viewMode = viewLog
		m.showConfig = false
		m.unreadErrors = 0
		m.selectedLog = 0
	case "g":
		m.viewMode = viewGuests
		m.showConfig = false
		m.message = ""
	case "p":
		m.viewMode = viewUpdates
		m.message = "package updates"
		if len(m.data.updates) == 0 {
			m.loading = true
			return m, m.loadUpdates
		}
	case "b":
		m.viewMode = viewBackups
		m.message = "backup jobs"
		if m.backupItemCount() == 0 {
			m.loading = true
			return m, m.loadBackups
		}
	case "tab":
		if m.viewMode == viewBackups {
			if m.backupMode == "jobs" {
				m.backupMode = "volumes"
			} else {
				m.backupMode = "jobs"
			}
			m.selectedBackup = 0
		} else if m.viewMode == viewGuests {
			m.nextGuestTypeFilter()
			m.selected = 0
		} else if m.viewMode == viewLog {
			m.nextLogSource()
			m.selectedLog = 0
		}
	case "f":
		if m.viewMode == viewLog {
			m.nextLogLevel()
			m.selectedLog = 0
			break
		}
		m.nextFilter()
		m.selected = 0
	case "t":
		m.nextSort()
		m.selected = 0
	case "c":
		m.query = ""
		m.resetActiveSelection()
	case "up", "k":
		if m.showConfig {
			m.configScroll = max(0, m.configScroll-1)
			break
		}
		m.moveSelection(-1)
	case "down", "j":
		if m.showConfig {
			m.configScroll++
			break
		}
		m.moveSelection(1)
	case "+":
		if m.viewMode == viewGuests {
			if guest, ok := m.selectedGuest(); ok {
				m.openConfirm("start-guest", "START GUEST", fmt.Sprintf("%d", guest.VMID), guest.VMID, fmt.Sprintf("Start %s %d (%s) ?", strings.ToUpper(string(guest.Type)), guest.VMID, guest.Name))
			}
		}
	case "-":
		if m.viewMode == viewGuests {
			if guest, ok := m.selectedGuest(); ok {
				m.openConfirm("stop-guest", "STOP GUEST", fmt.Sprintf("%d", guest.VMID), guest.VMID, fmt.Sprintf("Stop %s %d (%s) ?", strings.ToUpper(string(guest.Type)), guest.VMID, guest.Name))
			}
		}
	case "A":
		if m.viewMode == viewGuests {
			if guest, ok := m.selectedGuest(); ok {
				if guest.Type == proxmox.GuestTypeLXC {
					m.message = proxmox.ErrNoAgent.Error()
					break
				}
				m.message = fmt.Sprintf("reading the guest agent of %d", guest.VMID)
				return m, m.loadAgent(guest.VMID, guest.Name)
			}
		}
	case "R", "S":
		if m.viewMode == viewGuests {
			if guest, ok := m.selectedGuest(); ok {
				m.confirmGuestRestart(guest, msg.String() == "R")
			}
		}
	case "a":
		// Always the same way in: refresh the index, then ask, then upgrade.
		if m.viewMode == viewUpdates && !m.upgradeRunning {
			return m.startUpgrade()
		}
	case "r":
		m.loading = true
		if m.viewMode == viewUsers {
			return m, m.loadUsers
		}
		if m.viewMode == viewUpdates {
			return m, m.loadUpdates
		}
		if m.viewMode == viewBackups {
			return m, m.loadBackups
		}
		return m, m.load
	case "s":
		if m.viewMode == viewUsers {
			user, ok := m.selectedUserModel()
			if ok {
				m.tfaUserID = user.UserID
				m.tfaEntries = nil
				m.tfaErr = nil
				m.message = fmt.Sprintf("loading TFA for %s", user.UserID)
				return m, m.loadUserTFA(user.UserID)
			}
			return m, nil
		}
		guest, ok := m.selectedGuest()
		if ok {
			m.showSnaps = true
			m.snapshotVMID = guest.VMID
			m.snapshots = nil
			m.selectedSnapshot = 0
			m.snapErr = nil
			m.message = fmt.Sprintf("loading snapshots for %d", guest.VMID)
			return m, m.loadSnapshots(guest.VMID)
		}
	case "n":
		if m.viewMode == viewUsers {
			return m.startEnroll()
		}
		guest, ok := m.selectedGuest()
		if ok {
			m.openModal(modalSnapshot, guest.VMID, "", "")
		}
	case "m":
		if m.viewMode == viewUsers || m.viewMode == viewUpdates {
			return m, nil
		}
		vmid := 0
		if m.viewMode == viewBackups {
			vmid = m.selectedBackupVMID()
		} else if guest, ok := m.selectedGuest(); ok {
			vmid = guest.VMID
		}
		if vmid > 0 {
			m.openModal(modalBackup, vmid, "", m.defaultBackupStorage())
		}
	case "e":
		if m.viewMode == viewBackups && m.backupMode == "jobs" {
			job, ok := m.selectedBackupJob()
			if !ok {
				return m, nil
			}
			m.openModal(modalSchedule, 0, job.ID, job.Storage)
			m.modalInput = job.Schedule
			m.modalVMIDs = job.VMID
		}
	case "d":
		if m.viewMode == viewBackups && m.backupMode != "jobs" {
			if volume, ok := m.selectedBackupVolume(); ok {
				m.openConfirm("delete-backup", "DELETE BACKUP", volume.VolID, 0, "Delete backup "+volume.VolID+" ?")
			}
		} else if m.viewMode == viewGuests && m.showSnaps {
			if snap, ok := m.selectedSnapshotModel(); ok && snap.Name != "current" {
				m.openConfirm("delete-snapshot", "DELETE SNAPSHOT", snap.Name, m.snapshotVMID, fmt.Sprintf("Delete snapshot %s for %d ?", snap.Name, m.snapshotVMID))
			}
		}
	case "o":
		if m.viewMode == viewBackups && m.backupMode != "jobs" {
			if volume, ok := m.selectedBackupVolume(); ok {
				m.openConfirm("restore-backup", "RESTORE BACKUP", volume.VolID, volume.VMID, fmt.Sprintf("Restore %s to VMID %d ? Any existing guest %d will be OVERWRITTEN (it must be stopped).", volume.VolID, volume.VMID, volume.VMID))
			}
		} else if m.viewMode == viewGuests && m.showSnaps {
			if snap, ok := m.selectedSnapshotModel(); ok && snap.Name != "current" {
				m.openConfirm("restore-snapshot", "RESTORE SNAPSHOT", snap.Name, m.snapshotVMID, fmt.Sprintf("Rollback guest %d to snapshot %s ?", m.snapshotVMID, snap.Name))
			}
		}
	case "x":
		if m.viewMode == viewLog {
			cleared := len(m.entries)
			m.entries, m.unreadErrors, m.selectedLog = nil, 0, 0
			m.message = fmt.Sprintf("log cleared (%d entries)", cleared)
		}
		if m.viewMode == viewUsers {
			user, ok := m.selectedUserModel()
			if ok {
				m.openConfirm("disable-tfa", "DISABLE TWO-FACTOR", user.UserID, 0, fmt.Sprintf("Remove every two-factor entry of %s ?", user.UserID))
			}
		}
	}
	return m, nil
}

// updateResult applies the result of an asynchronous command to the model.
func (m Model) updateResult(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case loadMsg:
		m.loading = false
		next := snapshot(msg)
		next.users = m.data.users
		next.updates = m.data.updates
		next.backups = m.data.backups
		next.backupVolumes = m.data.backupVolumes
		next.updateErr = m.data.updateErr
		next.backupErr = m.data.backupErr
		m.data = next
		m.clampSelections()
		if next.err != nil {
			m = m.logErrorQuiet("refresh guests and storages", next.err)
		}
	case tickMsg:
		cmds := []tea.Cmd{tick()}
		if !m.loading {
			m.loading = true
			cmds = append(cmds, m.load)
		}
		if !m.clusterTasks.loading {
			m.clusterTasks.loading = true
			cmds = append(cmds, m.loadClusterTasks)
		}
		return m, tea.Batch(cmds...)
	case clusterTasksMsg:
		m = m.handleClusterTasks(msg)
	case snapshotsMsg:
		if msg.vmid == m.snapshotVMID {
			m.snapshots = msg.snapshots
			m.selectedSnapshot = clampIndex(m.selectedSnapshot, len(msg.snapshots))
			m.snapErr = msg.err
			if msg.err != nil {
				m = m.logError(fmt.Sprintf("list snapshots of guest %d", msg.vmid), msg.err)
			} else {
				m.message = fmt.Sprintf("%d snapshots loaded for %d", len(msg.snapshots), msg.vmid)
			}
		}
	case createSnapshotMsg:
		if msg.err != nil {
			m = m.logError(fmt.Sprintf("create snapshot %s for guest %d", msg.name, msg.vmid), msg.err)
		} else {
			m.message = fmt.Sprintf("snapshot %s created for %d", msg.name, msg.vmid)
			if msg.upid == "" {
				// With a task, its result is logged when it ends.
				m = m.logDone(fmt.Sprintf("create snapshot %s for guest %d", msg.name, msg.vmid), "done")
			}
			m.showSnaps = true
			m.snapshotVMID = msg.vmid
			if msg.upid != "" {
				m.message = fmt.Sprintf("snapshot %s started for %d (task running)", msg.name, msg.vmid)
				m = m.track(msg.upid, fmt.Sprintf("snapshot %s for %d", msg.name, msg.vmid), refreshSnapshots, msg.vmid)
				// Show the list now (the panel opens on it), and again when the task ends.
				return m, tea.Batch(m.loadSnapshots(msg.vmid), taskTick(msg.upid))
			}
			return m, m.loadSnapshots(msg.vmid)
		}
	case runBackupMsg:
		if msg.err != nil {
			m = m.logError(fmt.Sprintf("start backup of guest %d", msg.vmid), msg.err)
		} else {
			m.message = fmt.Sprintf("backup started for %d (running in background) %s", msg.vmid, msg.upid)
			m.viewMode = viewBackups
			if msg.upid != "" {
				m = m.track(msg.upid, fmt.Sprintf("backup of %d", msg.vmid), refreshBackups, msg.vmid)
				return m, tea.Batch(m.loadBackups, taskTick(msg.upid))
			}
			return m, m.loadBackups
		}
	case updateBackupScheduleMsg:
		if msg.err != nil {
			m = m.logError("update backup job "+msg.id, msg.err)
		} else {
			m.message = fmt.Sprintf("backup job %s scheduled %s", msg.id, msg.schedule)
			m = m.logDone("update backup job "+msg.id, "scheduled "+msg.schedule)
			m.viewMode = viewBackups
			m.backupMode = "jobs"
			return m, m.loadBackups
		}
	case configMsg:
		if msg.vmid == m.configVMID {
			m.config = msg.config
			m.configErr = msg.err
			if msg.err != nil {
				m = m.logError(fmt.Sprintf("read configuration of guest %d", msg.vmid), msg.err)
			} else {
				m.message = fmt.Sprintf("config loaded for %d", msg.vmid)
			}
		}
	case userTFAMsg:
		if msg.err == nil {
			// What Proxmox says is the truth: the list follows it.
			m = m.withTFA(msg.userid, len(msg.entries) > 0)
		}
		if msg.userid == m.tfaUserID {
			m.tfaEntries = msg.entries
			m.tfaErr = msg.err
			if msg.err != nil {
				m = m.logError("read TFA entries of "+msg.userid, msg.err)
			} else {
				m.message = fmt.Sprintf("%d TFA entries for %s", len(msg.entries), msg.userid)
			}
		}
	case enrollMsg:
		return m.applyEnroll(msg)
	case recoveryMsg:
		return m.applyRecovery(msg), nil
	case passwdMsg:
		return m.applyPasswd(msg), nil
	case guestNetMsg:
		m = m.handleGuestNet(msg)
	case agentStateMsg:
		if msg.err != nil {
			m = m.logError(fmt.Sprintf("read the guest agent of %d", msg.vmid), msg.err)
			break
		}
		m.confirmAgent(msg)
	case agentSetMsg:
		context := fmt.Sprintf("%s the guest agent of %d", agentVerb(msg.enable), msg.vmid)
		if msg.err != nil {
			m = m.logError(context, msg.err)
			break
		}
		m = m.forgetGuestNet(msg.vmid)
		m.message = fmt.Sprintf("guest agent %sd for %d: stop and start the VM to apply it", agentVerb(msg.enable), msg.vmid)
		m = m.logDone(context, "done, applies after a full stop and start")
		if m.showConfig && m.configVMID == msg.vmid {
			return m, m.loadConfig(msg.vmid)
		}
	case disableTFAMsg:
		if msg.err != nil {
			m = m.logError("disable TFA of "+msg.userid, msg.err)
		} else {
			m.message = fmt.Sprintf("TFA disabled for %s", msg.userid)
			m = m.logDone("disable TFA of "+msg.userid, "done")
			m = m.withTFA(msg.userid, false)
			m.tfaUserID, m.tfaEntries, m.tfaErr = msg.userid, nil, nil
			return m, m.loadUserTFA(msg.userid)
		}
	case usersMsg:
		m.loading = false
		m.data.users = msg.users
		m.clampSelections()
		if msg.err != nil {
			m = m.logError("list users", msg.err)
		}
	case updatesMsg:
		m.loading = false
		m.data.updates = msg.updates
		m.data.updateErr = msg.err
		if msg.err != nil {
			m = m.logErrorQuiet("list package updates", msg.err)
		}
	case backupsMsg:
		m.loading = false
		m.data.backups = msg.jobs
		m.data.backupVolumes = msg.volumes
		m.data.backupErr = msg.err
		m.clampSelections()
		if msg.err != nil {
			m = m.logErrorQuiet("list backups", msg.err)
		}
	case destructiveActionMsg:
		if msg.err != nil {
			m = m.logError(actionContext(msg), msg.err)
		} else {
			if msg.upid != "" {
				m.message = fmt.Sprintf("%s started (task running)", msg.action)
				m = m.track(msg.upid, msg.action+" "+msg.name, destructiveRefresh(msg), msg.vmid)
				return m, taskTick(msg.upid)
			}
			m.message = fmt.Sprintf("%s completed", msg.action)
			m = m.logDone(actionContext(msg), "completed")
			if strings.Contains(msg.action, "backup") {
				return m, m.loadBackups
			}
			if strings.Contains(msg.action, "guest") {
				return m, m.load
			}
			if msg.vmid > 0 {
				return m, m.loadSnapshots(msg.vmid)
			}
		}
	case spinMsg:
		return m, spinTick(m.busy())
	case taskPollMsg:
		if m.taskIndex(msg.upid) < 0 {
			return m, nil
		}
		return m, m.pollTask(msg.upid)
	case taskResultMsg:
		return m.handleTaskResult(msg)
	case updateStartMsg:
		if m.upd.phase == updateIdle && m.updater != nil {
			m.upd.phase = updateChecking
			return m, m.checkUpdate(false)
		}
	case updateCheckMsg:
		m = m.handleUpdateCheck(msg)
	case updateGuardMsg:
		m.upd.guard = false
	case updateStepMsg:
		return m.handleUpdateStep(msg)
	case updateInstallDoneMsg:
		m = m.handleUpdateDone(msg)
	case upgradeLineMsg:
		return m.handleUpgradeLine(msg)
	case upgradeStepDoneMsg:
		return m.handleUpgradeStepDone(msg)
	case upgradeListMsg:
		m = m.handleUpgradeList(msg)
	}

	return m, nil
}
