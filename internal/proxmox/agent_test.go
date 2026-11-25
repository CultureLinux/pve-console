package proxmox

import (
	"context"
	"errors"
	"strings"
	"testing"

	"pvc/internal/command"
)

func TestAgentEnabled(t *testing.T) {
	tests := []struct {
		name   string
		config GuestConfig
		want   bool
	}{
		{"missing", GuestConfig{}, false},
		{"bare 1", GuestConfig{"agent": "1"}, true},
		{"bare 0", GuestConfig{"agent": "0"}, false},
		{"number", GuestConfig{"agent": float64(1)}, true},
		{"enabled", GuestConfig{"agent": "enabled=1,type=virtio"}, true},
		{"disabled", GuestConfig{"agent": "enabled=0,fstrim_cloned_disks=0,type=virtio"}, false},
		{"type first", GuestConfig{"agent": "type=virtio,enabled=1"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AgentEnabled(tt.config); got != tt.want {
				t.Fatalf("AgentEnabled(%v) = %v, want %v", tt.config, got, tt.want)
			}
		})
	}
}

func TestAgentOptionKeepsTheOtherProperties(t *testing.T) {
	tests := []struct {
		name   string
		config GuestConfig
		enable bool
		want   string
	}{
		{"missing on", GuestConfig{}, true, "enabled=1"},
		{"missing off", GuestConfig{}, false, "enabled=0"},
		{"bare", GuestConfig{"agent": "0"}, true, "enabled=1"},
		{"all properties", GuestConfig{"agent": "enabled=0,fstrim_cloned_disks=0,type=virtio"}, true, "enabled=1,fstrim_cloned_disks=0,type=virtio"},
		{"type first", GuestConfig{"agent": "type=isa,enabled=1"}, false, "enabled=0,type=isa"},
		{"no enabled", GuestConfig{"agent": "type=virtio"}, true, "enabled=1,type=virtio"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := agentOption(tt.config, tt.enable); got != tt.want {
				t.Fatalf("agentOption = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSetGuestAgent(t *testing.T) {
	runner := &command.MockRunner{Responses: map[string]command.Result{
		"pvesh get /nodes/pve/qemu/204/config --output-format json":                               {Stdout: []byte(`{"agent":"enabled=0,fstrim_cloned_disks=0,type=virtio","memory":"16384"}`)},
		"pvesh set /nodes/pve/qemu/204/config -agent enabled=1,fstrim_cloned_disks=0,type=virtio": {},
	}}
	if err := (Client{Runner: runner, Node: "pve"}).SetGuestAgent(context.Background(), 204, true); err != nil {
		t.Fatalf("SetGuestAgent: %v", err)
	}
}

func TestGuestAgentOfAContainer(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"pvesh get /nodes/pve/lxc/400/status/current --output-format json": {Stdout: []byte(`{"status":"running"}`)},
		},
		Errors: map[string]error{
			"pvesh get /nodes/pve/qemu/400/config --output-format json":         missingGuest("qemu-server/400.conf"),
			"pvesh get /nodes/pve/qemu/400/status/current --output-format json": missingGuest("qemu-server/400.conf"),
		},
	}
	_, err := Client{Runner: runner, Node: "pve"}.GuestAgent(context.Background(), 400)
	if !errors.Is(err, ErrNoAgent) {
		t.Fatalf("expected ErrNoAgent, got %v", err)
	}
}

func TestGuestAgentOfAMissingGuest(t *testing.T) {
	runner := &command.MockRunner{Errors: map[string]error{
		"pvesh get /nodes/pve/qemu/999/config --output-format json":         missingGuest("qemu-server/999.conf"),
		"pvesh get /nodes/pve/qemu/999/status/current --output-format json": missingGuest("qemu-server/999.conf"),
		"pvesh get /nodes/pve/lxc/999/status/current --output-format json":  missingGuest("lxc/999.conf"),
	}}
	_, err := Client{Runner: runner, Node: "pve"}.GuestAgent(context.Background(), 999)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected a not found error, got %v", err)
	}
}
