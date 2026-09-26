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

	if guest, err := c.guestStatusByType(ctx, node, vmid, GuestTypeQEMU); err == nil {
		return guest, nil
	}

	if guest, err := c.guestStatusByType(ctx, node, vmid, GuestTypeLXC); err == nil {
		return guest, nil
	}

	return Guest{}, fmt.Errorf("guest %d was not found as QEMU VM or LXC container", vmid)
}

func (c Client) GuestConfig(ctx context.Context, vmid int) (GuestConfig, error) {
	node, err := c.nodeName()
	if err != nil {
		return nil, err
	}

	if config, err := c.guestConfigByType(ctx, node, vmid, GuestTypeQEMU); err == nil {
		return config, nil
	}

	if config, err := c.guestConfigByType(ctx, node, vmid, GuestTypeLXC); err == nil {
		return config, nil
	}

	return nil, fmt.Errorf("guest %d was not found as QEMU VM or LXC container", vmid)
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

func (c Client) Snapshots(ctx context.Context, vmid int) ([]Snapshot, error) {
	node, err := c.nodeName()
	if err != nil {
		return nil, err
	}

	if snapshots, err := c.snapshotsByType(ctx, node, vmid, GuestTypeQEMU); err == nil {
		return snapshots, nil
	}

	if snapshots, err := c.snapshotsByType(ctx, node, vmid, GuestTypeLXC); err == nil {
		return snapshots, nil
	}

	return nil, fmt.Errorf("guest %d was not found as QEMU VM or LXC container", vmid)
}

func (c Client) CreateSnapshot(ctx context.Context, vmid int, name string, description string, vmstate bool) error {
	node, err := c.nodeName()
	if err != nil {
		return err
	}

	if err := c.createSnapshotByType(ctx, node, vmid, GuestTypeQEMU, name, description, vmstate); err == nil {
		return nil
	}

	if err := c.createSnapshotByType(ctx, node, vmid, GuestTypeLXC, name, description, vmstate); err == nil {
		return nil
	}

	return fmt.Errorf("guest %d was not found as QEMU VM or LXC container", vmid)
}

func (c Client) Users(ctx context.Context) ([]User, error) {
	var users []User
	if err := c.pveshJSON(ctx, &users, "get", "/access/users", "--output-format", "json"); err != nil {
		return nil, fmt.Errorf("unable to list users: %w", err)
	}

	return users, nil
}

func (c Client) UserTFA(ctx context.Context, userid string) ([]TFAEntry, error) {
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

func (c Client) RunBackup(ctx context.Context, vmid int, storage string) error {
	node, err := c.nodeName()
	if err != nil {
		return err
	}

	args := []string{"create", fmt.Sprintf("/nodes/%s/vzdump", node), "-vmid", fmt.Sprintf("%d", vmid), "-mode", "snapshot"}
	if storage != "" {
		args = append(args, "-storage", storage)
	}

	if _, err := c.Runner.Run(ctx, "pvesh", args...); err != nil {
		return fmt.Errorf("unable to start backup for guest %d: %w", vmid, err)
	}
	return nil
}

func (c Client) UpdateBackupSchedule(ctx context.Context, id string, schedule string) error {
	if _, err := c.Runner.Run(ctx, "pvesh", "set", fmt.Sprintf("/cluster/backup/%s", id), "-schedule", schedule); err != nil {
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

func (c Client) createSnapshotByType(ctx context.Context, node string, vmid int, typ GuestType, name string, description string, vmstate bool) error {
	args := []string{"create", fmt.Sprintf("/nodes/%s/%s/%d/snapshot", node, pathForGuestType(typ), vmid), "-snapname", name}
	if description != "" {
		args = append(args, "-description", description)
	}
	if typ == GuestTypeQEMU && vmstate {
		args = append(args, "-vmstate", "1")
	}

	_, err := c.Runner.Run(ctx, "pvesh", args...)
	return err
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
