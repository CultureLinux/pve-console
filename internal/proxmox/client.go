package proxmox

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"pvc/internal/command"
	"pvc/internal/validate"
)

type Client struct {
	Runner command.Runner
	Node   string
}

func NewClient(runner command.Runner) Client {
	return Client{Runner: runner}
}

func (c Client) nodeName() (string, error) {
	if c.Node != "" {
		return c.Node, nil
	}

	name, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("unable to detect local hostname: %w", err)
	}

	return name, nil
}

func (c Client) NodeInfo(ctx context.Context) (NodeInfo, error) {
	node, err := c.nodeName()
	if err != nil {
		return NodeInfo{}, err
	}

	info := NodeInfo{Name: node}

	if result, err := c.Runner.Run(ctx, "pveversion"); err == nil {
		info.ProxmoxVersion = strings.TrimSpace(string(result.Stdout))
	}
	if result, err := c.Runner.Run(ctx, "uname", "-r"); err == nil {
		info.Kernel = strings.TrimSpace(string(result.Stdout))
	}

	return info, nil
}

func (c Client) Guests(ctx context.Context) ([]Guest, error) {
	node, err := c.nodeName()
	if err != nil {
		return nil, err
	}

	qemu, err := c.guestsByType(ctx, node, GuestTypeQEMU)
	if err != nil {
		return nil, err
	}

	lxc, err := c.guestsByType(ctx, node, GuestTypeLXC)
	if err != nil {
		return nil, err
	}

	return append(qemu, lxc...), nil
}

func (c Client) GuestStatus(ctx context.Context, vmid int) (Guest, error) {
	node, err := c.nodeName()
	if err != nil {
		return Guest{}, err
	}
	return byGuestType(guestNotFound(vmid), func(typ GuestType) (Guest, error) {
		return c.guestStatusByType(ctx, node, vmid, typ)
	})
}

func (c Client) GuestConfig(ctx context.Context, vmid int) (GuestConfig, error) {
	node, err := c.nodeName()
	if err != nil {
		return nil, err
	}
	return byGuestType(guestNotFound(vmid), func(typ GuestType) (GuestConfig, error) {
		return c.guestConfigByType(ctx, node, vmid, typ)
	})
}

func (c Client) Storages(ctx context.Context) ([]Storage, error) {
	node, err := c.nodeName()
	if err != nil {
		return nil, err
	}

	var storages []Storage
	if err := c.pveshJSON(ctx, &storages, "get", fmt.Sprintf("/nodes/%s/storage", node), "--output-format", "json"); err != nil {
		return nil, fmt.Errorf("unable to list storages: %w", err)
	}

	for i := range storages {
		if storages[i].Total > 0 {
			storages[i].Usage = float64(storages[i].Used) / float64(storages[i].Total)
		}
	}

	return storages, nil
}

// StartGuest starts a guest and returns the UPID of the task, if Proxmox created one.
func (c Client) StartGuest(ctx context.Context, vmid int) (string, error) {
	return c.guestPowerAction(ctx, vmid, "start")
}

// StopGuest stops a guest immediately and returns the UPID of the task, if any.
func (c Client) StopGuest(ctx context.Context, vmid int) (string, error) {
	return c.guestPowerAction(ctx, vmid, "stop")
}

// RebootGuest asks a running guest to reboot cleanly and returns the UPID of the task, if any.
func (c Client) RebootGuest(ctx context.Context, vmid int) (string, error) {
	return c.guestPowerAction(ctx, vmid, "reboot")
}

// ShutdownGuest asks a running guest to shut down cleanly (ACPI for a VM) and
// returns the UPID of the task, if any. Unlike StopGuest it does not power the
// guest off by force.
func (c Client) ShutdownGuest(ctx context.Context, vmid int) (string, error) {
	return c.guestPowerAction(ctx, vmid, "shutdown")
}
func (c Client) Snapshots(ctx context.Context, vmid int) ([]Snapshot, error) {
	node, err := c.nodeName()
	if err != nil {
		return nil, err
	}
	return byGuestType(guestNotFound(vmid), func(typ GuestType) ([]Snapshot, error) {
		return c.snapshotsByType(ctx, node, vmid, typ)
	})
}

// CreateSnapshot creates a snapshot and returns the UPID of the task, if any.
func (c Client) CreateSnapshot(ctx context.Context, vmid int, name string, description string, vmstate bool) (string, error) {
	node, err := c.nodeName()
	if err != nil {
		return "", err
	}
	typ, err := c.resolveGuestType(ctx, node, vmid)
	if err != nil {
		return "", err
	}
	if vmstate && typ == GuestTypeQEMU && c.hasVirtiofs(ctx, node, vmid) {
		// Proxmox refuses a snapshot with RAM for a VM with a virtiofs share.
		vmstate = false
	}
	return c.createSnapshotByType(ctx, node, vmid, typ, name, description, vmstate)
}

// hasVirtiofs reports whether the VM config has a virtiofs share. A config that
// cannot be read counts as "no": the snapshot is then tried as asked and
// Proxmox says what it thinks.
func (c Client) hasVirtiofs(ctx context.Context, node string, vmid int) bool {
	config, err := c.guestConfigByType(ctx, node, vmid, GuestTypeQEMU)
	if err != nil {
		return false
	}
	for key := range config {
		if strings.HasPrefix(key, "virtiofs") {
			return true
		}
	}
	return false
}

// DeleteSnapshot deletes a snapshot and returns the UPID of the task, if any.
func (c Client) DeleteSnapshot(ctx context.Context, vmid int, name string) (string, error) {
	node, err := c.nodeName()
	if err != nil {
		return "", err
	}
	typ, err := c.resolveGuestType(ctx, node, vmid)
	if err != nil {
		return "", err
	}
	return c.deleteSnapshotByType(ctx, node, vmid, typ, name)
}

// RollbackSnapshot rolls a guest back to a snapshot and returns the UPID of the task, if any.
func (c Client) RollbackSnapshot(ctx context.Context, vmid int, name string) (string, error) {
	node, err := c.nodeName()
	if err != nil {
		return "", err
	}
	typ, err := c.resolveGuestType(ctx, node, vmid)
	if err != nil {
		return "", err
	}
	return c.rollbackSnapshotByType(ctx, node, vmid, typ, name)
}
func (c Client) Users(ctx context.Context) ([]User, error) {
	var users []User
	if err := c.pveshJSON(ctx, &users, "get", "/access/users", "--output-format", "json"); err != nil {
		return nil, fmt.Errorf("unable to list users: %w", err)
	}

	return users, nil
}

func (c Client) UserTFA(ctx context.Context, userid string) ([]TFAEntry, error) {
	if err := validate.UserID(userid); err != nil {
		return nil, err
	}
	result, err := c.Runner.Run(ctx, "pvesh", "get", fmt.Sprintf("/access/users/%s/tfa", userid), "--output-format", "json")
	if err != nil {
		return nil, fmt.Errorf("unable to list TFA entries for user %s: %w", userid, err)
	}

	entries, err := parseTFAEntries(result.Stdout)
	if err != nil {
		return nil, fmt.Errorf("unable to list TFA entries for user %s: %w", userid, err)
	}

	return entries, nil
}

func (c Client) DisableUserTFA(ctx context.Context, userid string) error {
	if err := validate.UserID(userid); err != nil {
		return err
	}
	_, err := c.Runner.Run(ctx, "pveum", "user", "tfa", "delete", userid)
	if err != nil {
		return fmt.Errorf("unable to disable TFA for user %s: %w", userid, err)
	}

	return nil
}

func (c Client) PackageUpdates(ctx context.Context) ([]PackageUpdate, error) {
	result, err := c.Runner.Run(ctx, "apt", "list", "--upgradable")
	if err != nil {
		return nil, fmt.Errorf("unable to list package updates: %w", err)
	}
	return parseAptUpdates(result.Stdout), nil
}

func (c Client) BackupJobs(ctx context.Context) ([]BackupJob, error) {
	var jobs []BackupJob
	if err := c.pveshJSON(ctx, &jobs, "get", "/cluster/backup", "--output-format", "json"); err != nil {
		return nil, fmt.Errorf("unable to list backup jobs: %w", err)
	}
	return jobs, nil
}

func (c Client) BackupVolumes(ctx context.Context, storages []Storage) ([]BackupVolume, error) {
	node, err := c.nodeName()
	if err != nil {
		return nil, err
	}

	volumes := make([]BackupVolume, 0)
	var lastErr error
	for _, storage := range storages {
		if !bool(storage.Active) {
			continue
		}
		var storageVolumes []BackupVolume
		path := fmt.Sprintf("/nodes/%s/storage/%s/content", node, storage.Name)
		err := c.pveshJSON(ctx, &storageVolumes, "get", path, "-content", "backup", "--output-format", "json")
		if err != nil {
			lastErr = err
			continue
		}
		volumes = append(volumes, storageVolumes...)
	}
	if len(volumes) == 0 && lastErr != nil {
		return nil, fmt.Errorf("unable to list backup volumes: %w", lastErr)
	}
	return volumes, nil
}

// DeleteBackupVolume deletes a backup volume and returns the UPID of the task, if any.
func (c Client) DeleteBackupVolume(ctx context.Context, volid string) (string, error) {
	if err := validate.VolID(volid); err != nil {
		return "", err
	}
	node, err := c.nodeName()
	if err != nil {
		return "", err
	}
	storage := storageFromVolID(volid)
	if storage == "" {
		return "", fmt.Errorf("invalid backup volume id %q", volid)
	}
	path := fmt.Sprintf("/nodes/%s/storage/%s/content/%s", node, storage, volid)
	result, err := c.Runner.Run(ctx, "pvesh", "delete", path)
	if err != nil {
		return "", fmt.Errorf("unable to delete backup %s: %w", volid, err)
	}
	return parseUPID(result.Stdout), nil
}

// RestoreBackupVolume restores a backup over its original VMID. It refuses to
// run while a guest with that VMID is running, because the restore overwrites it.
func (c Client) RestoreBackupVolume(ctx context.Context, volume BackupVolume) error {
	if err := validate.VolID(volume.VolID); err != nil {
		return err
	}
	if volume.VMID <= 0 {
		return fmt.Errorf("backup %s has no VMID", volume.VolID)
	}
	// An error here means the guest does not exist (or is unreadable): nothing to overwrite.
	if guest, err := c.GuestStatus(ctx, volume.VMID); err == nil && guest.Status == "running" {
		return fmt.Errorf("guest %d is running: stop it before restoring %s", volume.VMID, volume.VolID)
	}

	vmid := fmt.Sprintf("%d", volume.VMID)
	if isLXCBackup(volume) {
		if _, err := c.Runner.Run(ctx, "pct", "restore", vmid, volume.VolID, "--force", "1"); err != nil {
			return fmt.Errorf("unable to restore backup %s: %w", volume.VolID, err)
		}
		return nil
	}
	if _, err := c.Runner.Run(ctx, "qmrestore", volume.VolID, vmid, "--force"); err != nil {
		return fmt.Errorf("unable to restore backup %s: %w", volume.VolID, err)
	}
	return nil
}

// isLXCBackup tells container backups from VM backups. The subtype reported by
// Proxmox wins; volume id and format are only used as a fallback (including
// Proxmox Backup Server volumes such as "pbs:backup/ct/100/...").
func isLXCBackup(volume BackupVolume) bool {
	switch volume.Subtype {
	case "lxc":
		return true
	case "qemu":
		return false
	}
	return strings.Contains(volume.VolID, "vzdump-lxc-") ||
		strings.Contains(volume.VolID, ":backup/ct/") ||
		volume.Format == "pbs-ct" ||
		strings.Contains(volume.Format, "tar")
}

func storageFromVolID(volid string) string {
	storage, _, ok := strings.Cut(volid, ":")
	if !ok {
		return ""
	}
	return storage
}

// RunBackup starts a vzdump task and returns its UPID. The task keeps running
// after this call returns: success only means it was accepted.
func (c Client) RunBackup(ctx context.Context, vmid int, storage string) (string, error) {
	if vmid <= 0 {
		return "", fmt.Errorf("invalid VMID %d", vmid)
	}
	if storage != "" {
		if err := validate.StorageName(storage); err != nil {
			return "", err
		}
	}
	node, err := c.nodeName()
	if err != nil {
		return "", err
	}

	args := []string{"create", fmt.Sprintf("/nodes/%s/vzdump", node), "-vmid", fmt.Sprintf("%d", vmid), "-mode", "snapshot"}
	if storage != "" {
		args = append(args, "-storage", storage)
	}

	result, err := c.Runner.Run(ctx, "pvesh", args...)
	if err != nil {
		return "", fmt.Errorf("unable to start backup for guest %d: %w", vmid, err)
	}
	return parseUPID(result.Stdout), nil
}

// UpdateBackupJob changes the schedule of a backup job. An empty storage or
// vmids leaves that setting unchanged.
func (c Client) UpdateBackupJob(ctx context.Context, id string, storage string, schedule string, vmids string) error {
	if err := validate.StorageName(id); err != nil {
		return fmt.Errorf("invalid backup job id: %w", err)
	}
	if err := validate.Schedule(schedule); err != nil {
		return err
	}
	if storage != "" {
		if err := validate.StorageName(storage); err != nil {
			return err
		}
	}
	if vmids != "" {
		if err := validate.VMIDList(vmids); err != nil {
			return err
		}
	}

	args := []string{"set", fmt.Sprintf("/cluster/backup/%s", id), "-schedule", schedule}
	if storage != "" {
		args = append(args, "-storage", storage)
	}
	if vmids != "" {
		args = append(args, "-vmid", vmids)
	}
	if _, err := c.Runner.Run(ctx, "pvesh", args...); err != nil {
		return fmt.Errorf("unable to update backup job %s: %w", id, err)
	}
	return nil
}

func (c Client) guestsByType(ctx context.Context, node string, typ GuestType) ([]Guest, error) {
	var guests []Guest
	path := fmt.Sprintf("/nodes/%s/%s", node, pathForGuestType(typ))
	if err := c.pveshJSON(ctx, &guests, "get", path, "--output-format", "json"); err != nil {
		return nil, fmt.Errorf("unable to list %s guests: %w", typ, err)
	}

	for i := range guests {
		guests[i].Type = typ
	}

	return guests, nil
}

func (c Client) guestPowerAction(ctx context.Context, vmid int, action string) (string, error) {
	node, err := c.nodeName()
	if err != nil {
		return "", err
	}
	typ, err := c.resolveGuestType(ctx, node, vmid)
	if err != nil {
		return "", err
	}
	return c.guestPowerActionByType(ctx, node, vmid, typ, action)
}

func (c Client) guestPowerActionByType(ctx context.Context, node string, vmid int, typ GuestType, action string) (string, error) {
	path := fmt.Sprintf("/nodes/%s/%s/%d/status/%s", node, pathForGuestType(typ), vmid, action)
	result, err := c.Runner.Run(ctx, "pvesh", "create", path)
	if err != nil {
		return "", err
	}
	return parseUPID(result.Stdout), nil
}
func (c Client) guestStatusByType(ctx context.Context, node string, vmid int, typ GuestType) (Guest, error) {
	var guest Guest
	path := fmt.Sprintf("/nodes/%s/%s/%d/status/current", node, pathForGuestType(typ), vmid)
	if err := c.pveshJSON(ctx, &guest, "get", path, "--output-format", "json"); err != nil {
		return Guest{}, err
	}

	guest.VMID = vmid
	guest.Type = typ
	return guest, nil
}

func (c Client) guestConfigByType(ctx context.Context, node string, vmid int, typ GuestType) (GuestConfig, error) {
	var config GuestConfig
	path := fmt.Sprintf("/nodes/%s/%s/%d/config", node, pathForGuestType(typ), vmid)
	if err := c.pveshJSON(ctx, &config, "get", path, "--output-format", "json"); err != nil {
		return nil, err
	}

	return config, nil
}

func (c Client) snapshotsByType(ctx context.Context, node string, vmid int, typ GuestType) ([]Snapshot, error) {
	var snapshots []Snapshot
	path := fmt.Sprintf("/nodes/%s/%s/%d/snapshot", node, pathForGuestType(typ), vmid)
	if err := c.pveshJSON(ctx, &snapshots, "get", path, "--output-format", "json"); err != nil {
		return nil, err
	}

	return snapshots, nil
}

func (c Client) createSnapshotByType(ctx context.Context, node string, vmid int, typ GuestType, name string, description string, vmstate bool) (string, error) {
	args := []string{"create", fmt.Sprintf("/nodes/%s/%s/%d/snapshot", node, pathForGuestType(typ), vmid), "-snapname", name}
	if description != "" {
		args = append(args, "-description", description)
	}
	if typ == GuestTypeQEMU && vmstate {
		args = append(args, "-vmstate", "1")
	}

	result, err := c.Runner.Run(ctx, "pvesh", args...)
	if err != nil {
		return "", err
	}
	return parseUPID(result.Stdout), nil
}

func (c Client) deleteSnapshotByType(ctx context.Context, node string, vmid int, typ GuestType, name string) (string, error) {
	path := fmt.Sprintf("/nodes/%s/%s/%d/snapshot/%s", node, pathForGuestType(typ), vmid, name)
	result, err := c.Runner.Run(ctx, "pvesh", "delete", path)
	if err != nil {
		return "", err
	}
	return parseUPID(result.Stdout), nil
}

func (c Client) rollbackSnapshotByType(ctx context.Context, node string, vmid int, typ GuestType, name string) (string, error) {
	path := fmt.Sprintf("/nodes/%s/%s/%d/snapshot/%s/rollback", node, pathForGuestType(typ), vmid, name)
	result, err := c.Runner.Run(ctx, "pvesh", "create", path)
	if err != nil {
		return "", err
	}
	return parseUPID(result.Stdout), nil
}

var aptUpdatePattern = regexp.MustCompile(`^([^/]+)/([^ ]+) ([^ ]+) .*\[upgradable from: ([^\]]+)\]`)

func parseAptUpdates(data []byte) []PackageUpdate {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	updates := make([]PackageUpdate, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Listing") {
			continue
		}
		match := aptUpdatePattern.FindStringSubmatch(line)
		if len(match) == 5 {
			updates = append(updates, PackageUpdate{Name: match[1], Suite: match[2], Candidate: match[3], Current: match[4]})
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			name := strings.SplitN(fields[0], "/", 2)[0]
			updates = append(updates, PackageUpdate{Name: name, Candidate: fields[1]})
		}
	}
	return updates
}

func parseTFAEntries(data []byte) ([]TFAEntry, error) {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid pvesh JSON: %w", err)
	}
	return collectTFAEntries(raw, ""), nil
}

func collectTFAEntries(value any, idHint string) []TFAEntry {
	switch typed := value.(type) {
	case []any:
		entries := make([]TFAEntry, 0, len(typed))
		for _, item := range typed {
			entries = append(entries, collectTFAEntries(item, "")...)
		}
		return entries
	case map[string]any:
		if _, ok := typed["type"]; ok {
			entry := tfaEntryFromMap(typed)
			if entry.ID == "" {
				entry.ID = idHint
			}
			return []TFAEntry{entry}
		}

		entries := make([]TFAEntry, 0)
		for key, item := range typed {
			if key == "recovery" {
				continue
			}
			entries = append(entries, collectTFAEntries(item, key)...)
		}
		return entries
	default:
		return nil
	}
}

func tfaEntryFromMap(raw map[string]any) TFAEntry {
	entry := TFAEntry{}
	if value, ok := raw["id"].(string); ok {
		entry.ID = value
	}
	if value, ok := raw["type"].(string); ok {
		entry.Type = value
	}
	if value, ok := raw["description"].(string); ok {
		entry.Description = value
	}
	if value, ok := raw["created"].(float64); ok {
		entry.Created = int64(value)
	}
	return entry
}

func (c Client) pveshJSON(ctx context.Context, target any, args ...string) error {
	result, err := c.Runner.Run(ctx, "pvesh", args...)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(result.Stdout, target); err != nil {
		return fmt.Errorf("invalid pvesh JSON: %w", err)
	}

	return nil
}

func pathForGuestType(typ GuestType) string {
	if typ == GuestTypeLXC {
		return "lxc"
	}
	return "qemu"
}
