package proxmox

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"pvc/internal/command"
)

// missingGuest is what Proxmox says when the guest is not of that type.
func missingGuest(path string) error {
	return failure(255, "Configuration file 'nodes/pve/"+path+"' does not exist")
}

// failure is a command that exited with stderr text.
func failure(code int, stderr string) error {
	return command.CommandError{
		Result: command.Result{Command: "pvesh", ExitCode: code, Stderr: []byte(stderr)},
		Err:    errors.New("exit status " + fmt.Sprint(code)),
	}
}

func TestIsRealError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"does not exist", missingGuest("qemu-server/1.conf"), false},
		{"not found", failure(2, "VM 5 not found"), false},
		{"no such", failure(2, "No such file or directory"), false},
		{"silent failure", failure(255, ""), false},
		{"refused", failure(255, "snapshot name 'x' already exists"), true},
		{"locked", failure(255, "VM is locked (backup)"), true},
		{"timeout", command.CommandError{Err: context.DeadlineExceeded}, true},
		{"canceled", context.Canceled, true},
		{"program missing", command.CommandError{Err: &exec.Error{Name: "pvesh", Err: exec.ErrNotFound}}, true},
		{"unreadable answer", errors.New("invalid character"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRealError(tt.err); got != tt.want {
				t.Fatalf("isRealError(%v) = %t, want %t", tt.err, got, tt.want)
			}
		})
	}
}

func TestByGuestType(t *testing.T) {
	notFound := errors.New("not found")
	missing := missingGuest("x.conf")
	refused := failure(255, "snapshot name 'x' already exists")
	locked := failure(255, "VM is locked (backup)")

	tests := []struct {
		name    string
		qemu    error
		lxc     error
		want    string // text the error must contain; "" means success
		exclude string
	}{
		{"qemu works", nil, missing, "", ""},
		{"lxc works", missing, nil, "", ""},
		{"the VM refuses, the container does not exist", refused, missing, "already exists", "not found"},
		{"the container refuses, the VM does not exist", missing, refused, "already exists", "not found"},
		{"neither exists", missing, missing, "not found", "does not exist"},
		{"neither exists, silently", failure(255, ""), failure(255, ""), "not found", ""},
		{"both fail the same way", locked, locked, "locked", ""},
		{"both fail differently", refused, locked, "already exists", ""},
		{"a real error on qemu is not hidden even when lxc works", refused, nil, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := byGuestType(notFound, func(typ GuestType) (string, error) {
				if typ == GuestTypeQEMU {
					return "qemu", tt.qemu
				}
				return "lxc", tt.lxc
			})
			if tt.want == "" {
				if err != nil || value == "" {
					t.Fatalf("value=%q err=%v", value, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err=%v, want it to contain %q", err, tt.want)
			}
			if tt.exclude != "" && strings.Contains(err.Error(), tt.exclude) {
				t.Fatalf("err=%v must not contain %q", err, tt.exclude)
			}
		})
	}

	// Both real errors with different texts are both kept.
	_, err := byGuestType(notFound, func(typ GuestType) (string, error) {
		if typ == GuestTypeQEMU {
			return "", refused
		}
		return "", locked
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("both messages are needed: %v", err)
	}
}

// The reported bug: a snapshot refused by Proxmox on a VM was first shown as
// "guest N was not found", then (after the fallback) as a doomed LXC task
// ("Configuration file 'nodes/x/lxc/N.conf' does not exist"). The type is now
// probed first, so the snapshot is only tried on the real type and its error is
// shown as is.
func TestCreateSnapshotShowsWhatProxmoxSaid(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"pvesh get /nodes/pve/qemu/300/status/current --output-format json": {Stdout: []byte(`{"name":"vm","status":"running"}`)},
		},
		Errors: map[string]error{
			"pvesh create /nodes/pve/qemu/300/snapshot -snapname t1": failure(255, "400 Parameter verification failed.\nsnapname: invalid format"),
		},
	}
	_, err := Client{Runner: runner, Node: "pve"}.CreateSnapshot(context.Background(), 300, "t1", "", false)
	if err == nil || !strings.Contains(err.Error(), "Parameter verification failed") {
		t.Fatalf("the Proxmox error must be reported, got %v", err)
	}
	for _, call := range runner.Calls {
		if strings.Contains(call, "/lxc/") {
			t.Fatalf("no LXC action must be tried on a QEMU VM: %v", runner.Calls)
		}
	}
	var commandErr command.CommandError
	if !errors.As(err, &commandErr) || !strings.Contains(string(commandErr.Result.Stderr), "snapname") {
		t.Fatalf("the command error must stay reachable for the details, got %T", err)
	}
}

func TestEveryGuestTypeFallbackReportsTheRealError(t *testing.T) {
	refused := failure(255, "VM is locked (backup)")
	ctx := context.Background()
	client := func(qemu, lxc string) Client {
		return Client{Node: "pve", Runner: &command.MockRunner{
			Responses: map[string]command.Result{},
			Errors:    map[string]error{qemu: refused, lxc: missingGuest("lxc/300.conf")},
		}}
	}

	tests := map[string]func(Client) error{
		"status":         func(c Client) error { _, err := c.GuestStatus(ctx, 300); return err },
		"config":         func(c Client) error { _, err := c.GuestConfig(ctx, 300); return err },
		"list snapshots": func(c Client) error { _, err := c.Snapshots(ctx, 300); return err },
		"start":          func(c Client) error { _, err := c.StartGuest(ctx, 300); return err },
		"delete snapshot": func(c Client) error {
			_, err := c.DeleteSnapshot(ctx, 300, "s1")
			return err
		},
		"rollback": func(c Client) error { _, err := c.RollbackSnapshot(ctx, 300, "s1"); return err },
	}
	paths := map[string][2]string{
		"status":          {"pvesh get /nodes/pve/qemu/300/status/current --output-format json", "pvesh get /nodes/pve/lxc/300/status/current --output-format json"},
		"config":          {"pvesh get /nodes/pve/qemu/300/config --output-format json", "pvesh get /nodes/pve/lxc/300/config --output-format json"},
		"list snapshots":  {"pvesh get /nodes/pve/qemu/300/snapshot --output-format json", "pvesh get /nodes/pve/lxc/300/snapshot --output-format json"},
		"start":           {"pvesh get /nodes/pve/qemu/300/status/current --output-format json", "pvesh get /nodes/pve/lxc/300/status/current --output-format json"},
		"delete snapshot": {"pvesh get /nodes/pve/qemu/300/status/current --output-format json", "pvesh get /nodes/pve/lxc/300/status/current --output-format json"},
		"rollback":        {"pvesh get /nodes/pve/qemu/300/status/current --output-format json", "pvesh get /nodes/pve/lxc/300/status/current --output-format json"},
	}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			err := run(client(paths[name][0], paths[name][1]))
			if err == nil || !strings.Contains(err.Error(), "locked") || strings.Contains(err.Error(), "not found") {
				t.Fatalf("got %v", err)
			}
		})
	}
}
