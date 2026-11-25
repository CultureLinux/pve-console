package ui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"pvc/internal/proxmox"
)

// guestNetTTL is how long the network of a guest is kept before it is read again.
const guestNetTTL = 30 * time.Second

// guestNetEntry is the last read of the network of a guest.
type guestNetEntry struct {
	net proxmox.GuestNet
	err error
	at  time.Time
}

// guestNetMsg is the answer to the read of the network of a guest.
type guestNetMsg struct {
	vmid int
	net  proxmox.GuestNet
	err  error
}

func (m Model) loadGuestNet(vmid int, typ proxmox.GuestType) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		info, err := m.client.GuestNetwork(ctx, vmid, typ)
		return guestNetMsg{vmid: vmid, net: info, err: err}
	}
}

// wantGuestNet reads the network of the selected guest when it runs and nothing
// fresh is known of it. One read at a time, and never for a guest that is not
// running: the panel of the other guests costs nothing.
func (m Model) wantGuestNet() (Model, tea.Cmd) {
	if m.netInFlight != 0 || m.viewMode != viewGuests {
		return m, nil
	}
	guest, ok := m.selectedGuest()
	if !ok || guest.Status != "running" {
		return m, nil
	}
	if entry, known := m.guestNets[guest.VMID]; known && clock().Sub(entry.at) < guestNetTTL {
		return m, nil
	}
	m.netInFlight = guest.VMID
	return m, m.loadGuestNet(guest.VMID, guest.Type)
}

// handleGuestNet keeps the answer, a failed one too, so that a failure is not
// retried at every key press.
func (m Model) handleGuestNet(msg guestNetMsg) Model {
	if m.netInFlight == msg.vmid {
		m.netInFlight = 0
	}
	if m.guestNets == nil {
		m.guestNets = map[int]guestNetEntry{}
	}
	m.guestNets[msg.vmid] = guestNetEntry{net: msg.net, err: msg.err, at: clock()}
	if msg.err != nil {
		m = m.logErrorQuiet(fmt.Sprintf("read the network of guest %d", msg.vmid), msg.err)
	}
	return m
}

// forgetGuestNet drops what is known of a guest, to read it again at once.
func (m Model) forgetGuestNet(vmid int) Model {
	delete(m.guestNets, vmid)
	return m
}

// guestNetLine is the line under the status of the detail panel: the agent
// marker and the IPv4 of a running VM, the IPv4 of a running container. Empty
// when nothing is known (guest stopped, not read yet, read failed).
func (m Model) guestNetLine(guest proxmox.Guest) string {
	if guest.Status != "running" {
		return ""
	}
	entry, ok := m.guestNets[guest.VMID]
	if !ok || entry.err != nil {
		return ""
	}
	info := entry.net
	if guest.Type == proxmox.GuestTypeLXC {
		if info.IPv4 == "" {
			return ""
		}
		return styleMuted.Render("IP  ") + info.IPv4
	}

	switch {
	case !info.AgentEnabled:
		return styleMuted.Render(glyphDotOff+" agent off  ") + hintKey("A", "enable")
	case !info.AgentRunning:
		return styleWarn.Render(glyphDotOn+" agent") + styleMuted.Render("  not responding")
	case info.IPv4 == "":
		return styleOK.Render(glyphDotOn+" agent") + styleMuted.Render("  no IPv4")
	}
	return styleOK.Render(glyphDotOn+" agent") + "  " + info.IPv4
}
