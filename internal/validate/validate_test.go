package validate

import (
	"strings"
	"testing"
)

func TestVMID(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    int
		wantErr bool
	}{
		{name: "valid", raw: "300", want: 300},
		{name: "empty", raw: "", wantErr: true},
		{name: "zero", raw: "0", wantErr: true},
		{name: "negative", raw: "-1", wantErr: true},
		{name: "text", raw: "abc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := VMID(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, got)
			}
		})
	}
}

func TestSnapshotName(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "valid dash", value: "before-upgrade"},
		{name: "valid generated", value: "manual-20260925-123500"},
		{name: "empty", value: "", wantErr: true},
		{name: "space", value: "before upgrade", wantErr: true},
		{name: "slash", value: "../backup", wantErr: true},
		{name: "starts with dot", value: ".hidden", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SnapshotName(tt.value)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestStorageName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		ok    bool
	}{
		{"simple", "backup-nas", true},
		{"dots", "local.zfs_1", true},
		{"empty", "", false},
		{"option", "--delete", false},
		{"space", "a b", false},
		{"slash", "a/b", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := StorageName(tt.input); (err == nil) != tt.ok {
				t.Fatalf("StorageName(%q) error = %v, want ok=%t", tt.input, err, tt.ok)
			}
		})
	}
}

func TestVMIDList(t *testing.T) {
	tests := []struct {
		name  string
		input string
		ok    bool
	}{
		{"single", "300", true},
		{"list", "300,302", true},
		{"empty", "", false},
		{"zero", "0", false},
		{"leading comma", ",300", false},
		{"trailing comma", "300,", false},
		{"injection", "300;rm", false},
		{"space", "300, 302", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := VMIDList(tt.input); (err == nil) != tt.ok {
				t.Fatalf("VMIDList(%q) error = %v, want ok=%t", tt.input, err, tt.ok)
			}
		})
	}
}

func TestSchedule(t *testing.T) {
	tests := []struct {
		name  string
		input string
		ok    bool
	}{
		{"time", "02:30", true},
		{"keyword", "daily", true},
		{"calendar", "mon..fri 01:00", true},
		{"step", "*/15", true},
		{"empty", "", false},
		{"option", "--delete", false},
		{"dash start", "-1", false},
		{"semicolon", "02:30;x", false},
		{"too long", strings.Repeat("a", 101), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Schedule(tt.input); (err == nil) != tt.ok {
				t.Fatalf("Schedule(%q) error = %v, want ok=%t", tt.input, err, tt.ok)
			}
		})
	}
}

func TestVolID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		ok    bool
	}{
		{"vma", "backup-nas:backup/vzdump-qemu-300.vma.zst", true},
		{"lxc", "local:backup/vzdump-lxc-400-2026_01_01-00_00_00.tar.zst", true},
		{"pbs", "pbs:backup/ct/400/2026-01-01T00:00:00Z", true},
		{"empty", "", false},
		{"no storage", "backup/vzdump.vma", false},
		{"traversal", "local:backup/../../etc/passwd", false},
		{"double slash", "local:backup//x", false},
		{"space", "local:backup/a b", false},
		{"option", "--delete:x", false},
		{"semicolon", "local:backup/a;b", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := VolID(tt.input); (err == nil) != tt.ok {
				t.Fatalf("VolID(%q) error = %v, want ok=%t", tt.input, err, tt.ok)
			}
		})
	}
}

func TestUserID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		ok    bool
	}{
		{"pam", "root@pam", true},
		{"pve", "bob.smith@pve", true},
		{"dash inside", "svc-backup@pve", true},
		{"no realm", "root", false},
		{"empty", "", false},
		{"option", "-x@pam", false},
		{"traversal", "../x@pam", false},
		{"slash", "a/b@pam", false},
		{"space", "a b@pam", false},
		{"two at", "a@b@pam", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := UserID(tt.input); (err == nil) != tt.ok {
				t.Fatalf("UserID(%q) error = %v, want ok=%t", tt.input, err, tt.ok)
			}
		})
	}
}

func TestUPID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		ok    bool
	}{
		{"vzdump", "UPID:pve:0001A2B3:00C4D5E6:67890ABC:vzdump::root@pam:", true},
		{"qmstart", "UPID:pve:0001A2B3:00C4D5E6:67890ABC:qmstart:300:root@pam:", true},
		{"token user", "UPID:pve-1.lan:0001A2B3:00C4D5E6:67890ABC:qmsnapshot:300:root@pam!tok:", true},
		{"empty", "", false},
		{"no trailing colon", "UPID:pve:0001A2B3:00C4D5E6:67890ABC:vzdump::root@pam", false},
		{"bad hex", "UPID:pve:ZZZZZZZZ:00C4D5E6:67890ABC:vzdump::root@pam:", false},
		{"traversal", "UPID:pve:0001A2B3:00C4D5E6:67890ABC:qmstart:../../x:root@pam:", false},
		{"space", "UPID:pve:0001A2B3:00C4D5E6:67890ABC:vzdump:a b:root@pam:", false},
		{"other text", "OK", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := UPID(tt.input); (err == nil) != tt.ok {
				t.Fatalf("UPID(%q) error = %v, want ok=%t", tt.input, err, tt.ok)
			}
		})
	}
}

func TestTokenID(t *testing.T) {
	tests := []struct {
		id      string
		wantErr bool
	}{
		{id: "grafana"},
		{id: "opentofu-ci_1.a"},
		{id: "ab"},
		{id: "", wantErr: true},
		{id: "a", wantErr: true},
		{id: "1abc", wantErr: true},
		{id: "-abc", wantErr: true},
		{id: "a b", wantErr: true},
		{id: "a/b", wantErr: true},
		{id: "a!b", wantErr: true},
		{id: "a=b", wantErr: true},
		{id: "../x", wantErr: true},
		{id: strings.Repeat("a", 65), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			err := TokenID(tt.id)
			if (err != nil) != tt.wantErr {
				t.Fatalf("TokenID(%q) = %v, wantErr %v", tt.id, err, tt.wantErr)
			}
		})
	}
}
