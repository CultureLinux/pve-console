package cli

import (
	"bytes"
	"strings"
	"testing"

	"pvc/internal/command"
	"pvc/internal/proxmox"
)

func TestRunVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"version"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if got := strings.TrimSpace(stdout.String()); got != "pve-console dev" {
		t.Fatalf("expected version output %q, got %q", "pve-console dev", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestRunVersionWithDebug(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"--debug", "version"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if got := strings.TrimSpace(stdout.String()); got != "pve-console dev" {
		t.Fatalf("expected version output %q, got %q", "pve-console dev", got)
	}
	if !strings.Contains(stderr.String(), "debug: command=version") {
		t.Fatalf("expected debug output, got %q", stderr.String())
	}
}

func TestRunHelpPrintsUsage(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"help"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("expected usage in stdout, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestParseGlobalOptions(t *testing.T) {
	opts, rest, err := ParseGlobalOptions([]string{"--debug", "--json", "vm", "list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.Debug {
		t.Fatal("expected debug enabled")
	}
	if !opts.Verbose {
		t.Fatal("expected debug to enable verbose")
	}
	if !opts.JSON {
		t.Fatal("expected json enabled")
	}
	if len(rest) != 2 || rest[0] != "vm" || rest[1] != "list" {
		t.Fatalf("unexpected rest args: %#v", rest)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"wat"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unknown command: wat") {
		t.Fatalf("expected unknown command error, got %q", stderr.String())
	}
}

func TestRunVMList(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := RunWithClient([]string{"vm", "list"}, Options{}, testClient(), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "300") || !strings.Contains(stdout.String(), "qemu") {
		t.Fatalf("expected qemu guest in output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "400") || !strings.Contains(stdout.String(), "lxc") {
		t.Fatalf("expected lxc guest in output, got %q", stdout.String())
	}
}

func TestRunStorageListJSON(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := RunWithClient([]string{"storage", "list"}, Options{JSON: true}, testClient(), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"storage": "local"`) {
		t.Fatalf("expected storage JSON, got %q", stdout.String())
	}
}

func TestRunVMStatusRejectsInvalidVMID(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := RunWithClient([]string{"vm", "status", "abc"}, Options{}, testClient(), &stdout, &stderr)

	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "Invalid VMID") {
		t.Fatalf("expected VMID error, got %q", stderr.String())
	}
}

func TestRunCheckJSONReturnsWarnExitCode(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := RunWithClient([]string{"check"}, Options{JSON: true}, testClient(), &stdout, &stderr)

	if code != 1 {
		t.Fatalf("expected exit code 1 for warn, got %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"status": "warn"`) {
		t.Fatalf("expected warn JSON, got %q", stdout.String())
	}
}

func TestRunSnapshotList(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := RunWithClient([]string{"snapshot", "list", "300"}, Options{}, testClient(), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "before-upgrade") {
		t.Fatalf("expected snapshot in output, got %q", stdout.String())
	}
}

func TestRunSnapshotCreate(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := RunWithClient([]string{"snapshot", "create", "300", "before-upgrade", "--description", "Before upgrade"}, Options{}, testClient(), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `Snapshot "before-upgrade" created`) {
		t.Fatalf("expected creation message, got %q", stdout.String())
	}
}

func testClient() proxmox.Client {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"pveversion": {Stdout: []byte("pve-manager/9.2.20\n")},
			"uname -r":   {Stdout: []byte("7.0.14-14-pve\n")},
			"df -P":      {Stdout: []byte("Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/root 100 70 30 70% /\n")},
			"pvesh get /nodes/pve/qemu --output-format json":                                                 {Stdout: []byte(`[{"vmid":300,"name":"vm","status":"running"}]`)},
			"pvesh get /nodes/pve/lxc --output-format json":                                                  {Stdout: []byte(`[{"vmid":400,"name":"ct","status":"stopped"}]`)},
			"pvesh get /nodes/pve/qemu/300/status/current --output-format json":                              {Stdout: []byte(`{"name":"vm","status":"running"}`)},
			"pvesh get /nodes/pve/storage --output-format json":                                              {Stdout: []byte(`[{"storage":"local","type":"dir","used":85,"total":100,"enabled":true,"active":true}]`)},
			"pvesh get /nodes/pve/qemu/300/snapshot --output-format json":                                    {Stdout: []byte(`[{"name":"before-upgrade","description":"Before upgrade"}]`)},
			"pvesh create /nodes/pve/qemu/300/snapshot -snapname before-upgrade -description Before upgrade": {},
		},
	}

	return proxmox.Client{Runner: runner, Node: "pve"}
}
