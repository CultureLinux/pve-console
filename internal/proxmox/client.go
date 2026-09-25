package proxmox

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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

func (c Client) CreateSnapshot(ctx context.Context, vmid int, name string, description string) error {
	node, err := c.nodeName()
	if err != nil {
		return err
	}

	if err := c.createSnapshotByType(ctx, node, vmid, GuestTypeQEMU, name, description); err == nil {
		return nil
	}

	if err := c.createSnapshotByType(ctx, node, vmid, GuestTypeLXC, name, description); err == nil {
		return nil
	}

	return fmt.Errorf("guest %d was not found as QEMU VM or LXC container", vmid)
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

func (c Client) snapshotsByType(ctx context.Context, node string, vmid int, typ GuestType) ([]Snapshot, error) {
	var snapshots []Snapshot
	path := fmt.Sprintf("/nodes/%s/%s/%d/snapshot", node, pathForGuestType(typ), vmid)
	if err := c.pveshJSON(ctx, &snapshots, "get", path, "--output-format", "json"); err != nil {
		return nil, err
	}

	return snapshots, nil
}

func (c Client) createSnapshotByType(ctx context.Context, node string, vmid int, typ GuestType, name string, description string) error {
	args := []string{"create", fmt.Sprintf("/nodes/%s/%s/%d/snapshot", node, pathForGuestType(typ), vmid), "-snapname", name}
	if description != "" {
		args = append(args, "-description", description)
	}

	_, err := c.Runner.Run(ctx, "pvesh", args...)
	return err
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
