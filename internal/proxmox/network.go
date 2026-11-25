package proxmox

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
)

// GuestNet is what the node can tell of the network of a running guest.
type GuestNet struct {
	// AgentEnabled is the "agent" option of a QEMU VM; AgentRunning tells that the
	// agent inside the VM answered. Both are false for a container.
	AgentEnabled bool
	AgentRunning bool
	// IPv4 is the first usable IPv4 address of the guest, without its prefix.
	IPv4 string
}

// skippedInterfaces are the prefixes of the interfaces that belong to
// containers or bridges inside the guest, not to the guest itself.
var skippedInterfaces = []string{"lo", "docker", "br-", "veth", "virbr", "cni", "flannel", "cali"}

// usableIPv4 returns the address if it is a unicast IPv4 that identifies the
// guest on the network (no loopback, no link-local), without any prefix length.
func usableIPv4(iface string, address string) (string, bool) {
	for _, prefix := range skippedInterfaces {
		if strings.HasPrefix(iface, prefix) {
			return "", false
		}
	}
	address, _, _ = strings.Cut(address, "/")
	addr, err := netip.ParseAddr(address)
	if err != nil || !addr.Is4() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsUnspecified() {
		return "", false
	}

	return addr.String(), true
}

// GuestNetwork reads the agent state and the IPv4 of a running guest. A QEMU VM
// whose agent does not answer is not an error: it is reported in AgentRunning.
func (c Client) GuestNetwork(ctx context.Context, vmid int, typ GuestType) (GuestNet, error) {
	node, err := c.nodeName()
	if err != nil {
		return GuestNet{}, err
	}
	if typ == GuestTypeLXC {
		return c.containerNetwork(ctx, node, vmid)
	}

	config, err := c.guestConfigByType(ctx, node, vmid, GuestTypeQEMU)
	if err != nil {
		return GuestNet{}, fmt.Errorf("unable to read the configuration of %d: %w", vmid, err)
	}
	info := GuestNet{AgentEnabled: AgentEnabled(config)}
	if !info.AgentEnabled {
		return info, nil
	}

	var reply struct {
		Result []struct {
			Name      string `json:"name"`
			Addresses []struct {
				Address string `json:"ip-address"`
			} `json:"ip-addresses"`
		} `json:"result"`
	}
	path := fmt.Sprintf("/nodes/%s/qemu/%d/agent/network-get-interfaces", node, vmid)
	if err := c.pveshJSON(ctx, &reply, "get", path, "--output-format", "json"); err != nil {
		// Service stopped inside the VM, VM still starting, call too slow: the
		// agent is not there to answer, which is an answer.
		return info, nil
	}
	info.AgentRunning = true
	for _, iface := range reply.Result {
		for _, address := range iface.Addresses {
			if ip, ok := usableIPv4(iface.Name, address.Address); ok {
				info.IPv4 = ip
				return info, nil
			}
		}
	}

	return info, nil
}

// containerNetwork reads the IPv4 of a running container, which needs no agent.
func (c Client) containerNetwork(ctx context.Context, node string, vmid int) (GuestNet, error) {
	var interfaces []struct {
		Name string `json:"name"`
		Inet string `json:"inet"`
	}
	path := fmt.Sprintf("/nodes/%s/lxc/%d/interfaces", node, vmid)
	if err := c.pveshJSON(ctx, &interfaces, "get", path, "--output-format", "json"); err != nil {
		return GuestNet{}, fmt.Errorf("unable to read the interfaces of %d: %w", vmid, err)
	}
	for _, iface := range interfaces {
		if ip, ok := usableIPv4(iface.Name, iface.Inet); ok {
			return GuestNet{IPv4: ip}, nil
		}
	}

	return GuestNet{}, nil
}
