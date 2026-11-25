package proxmox

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"pvc/internal/command"
)

const testUPID = "UPID:pve:0001A2B3:00C4D5E6:67890ABC:vzdump::root@pam:"

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

func TestGuestConfigFallsBackToLXC(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"pvesh get /nodes/pve/lxc/400/config --output-format json": {Stdout: []byte(`{"hostname":"ct","memory":1024,"net0":"name=eth0,bridge=vmbr0"}`)},
		},
		Errors: map[string]error{
			"pvesh get /nodes/pve/qemu/400/config --output-format json": command.CommandError{Result: command.Result{Command: "pvesh", ExitCode: 255}},
		},
	}
	client := Client{Runner: runner, Node: "pve"}

	config, err := client.GuestConfig(context.Background(), 400)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config["hostname"] != "ct" {
		t.Fatalf("unexpected config: %#v", config)
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

func TestStartStopGuestUsesPveshStatusActions(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"pvesh get /nodes/pve/qemu/300/status/current --output-format json": {Stdout: []byte(`{"name":"vm","status":"running"}`)},
			"pvesh create /nodes/pve/qemu/300/status/start":                     {},
			"pvesh create /nodes/pve/qemu/300/status/stop":                      {},
		},
	}
	client := Client{Runner: runner, Node: "pve"}

	if _, err := client.StartGuest(context.Background(), 300); err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}
	if _, err := client.StopGuest(context.Background(), 300); err != nil {
		t.Fatalf("unexpected stop error: %v", err)
	}
}
func TestSnapshotsFallsBackToLXC(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"pvesh get /nodes/pve/lxc/400/snapshot --output-format json": {Stdout: []byte(`[{"name":"before-upgrade","description":"Before upgrade"}]`)},
		},
		Errors: map[string]error{
			"pvesh get /nodes/pve/qemu/400/snapshot --output-format json": command.CommandError{Result: command.Result{Command: "pvesh", ExitCode: 255}},
		},
	}
	client := Client{Runner: runner, Node: "pve"}

	snapshots, err := client.Snapshots(context.Background(), 400)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].Name != "before-upgrade" {
		t.Fatalf("unexpected snapshots: %#v", snapshots)
	}
}

func TestCreateSnapshotUsesQEMUFirst(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"pvesh get /nodes/pve/qemu/300/status/current --output-format json":                      {Stdout: []byte(`{"name":"vm","status":"running"}`)},
			"pvesh create /nodes/pve/qemu/300/snapshot -snapname test -description hello -vmstate 1": {},
		},
	}
	client := Client{Runner: runner, Node: "pve"}

	_, err := client.CreateSnapshot(context.Background(), 300, "test", "hello", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runner.Calls) != 3 {
		t.Fatalf("expected the probe, the config read, then the snapshot, got %#v", runner.Calls)
	}
}

func TestParseAptUpdates(t *testing.T) {
	data := []byte("Listing...\nlibssl3/stable 3.0.17 amd64 [upgradable from: 3.0.16]\n")
	updates := parseAptUpdates(data)
	if len(updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(updates))
	}
	if updates[0].Name != "libssl3" || updates[0].Current != "3.0.16" || updates[0].Candidate != "3.0.17" {
		t.Fatalf("unexpected update: %#v", updates[0])
	}
}

func TestDeleteAndRollbackSnapshot(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"pvesh get /nodes/pve/qemu/300/status/current --output-format json": {Stdout: []byte(`{"name":"vm","status":"running"}`)},
			"pvesh delete /nodes/pve/qemu/300/snapshot/before-upgrade":          {},
			"pvesh create /nodes/pve/qemu/300/snapshot/before-upgrade/rollback": {},
		},
	}
	client := Client{Runner: runner, Node: "pve"}

	if _, err := client.DeleteSnapshot(context.Background(), 300, "before-upgrade"); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if _, err := client.RollbackSnapshot(context.Background(), 300, "before-upgrade"); err != nil {
		t.Fatalf("unexpected rollback error: %v", err)
	}
}

func TestDeleteAndRestoreBackupVolume(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"pvesh delete /nodes/pve/storage/backup-nas/content/backup-nas:backup/vzdump-qemu-300.vma.zst": {},
			"qmrestore backup-nas:backup/vzdump-qemu-300.vma.zst 300 --force":                              {},
		},
	}
	client := Client{Runner: runner, Node: "pve"}
	volume := BackupVolume{VolID: "backup-nas:backup/vzdump-qemu-300.vma.zst", VMID: 300, Format: "vma.zst"}

	if _, err := client.DeleteBackupVolume(context.Background(), volume.VolID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if err := client.RestoreBackupVolume(context.Background(), volume); err != nil {
		t.Fatalf("unexpected restore error: %v", err)
	}
}
func TestDistUpgradeReturnsOutput(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"env DEBIAN_FRONTEND=noninteractive apt-get -y -o Dpkg::Options::=--force-confold dist-upgrade": {Stdout: []byte("upgraded libssl\n"), Stderr: []byte("notice\n")},
		},
	}
	client := Client{Runner: runner, Node: "pve"}

	output, err := client.DistUpgrade(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(output, "upgraded libssl") || !strings.Contains(output, "notice") {
		t.Fatalf("unexpected output: %q", output)
	}
}
func TestBackupVolumes(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"pvesh get /nodes/pve/storage/backup-nas/content -content backup --output-format json": {Stdout: []byte(`[{"volid":"backup-nas:backup/vzdump-qemu-300.vma.zst","vmid":300,"format":"vma.zst","size":1234}]`)},
		},
	}
	client := Client{Runner: runner, Node: "pve"}

	volumes, err := client.BackupVolumes(context.Background(), []Storage{{Name: "backup-nas", Active: true}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(volumes) != 1 || volumes[0].VMID != 300 {
		t.Fatalf("unexpected volumes: %#v", volumes)
	}
}

func TestRunBackupStartsVzdump(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"pvesh create /nodes/pve/vzdump -vmid 300 -mode snapshot -storage backup-nas": {Stdout: []byte("\"" + testUPID + "\"\n")},
		},
	}
	client := Client{Runner: runner, Node: "pve"}

	upid, err := client.RunBackup(context.Background(), 300, "backup-nas")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if upid != testUPID {
		t.Fatalf("unexpected UPID %q", upid)
	}
	if len(runner.Calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", runner.Calls)
	}
}

func TestUpdateBackupJob(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"pvesh set /cluster/backup/daily -schedule 02:30 -storage backup-nas -vmid 300,302": {},
		},
	}
	client := Client{Runner: runner, Node: "pve"}

	err := client.UpdateBackupJob(context.Background(), "daily", "backup-nas", "02:30", "300,302")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUsers(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"pvesh get /access/users --output-format json": {Stdout: []byte(`[{"userid":"root@pam","enable":true}]`)},
		},
	}
	client := Client{Runner: runner, Node: "pve"}

	users, err := client.Users(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(users) != 1 || users[0].UserID != "root@pam" {
		t.Fatalf("unexpected users: %#v", users)
	}
}

func TestUserTFAParsesObjectResponse(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"pvesh get /access/users/root@pam/tfa --output-format json": {Stdout: []byte(`{"totp":{"type":"totp","description":"phone"},"webauthn":{"type":"webauthn","description":"key"}}`)},
		},
	}
	client := Client{Runner: runner, Node: "pve"}

	entries, err := client.UserTFA(context.Background(), "root@pam")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %#v", entries)
	}
	if entries[0].Type == "" || entries[1].Type == "" {
		t.Fatalf("expected entry types, got %#v", entries)
	}
}

func TestUserTFAParsesArrayResponse(t *testing.T) {
	entries, err := parseTFAEntries([]byte(`[{"id":"totp","type":"totp","description":"phone"}]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != "totp" {
		t.Fatalf("unexpected entries: %#v", entries)
	}
}

func TestDisableUserTFA(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"pveum user tfa delete root@pam": {},
		},
	}
	client := Client{Runner: runner, Node: "pve"}

	err := client.DisableUserTFA(context.Background(), "root@pam")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runner.Calls) != 1 || runner.Calls[0] != "pveum user tfa delete root@pam" {
		t.Fatalf("unexpected calls: %#v", runner.Calls)
	}
}

func TestRunBackupRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		vmid    int
		storage string
	}{
		{"zero vmid", 0, "backup-nas"},
		{"option-like storage", 300, "--delete"},
		{"storage with space", 300, "a b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &command.MockRunner{}
			client := Client{Runner: runner, Node: "pve"}
			if _, err := client.RunBackup(context.Background(), tt.vmid, tt.storage); err == nil {
				t.Fatal("expected error")
			}
			if len(runner.Calls) != 0 {
				t.Fatalf("no command expected, got %#v", runner.Calls)
			}
		})
	}
}

func TestUpdateBackupJobRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name, id, storage, schedule, vmids string
	}{
		{"option-like schedule", "daily", "", "--delete", ""},
		{"empty schedule", "daily", "", "", ""},
		{"bad vmids", "daily", "", "02:30", "300;rm"},
		{"bad storage", "daily", "-x", "02:30", ""},
		{"bad id", "../x", "", "02:30", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &command.MockRunner{}
			client := Client{Runner: runner, Node: "pve"}
			if err := client.UpdateBackupJob(context.Background(), tt.id, tt.storage, tt.schedule, tt.vmids); err == nil {
				t.Fatal("expected error")
			}
			if len(runner.Calls) != 0 {
				t.Fatalf("no command expected, got %#v", runner.Calls)
			}
		})
	}
}

func TestRestoreBackupVolumeRefusesRunningGuest(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"pvesh get /nodes/pve/qemu/300/status/current --output-format json": {Stdout: []byte(`{"name":"vm","status":"running"}`)},
		},
	}
	client := Client{Runner: runner, Node: "pve"}
	volume := BackupVolume{VolID: "backup-nas:backup/vzdump-qemu-300.vma.zst", VMID: 300, Format: "vma.zst"}

	err := client.RestoreBackupVolume(context.Background(), volume)
	if err == nil || !strings.Contains(err.Error(), "running") {
		t.Fatalf("expected running guest error, got %v", err)
	}
	for _, call := range runner.Calls {
		if strings.HasPrefix(call, "qmrestore") || strings.HasPrefix(call, "pct") {
			t.Fatalf("restore must not run, got call %q", call)
		}
	}
}

func TestRestoreBackupVolumeStoppedGuest(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"pvesh get /nodes/pve/qemu/300/status/current --output-format json": {Stdout: []byte(`{"name":"vm","status":"stopped"}`)},
			"qmrestore backup-nas:backup/vzdump-qemu-300.vma.zst 300 --force":   {},
		},
	}
	client := Client{Runner: runner, Node: "pve"}
	volume := BackupVolume{VolID: "backup-nas:backup/vzdump-qemu-300.vma.zst", VMID: 300, Format: "vma.zst"}

	if err := client.RestoreBackupVolume(context.Background(), volume); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsLXCBackup(t *testing.T) {
	tests := []struct {
		name   string
		volume BackupVolume
		want   bool
	}{
		{"vzdump lxc", BackupVolume{VolID: "s:backup/vzdump-lxc-400.tar.zst", Format: "tar.zst"}, true},
		{"vzdump qemu", BackupVolume{VolID: "s:backup/vzdump-qemu-300.vma.zst", Format: "vma.zst"}, false},
		{"pbs container", BackupVolume{VolID: "pbs:backup/ct/400/2026-01-01T00:00:00Z", Format: "pbs-ct"}, true},
		{"pbs container by volid", BackupVolume{VolID: "pbs:backup/ct/400/2026-01-01T00:00:00Z"}, true},
		{"pbs vm", BackupVolume{VolID: "pbs:backup/vm/300/2026-01-01T00:00:00Z", Format: "pbs-vm"}, false},
		{"subtype lxc wins", BackupVolume{VolID: "s:backup/custom.bin", Subtype: "lxc"}, true},
		{"subtype qemu wins", BackupVolume{VolID: "s:backup/vzdump-lxc-1.tar", Format: "tar", Subtype: "qemu"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isLXCBackup(tt.volume); got != tt.want {
				t.Fatalf("isLXCBackup() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestRestoreBackupVolumeLXCUsesPct(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"pct restore 400 pbs:backup/ct/400/2026-01-01T00:00:00Z --force 1": {},
		},
	}
	client := Client{Runner: runner, Node: "pve"}
	volume := BackupVolume{VolID: "pbs:backup/ct/400/2026-01-01T00:00:00Z", VMID: 400, Format: "pbs-ct"}

	if err := client.RestoreBackupVolume(context.Background(), volume); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientRejectsUnsafeIdentifiers(t *testing.T) {
	runner := &command.MockRunner{}
	client := Client{Runner: runner, Node: "pve"}
	ctx := context.Background()

	if _, err := client.DeleteBackupVolume(ctx, "local:backup/../../etc/passwd"); err == nil {
		t.Error("DeleteBackupVolume must reject path traversal")
	}
	if err := client.RestoreBackupVolume(ctx, BackupVolume{VolID: "local:backup/../x", VMID: 300}); err == nil {
		t.Error("RestoreBackupVolume must reject path traversal")
	}
	if _, err := client.UserTFA(ctx, "../x@pam"); err == nil {
		t.Error("UserTFA must reject an invalid user id")
	}
	if err := client.DisableUserTFA(ctx, "-x@pam"); err == nil {
		t.Error("DisableUserTFA must reject an invalid user id")
	}
	if len(runner.Calls) != 0 {
		t.Fatalf("no command must run, got %#v", runner.Calls)
	}
}

func TestAsyncActionsReturnUPID(t *testing.T) {
	tests := []struct {
		name string
		call string
		run  func(Client) (string, error)
	}{
		{"start", "pvesh create /nodes/pve/qemu/300/status/start", func(c Client) (string, error) { return c.StartGuest(context.Background(), 300) }},
		{"stop", "pvesh create /nodes/pve/qemu/300/status/stop", func(c Client) (string, error) { return c.StopGuest(context.Background(), 300) }},
		{"snapshot create", "pvesh create /nodes/pve/qemu/300/snapshot -snapname s1", func(c Client) (string, error) {
			return c.CreateSnapshot(context.Background(), 300, "s1", "", false)
		}},
		{"snapshot delete", "pvesh delete /nodes/pve/qemu/300/snapshot/s1", func(c Client) (string, error) { return c.DeleteSnapshot(context.Background(), 300, "s1") }},
		{"snapshot rollback", "pvesh create /nodes/pve/qemu/300/snapshot/s1/rollback", func(c Client) (string, error) { return c.RollbackSnapshot(context.Background(), 300, "s1") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &command.MockRunner{Responses: map[string]command.Result{
				"pvesh get /nodes/pve/qemu/300/status/current --output-format json": {Stdout: []byte(`{"name":"vm","status":"running"}`)},
				tt.call: {Stdout: []byte(`"` + testUPID + `"`)},
			}}
			upid, err := tt.run(Client{Runner: runner, Node: "pve"})
			if err != nil || upid != testUPID {
				t.Fatalf("upid=%q err=%v", upid, err)
			}
		})
	}
}

func TestSynchronousActionHasNoUPID(t *testing.T) {
	runner := &command.MockRunner{Responses: map[string]command.Result{
		"pvesh get /nodes/pve/qemu/300/status/current --output-format json": {Stdout: []byte(`{"name":"vm","status":"running"}`)},
		"pvesh create /nodes/pve/qemu/300/status/start":                     {Stdout: []byte("done\n")},
	}}
	upid, err := Client{Runner: runner, Node: "pve"}.StartGuest(context.Background(), 300)
	if err != nil || upid != "" {
		t.Fatalf("upid=%q err=%v", upid, err)
	}
}

func TestParseUPID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", testUPID + "\n", testUPID},
		{"quoted", `"` + testUPID + `"`, testUPID},
		{"empty", "", ""},
		{"text", "OK", ""},
		{"json", `{"upid":"x"}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseUPID([]byte(tt.in)); got != tt.want {
				t.Fatalf("parseUPID(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRebootAndShutdownUsePveshStatusActions(t *testing.T) {
	runner := &command.MockRunner{Responses: map[string]command.Result{
		"pvesh get /nodes/pve/qemu/300/status/current --output-format json": {Stdout: []byte(`{"name":"vm","status":"running"}`)},
		"pvesh create /nodes/pve/qemu/300/status/reboot":                    {Stdout: []byte(testUPID)},
		"pvesh create /nodes/pve/qemu/300/status/shutdown":                  {Stdout: []byte(testUPID)},
	}}
	client := Client{Runner: runner, Node: "pve"}

	if upid, err := client.RebootGuest(context.Background(), 300); err != nil || upid != testUPID {
		t.Fatalf("reboot: upid=%q err=%v", upid, err)
	}
	if upid, err := client.ShutdownGuest(context.Background(), 300); err != nil || upid != testUPID {
		t.Fatalf("shutdown: upid=%q err=%v", upid, err)
	}
}

func TestShutdownFallsBackToLXC(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"pvesh get /nodes/pve/lxc/400/status/current --output-format json": {Stdout: []byte(`{"name":"ct","status":"running"}`)},
			"pvesh create /nodes/pve/lxc/400/status/shutdown":                  {},
		},
		Errors: map[string]error{"pvesh get /nodes/pve/qemu/400/status/current --output-format json": command.CommandError{Result: command.Result{Command: "pvesh", ExitCode: 255}}},
	}
	if _, err := (Client{Runner: runner, Node: "pve"}).ShutdownGuest(context.Background(), 400); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runner.Calls) != 3 {
		t.Fatalf("calls=%v", runner.Calls)
	}
}

func TestRebootUnknownGuestFails(t *testing.T) {
	runner := &command.MockRunner{Errors: map[string]error{
		"pvesh get /nodes/pve/qemu/999/status/current --output-format json": missingGuest("qemu-server/999.conf"),
		"pvesh get /nodes/pve/lxc/999/status/current --output-format json":  missingGuest("lxc/999.conf"),
	}}
	if _, err := (Client{Runner: runner, Node: "pve"}).RebootGuest(context.Background(), 999); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected a not found error, got %v", err)
	}
}

// The API reports the memory in use as "mem": reading "memory" only would leave
// it at zero on a real node.
func TestGuestsReadTheMemoryInUse(t *testing.T) {
	runner := &command.MockRunner{Responses: map[string]command.Result{
		"pvesh get /nodes/pve/qemu --output-format json": {Stdout: []byte(`[{"vmid":300,"name":"vm","status":"running","cpu":0.25,"mem":1073741824,"maxmem":4294967296,"uptime":90}]`)},
		"pvesh get /nodes/pve/lxc --output-format json":  {Stdout: []byte(`[{"vmid":400,"name":"ct","status":"running","mem":209715200,"maxmem":536870912}]`)},
	}}
	guests, err := Client{Runner: runner, Node: "pve"}.Guests(context.Background())
	if err != nil || len(guests) != 2 {
		t.Fatalf("guests=%v err=%v", guests, err)
	}
	if guests[0].Memory != 1<<30 || guests[0].MaxMem != 4<<30 || guests[0].CPU != 0.25 || guests[0].Uptime != 90 {
		t.Fatalf("qemu guest: %+v", guests[0])
	}
	if guests[1].Memory != 200<<20 || guests[1].MaxMem != 512<<20 {
		t.Fatalf("lxc guest: %+v", guests[1])
	}
}

func TestGuestStatusReadsTheMemoryInUse(t *testing.T) {
	runner := &command.MockRunner{Responses: map[string]command.Result{
		"pvesh get /nodes/pve/qemu/300/status/current --output-format json": {Stdout: []byte(`{"name":"vm","status":"running","mem":2147483648,"maxmem":4294967296}`)},
	}}
	guest, err := Client{Runner: runner, Node: "pve"}.GuestStatus(context.Background(), 300)
	if err != nil || guest.Memory != 2<<30 || guest.MaxMem != 4<<30 {
		t.Fatalf("guest=%+v err=%v", guest, err)
	}
}

func TestGuestJSONRoundTrips(t *testing.T) {
	in := Guest{VMID: 300, Name: "vm", Type: GuestTypeQEMU, Status: "running", Memory: 1 << 30, MaxMem: 4 << 30}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"memory":1073741824`) {
		t.Fatalf("the --json output keeps the memory key: %s", data)
	}
	var out Guest
	if err := json.Unmarshal(data, &out); err != nil || out != in {
		t.Fatalf("out=%+v err=%v", out, err)
	}

	// "mem" wins when both are present; a broken document is an error, not a panic.
	var both Guest
	if err := json.Unmarshal([]byte(`{"memory":1,"mem":2}`), &both); err != nil || both.Memory != 2 {
		t.Fatalf("both=%+v err=%v", both, err)
	}
	if err := json.Unmarshal([]byte(`{"mem":"x"}`), &both); err == nil {
		t.Fatal("a non numeric memory must be reported")
	}
}

// A VM with a virtiofs share cannot be snapshotted with its RAM: the snapshot
// is made without it instead of failing.
func TestCreateSnapshotDropsVMStateWithVirtiofs(t *testing.T) {
	runner := &command.MockRunner{Responses: map[string]command.Result{
		"pvesh get /nodes/pve/qemu/202/status/current --output-format json": {Stdout: []byte(`{"name":"vm","status":"running"}`)},
		"pvesh get /nodes/pve/qemu/202/config --output-format json":         {Stdout: []byte(`{"memory":1024,"virtiofs0":"proxmox-virtio"}`)},
		"pvesh create /nodes/pve/qemu/202/snapshot -snapname test":          {},
	}}
	if _, err := (Client{Runner: runner, Node: "pve"}).CreateSnapshot(context.Background(), 202, "test", "", true); err != nil {
		t.Fatalf("unexpected error: %v (calls %v)", err, runner.Calls)
	}
}

const resourcesKey = "pvesh get /cluster/resources -type vm --output-format json"

// A fresh pvesh process answers cpu 0 in the lists: the live value comes from
// /cluster/resources, as in the web interface.
func TestGuestsTakeTheLiveCPU(t *testing.T) {
	runner := &command.MockRunner{Responses: map[string]command.Result{
		"pvesh get /nodes/pve/qemu --output-format json": {Stdout: []byte(`[{"vmid":300,"name":"vm","status":"running","cpu":0,"mem":1,"maxmem":2},{"vmid":301,"name":"idle","status":"stopped","cpu":0}]`)},
		"pvesh get /nodes/pve/lxc --output-format json":  {Stdout: []byte(`[{"vmid":400,"name":"ct","status":"running","cpu":0}]`)},
		resourcesKey: {Stdout: []byte(`[{"id":"qemu/300","type":"qemu","vmid":300,"cpu":0.4271},{"id":"lxc/400","type":"lxc","vmid":400,"cpu":0.05},{"id":"node/pve","type":"node","cpu":0.9},{"id":"qemu/999","vmid":999,"cpu":0.7}]`)},
	}}
	guests, err := Client{Runner: runner, Node: "pve"}.Guests(context.Background())
	if err != nil || len(guests) != 3 {
		t.Fatalf("guests=%v err=%v", guests, err)
	}
	if guests[0].CPU != 0.4271 || guests[2].CPU != 0.05 {
		t.Fatalf("running guests: %+v %+v", guests[0], guests[2])
	}
	// Not listed by pvestatd: left alone. The other fields are untouched.
	if guests[1].CPU != 0 || guests[0].Memory != 1 || guests[0].Name != "vm" {
		t.Fatalf("other fields changed: %+v %+v", guests[0], guests[1])
	}
	if len(guests) != 3 {
		t.Fatal("the resources of other nodes or kinds must not add guests")
	}
}

func TestGuestsKeepTheListWhenTheLiveCPUIsUnavailable(t *testing.T) {
	for name, runner := range map[string]*command.MockRunner{
		"refused":  {Responses: map[string]command.Result{}, Errors: map[string]error{resourcesKey: errors.New("permission denied")}},
		"bad json": {Responses: map[string]command.Result{resourcesKey: {Stdout: []byte("nope")}}},
	} {
		runner.Responses["pvesh get /nodes/pve/qemu --output-format json"] = command.Result{Stdout: []byte(`[{"vmid":300,"name":"vm","status":"running","cpu":0.25}]`)}
		runner.Responses["pvesh get /nodes/pve/lxc --output-format json"] = command.Result{Stdout: []byte(`[]`)}
		guests, err := Client{Runner: runner, Node: "pve"}.Guests(context.Background())
		if err != nil || len(guests) != 1 || guests[0].CPU != 0.25 {
			t.Errorf("%s: guests=%v err=%v", name, guests, err)
		}
	}
}

func TestGuestStatusTakesTheLiveCPU(t *testing.T) {
	runner := &command.MockRunner{Responses: map[string]command.Result{
		"pvesh get /nodes/pve/qemu/300/status/current --output-format json": {Stdout: []byte(`{"name":"vm","status":"running","cpu":0,"mem":2147483648,"maxmem":4294967296}`)},
		resourcesKey: {Stdout: []byte(`[{"type":"qemu","vmid":300,"cpu":0.31}]`)},
	}}
	guest, err := Client{Runner: runner, Node: "pve"}.GuestStatus(context.Background(), 300)
	if err != nil || guest.CPU != 0.31 || guest.VMID != 300 || guest.Memory != 2<<30 {
		t.Fatalf("guest=%+v err=%v", guest, err)
	}
	// An unknown guest is still an error, and asks for nothing more.
	missing := &command.MockRunner{Errors: map[string]error{}}
	if _, err := (Client{Runner: missing, Node: "pve"}).GuestStatus(context.Background(), 999); err == nil {
		t.Fatal("an unknown guest must fail")
	}
	for _, call := range missing.Calls {
		if call == resourcesKey {
			t.Fatal("no live CPU is read for a guest that does not exist")
		}
	}
}
