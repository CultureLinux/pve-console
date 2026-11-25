package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"pvc/internal/proxmox"
)

func TestDemoClientServesEveryRead(t *testing.T) {
	client := DemoClient()
	ctx := context.Background()

	node, err := client.NodeInfo(ctx)
	if err != nil || node.Name != "pve" || proxmoxVersion(node.ProxmoxVersion) != "9.0.3" || node.Kernel == "" {
		t.Fatalf("node=%+v err=%v", node, err)
	}
	guests, err := client.Guests(ctx)
	if err != nil || len(guests) != 7 {
		t.Fatalf("guests=%d err=%v", len(guests), err)
	}
	types := map[proxmox.GuestType]int{}
	for _, guest := range guests {
		types[guest.Type]++
	}
	if types[proxmox.GuestTypeQEMU] != 4 || types[proxmox.GuestTypeLXC] != 3 {
		t.Fatalf("types=%v", types)
	}

	storages, err := client.Storages(ctx)
	if err != nil || len(storages) != 3 || storages[2].Usage < 0.9 {
		t.Fatalf("storages=%+v err=%v", storages, err)
	}
	for _, vmid := range []int{100, 200} {
		if snapshots, err := client.Snapshots(ctx, vmid); err != nil || len(snapshots) != 3 {
			t.Fatalf("snapshots %d: %v %v", vmid, snapshots, err)
		}
		if config, err := client.GuestConfig(ctx, vmid); err != nil || len(config) < 5 {
			t.Fatalf("config %d: %v %v", vmid, config, err)
		}
		if guest, err := client.GuestStatus(ctx, vmid); err != nil || guest.VMID != vmid {
			t.Fatalf("status %d: %+v %v", vmid, guest, err)
		}
	}
	if jobs, err := client.BackupJobs(ctx); err != nil || len(jobs) != 2 {
		t.Fatalf("jobs=%v err=%v", jobs, err)
	}
	if volumes, err := client.BackupVolumes(ctx, storages); err != nil || len(volumes) != 3 {
		t.Fatalf("volumes=%v err=%v", volumes, err)
	}
	if users, err := client.Users(ctx); err != nil || len(users) != 3 {
		t.Fatalf("users=%v err=%v", users, err)
	}
	if entries, err := client.UserTFA(ctx, "root@pam"); err != nil || len(entries) != 1 {
		t.Fatalf("tfa=%v err=%v", entries, err)
	}
	if entries, err := client.UserTFA(ctx, "alice@pve"); err != nil || len(entries) != 0 {
		t.Fatalf("tfa=%v err=%v", entries, err)
	}
	if updates, err := client.PackageUpdates(ctx); err != nil || len(updates) != 3 {
		t.Fatalf("updates=%v err=%v", updates, err)
	}
}

func TestDemoActionsChangeTheNode(t *testing.T) {
	client := DemoClient()
	ctx := context.Background()
	status := func(vmid int) string {
		guest, err := client.GuestStatus(ctx, vmid)
		if err != nil {
			t.Fatalf("status %d: %v", vmid, err)
		}
		return guest.Status
	}

	if status(102) != "stopped" {
		t.Fatal("the demo starts with 102 stopped")
	}
	upid, err := client.StartGuest(ctx, 102)
	if err != nil || upid == "" {
		t.Fatalf("upid=%q err=%v", upid, err)
	}
	if status(102) != "running" {
		t.Fatal("start must change the status")
	}
	if _, err := client.ShutdownGuest(ctx, 102); err != nil || status(102) != "stopped" {
		t.Fatalf("shutdown: err=%v status=%s", err, status(102))
	}
	if _, err := client.StartGuest(ctx, 202); err != nil || status(202) != "running" {
		t.Fatalf("a container starts too: %v", err)
	}
	if _, err := client.RebootGuest(ctx, 100); err != nil {
		t.Fatalf("reboot: %v", err)
	}
	if _, err := client.StartGuest(ctx, 999); err == nil {
		t.Fatal("an unknown guest must fail")
	}
}

func TestDemoTasksRunForAFewPollsThenSucceed(t *testing.T) {
	client := DemoClient()
	ctx := context.Background()

	upid, err := client.RunBackup(ctx, 100, "backup-nas")
	if err != nil || upid == "" {
		t.Fatalf("upid=%q err=%v", upid, err)
	}
	polls := 0
	task, err := client.WaitTask(ctx, upid, time.Millisecond, func(proxmox.Task) { polls++ })
	if err != nil || !task.Succeeded() || polls != 3 {
		t.Fatalf("task=%+v err=%v polls=%d", task, err, polls)
	}

	first, _ := client.CreateSnapshot(ctx, 100, "s1", "", false)
	second, _ := client.DeleteSnapshot(ctx, 100, "s1")
	if first == "" || second == "" || first == second {
		t.Fatalf("every task has its own id: %q %q", first, second)
	}
}

func TestDemoRejectsWhatItDoesNotKnow(t *testing.T) {
	runner := newDemoRunner()
	if _, err := runner.Run(context.Background(), "rm", "-rf", "/"); err == nil {
		t.Fatal("the demo must refuse unknown programs")
	}
	if _, err := runner.Run(context.Background(), "pvesh", "get", "/nowhere"); err == nil {
		t.Fatal("the demo must refuse unknown paths")
	}
	if _, err := runner.Run(context.Background(), "pvesh"); err == nil {
		t.Fatal("pvesh without arguments is an error")
	}
}

func TestDemoModelRendersEverySection(t *testing.T) {
	m := NewWithStartup(DemoClient(), false)
	m.demo = true
	m.width, m.height = 120, 34

	m, _ = execute(t, m, m.load)
	m, _ = execute(t, m, m.loadUsers)
	m, _ = execute(t, m, m.loadUpdates)
	m, _ = execute(t, m, m.loadBackups)

	for _, tc := range []struct {
		view string
		want []string
	}{
		{viewGuests, []string{"wordpress", "postgres-primary", "nginx-proxy", "DEMO", "Proxmox VE 9.0.3", "4 running"}},
		{viewBackups, []string{"vzdump", "Volumes 3"}},
		{viewUsers, []string{"root@pam", "alice@pve"}},
		{viewUpdates, []string{"libssl3", "qemu-server"}},
	} {
		m.viewMode = tc.view
		out := stripANSI(m.View())
		for _, want := range tc.want {
			if tc.view == viewBackups && want == "vzdump" {
				want = "wordpress" // the volumes table shows the guest names
			}
			if !strings.Contains(out, want) {
				t.Errorf("%s view is missing %q:\n%s", tc.view, want, out)
			}
		}
		for i, line := range strings.Split(out, "\n") {
			if textWidth(line) > 120 {
				t.Fatalf("%s line %d is too wide", tc.view, i)
			}
		}
	}
}

func TestDemoBadgeOnlyInDemoMode(t *testing.T) {
	m := richModel()
	if strings.Contains(stripANSI(m.header(120)), "DEMO") {
		t.Fatal("a real session must not show the demo badge")
	}
	m.demo = true
	if !strings.Contains(stripANSI(m.header(120)), "DEMO") {
		t.Fatal("the demo badge is missing")
	}
}
