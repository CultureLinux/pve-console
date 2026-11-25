package proxmox

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrNoAgent is returned for a guest that cannot have a QEMU guest agent.
var ErrNoAgent = errors.New("the guest agent only exists on QEMU VMs")

// AgentEnabled reports whether the "agent" option of a guest configuration
// turns the QEMU guest agent on. The option is "1", "0" or a list of
// properties such as "enabled=1,type=virtio"; a missing option means off.
func AgentEnabled(config GuestConfig) bool {
	raw, ok := config["agent"]
	if !ok {
		return false
	}
	for _, field := range strings.Split(fmt.Sprint(raw), ",") {
		field = strings.TrimSpace(field)
		switch {
		case field == "1", field == "enabled=1":
			return true
		case field == "0", field == "enabled=0":
			return false
		}
	}
	return false
}

// agentOption builds the new value of the "agent" option: only "enabled" changes,
// the other properties (type, fstrim_cloned_disks) are kept.
func agentOption(config GuestConfig, enable bool) string {
	enabled := "enabled=0"
	if enable {
		enabled = "enabled=1"
	}
	raw, ok := config["agent"]
	if !ok {
		return enabled
	}

	fields := []string{enabled}
	for i, field := range strings.Split(fmt.Sprint(raw), ",") {
		field = strings.TrimSpace(field)
		switch {
		case field == "":
		case i == 0 && !strings.Contains(field, "="):
			// A bare "0" / "1" is the "enabled" property.
		case strings.HasPrefix(field, "enabled="):
		default:
			fields = append(fields, field)
		}
	}
	return strings.Join(fields, ",")
}

// GuestAgent reports whether the guest agent is enabled for a QEMU VM.
func (c Client) GuestAgent(ctx context.Context, vmid int) (bool, error) {
	node, err := c.nodeName()
	if err != nil {
		return false, err
	}
	config, err := c.guestConfigByType(ctx, node, vmid, GuestTypeQEMU)
	if err != nil {
		return false, c.agentError(ctx, node, vmid, err)
	}

	return AgentEnabled(config), nil
}

// SetGuestAgent turns the guest agent option of a QEMU VM on or off. Proxmox
// applies it at the next full stop and start of the VM, not at a reboot from
// inside the guest.
func (c Client) SetGuestAgent(ctx context.Context, vmid int, enable bool) error {
	node, err := c.nodeName()
	if err != nil {
		return err
	}
	config, err := c.guestConfigByType(ctx, node, vmid, GuestTypeQEMU)
	if err != nil {
		return c.agentError(ctx, node, vmid, err)
	}

	path := fmt.Sprintf("/nodes/%s/qemu/%d/config", node, vmid)
	if _, err := c.Runner.Run(ctx, "pvesh", "set", path, "-agent", agentOption(config, enable)); err != nil {
		return fmt.Errorf("unable to set the guest agent of %d: %w", vmid, err)
	}

	return nil
}

// agentError explains why the QEMU configuration could not be read: the guest is
// a container, it does not exist, or the read really failed.
func (c Client) agentError(ctx context.Context, node string, vmid int, err error) error {
	if isRealError(err) {
		return fmt.Errorf("unable to read the configuration of %d: %w", vmid, err)
	}
	if typ, typeErr := c.resolveGuestType(ctx, node, vmid); typeErr == nil && typ == GuestTypeLXC {
		return ErrNoAgent
	}

	return guestNotFound(vmid)
}
