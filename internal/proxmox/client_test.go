package proxmox

import (
	"context"
	"testing"

	"pvc/internal/command"
)

func TestGuestsCombinesQEMUAndLXC(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"pvesh get /nodes/pve/qemu --output-format json": {Stdout: []byte(`[{"vmid":300,"name":"vm","status":"running"}]`)},
			"pvesh get /nodes/pve/lxc --output-format json":  {Stdout: []byte(`[{"vmid":400,"name":"ct","status":"stopped"}]`)},
		},
	}
	client := Client{Runner: runner, Node: "pve"}

	guests, err := client.Guests(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(guests) != 2 {
		t.Fatalf("expected 2 guests, got %d", len(guests))
	}
	if guests[0].Type != GuestTypeQEMU || guests[1].Type != GuestTypeLXC {
		t.Fatalf("unexpected guest types: %#v", guests)
	}
}

func TestGuestStatusFallsBackToLXC(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"pvesh get /nodes/pve/lxc/400/status/current --output-format json": {Stdout: []byte(`{"name":"ct","status":"running"}`)},
		},
		Errors: map[string]error{
			"pvesh get /nodes/pve/qemu/400/status/current --output-format json": command.CommandError{Result: command.Result{Command: "pvesh", ExitCode: 255}},
		},
	}
	client := Client{Runner: runner, Node: "pve"}

	guest, err := client.GuestStatus(context.Background(), 400)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if guest.VMID != 400 || guest.Type != GuestTypeLXC || guest.Name != "ct" {
		t.Fatalf("unexpected guest: %#v", guest)
	}
}

func TestStoragesComputesUsage(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"pvesh get /nodes/pve/storage --output-format json": {Stdout: []byte(`[{"storage":"local","type":"dir","used":25,"total":100,"enabled":true,"active":true}]`)},
		},
	}
	client := Client{Runner: runner, Node: "pve"}

	storages, err := client.Storages(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(storages) != 1 {
		t.Fatalf("expected 1 storage, got %d", len(storages))
	}
	if storages[0].Name != "local" || storages[0].Usage != 0.25 {
		t.Fatalf("unexpected storage: %#v", storages[0])
	}
}
