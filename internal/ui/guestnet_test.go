package ui

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"pvc/internal/proxmox"
)

var ansiCodes = regexp.MustCompile("\x1b\\[[0-9;]*m")

func noColor(s string) string { return ansiCodes.ReplaceAllString(s, "") }

func TestGuestNetLine(t *testing.T) {
	qemu := proxmox.Guest{VMID: 200, Type: proxmox.GuestTypeQEMU, Status: "running"}
	lxc := proxmox.Guest{VMID: 400, Type: proxmox.GuestTypeLXC, Status: "running"}
	tests := []struct {
		name  string
		guest proxmox.Guest
		entry *guestNetEntry
		want  string
	}{
		{"not read yet", qemu, nil, ""},
		{"read failed", qemu, &guestNetEntry{err: errors.New("boom")}, ""},
		{"agent detected", qemu, &guestNetEntry{net: proxmox.GuestNet{AgentEnabled: true, AgentRunning: true, IPv4: "192.168.1.201"}}, "● agent  192.168.1.201"},
		{"agent without IPv4", qemu, &guestNetEntry{net: proxmox.GuestNet{AgentEnabled: true, AgentRunning: true}}, "● agent  no IPv4"},
		{"agent silent", qemu, &guestNetEntry{net: proxmox.GuestNet{AgentEnabled: true}}, "● agent  not responding"},
		{"agent off", qemu, &guestNetEntry{}, "○ agent off  A enable"},
		{"container", lxc, &guestNetEntry{net: proxmox.GuestNet{IPv4: "10.0.0.5"}}, "IP  10.0.0.5"},
		{"container without address", lxc, &guestNetEntry{}, ""},
		{"stopped guest", proxmox.Guest{VMID: 200, Type: proxmox.GuestTypeQEMU, Status: "stopped"}, &guestNetEntry{net: proxmox.GuestNet{AgentEnabled: true, AgentRunning: true, IPv4: "1.2.3.4"}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(&fakeRunner{})
			if tt.entry != nil {
				m.guestNets = map[int]guestNetEntry{tt.guest.VMID: *tt.entry}
			}
			if got := noColor(m.guestNetLine(tt.guest)); got != tt.want {
				t.Fatalf("line = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWantGuestNetReadsOnlyARunningSelectedGuestOnce(t *testing.T) {
	m := newTestModel(&fakeRunner{})

	// The first guest is stopped: nothing to read.
	if _, cmd := m.wantGuestNet(); cmd != nil {
		t.Fatal("a stopped guest must not be read")
	}

	m.selected = 1 // 301, running
	m, cmd := m.wantGuestNet()
	if cmd == nil || m.netInFlight != 301 {
		t.Fatalf("expected a read of 301, in flight = %d", m.netInFlight)
	}
	// One read at a time.
	if _, again := m.wantGuestNet(); again != nil {
		t.Fatal("a read is already running")
	}

	// A fresh answer is kept; a stale one is read again.
	m = m.handleGuestNet(guestNetMsg{vmid: 301, net: proxmox.GuestNet{AgentEnabled: true}})
	if m.netInFlight != 0 {
		t.Fatalf("in flight = %d", m.netInFlight)
	}
	if _, again := m.wantGuestNet(); again != nil {
		t.Fatal("a fresh answer must not be read again")
	}
	now := time.Now().Add(guestNetTTL + time.Second)
	old := clock
	clock = func() time.Time { return now }
	defer func() { clock = old }()
	if _, again := m.wantGuestNet(); again == nil {
		t.Fatal("a stale answer must be read again")
	}
}

func TestGuestNetFailureIsKeptAndQuiet(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m.selected = 1
	m, _ = m.wantGuestNet()
	m = m.handleGuestNet(guestNetMsg{vmid: 301, err: errors.New("boom")})
	if _, again := m.wantGuestNet(); again != nil {
		t.Fatal("a failed read must not be retried at once")
	}
	if strings.Contains(m.message, "boom") {
		t.Fatalf("a background read must not take the status line: %q", m.message)
	}
}

func TestSelectingARunningGuestReadsItsNetwork(t *testing.T) {
	runner := &fakeRunner{stdout: map[string]string{
		"/qemu/301/config":       `{"agent":"1"}`,
		"network-get-interfaces": `{"result":[{"name":"eth0","ip-addresses":[{"ip-address":"192.168.1.50","ip-address-type":"ipv4","prefix":24}]}]}`,
	}}
	m := newTestModel(runner)
	m, cmd := press(t, m, "down")
	if cmd == nil || m.netInFlight != 301 {
		t.Fatalf("in flight = %d", m.netInFlight)
	}
	m, _ = execute(t, m, cmd)
	if got := noColor(m.guestNetLine(m.data.guests[1])); got != "● agent  192.168.1.50" {
		t.Fatalf("line = %q", got)
	}
}
