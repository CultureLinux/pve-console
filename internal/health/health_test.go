package health

import (
	"context"
	"testing"

	"pvc/internal/command"
	"pvc/internal/proxmox"
)

func TestRunReturnsWarnForStorageWarning(t *testing.T) {
	client := proxmox.Client{Runner: &command.MockRunner{
		Responses: map[string]command.Result{
			"pveversion": {Stdout: []byte("pve-manager/9.2.20\n")},
			"uname -r":   {Stdout: []byte("7.0.14-14-pve\n")},
			"df -P":      {Stdout: []byte("Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/root 100 70 30 70% /\n")},
			"pvesh get /nodes/pve/qemu --output-format json":    {Stdout: []byte(`[{"vmid":300,"name":"vm","status":"running"}]`)},
			"pvesh get /nodes/pve/lxc --output-format json":     {Stdout: []byte(`[]`)},
			"pvesh get /nodes/pve/storage --output-format json": {Stdout: []byte(`[{"storage":"local","type":"dir","used":85,"total":100,"active":true}]`)},
		},
	}, Node: "pve"}

	report, err := Run(context.Background(), client, DefaultOptions())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Status != StatusWarn {
		t.Fatalf("expected warn status, got %s", report.Status)
	}
	if ExitCode(report.Status) != 1 {
		t.Fatalf("expected exit code 1, got %d", ExitCode(report.Status))
	}
}

func TestExitCode(t *testing.T) {
	tests := []struct {
		status Status
		want   int
	}{
		{StatusOK, 0},
		{StatusInfo, 0},
		{StatusWarn, 1},
		{StatusFail, 2},
		{Status("wat"), 3},
	}

	for _, tt := range tests {
		if got := ExitCode(tt.status); got != tt.want {
			t.Fatalf("ExitCode(%q) = %d, want %d", tt.status, got, tt.want)
		}
	}
}
