package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pvc/internal/command"
	"pvc/internal/proxmox"
)

const (
	testUPID = "UPID:pve:0001A2B3:00C4D5E6:67890ABC:vzdump::root@pam:"
	statusOf = "pvesh get /nodes/pve/tasks/" + testUPID + "/status --output-format json"
	volQEMU  = "backup-nas:backup/vzdump-qemu-300.vma.zst"
	upgrade  = "env DEBIAN_FRONTEND=noninteractive apt-get -y -o Dpkg::Options::=--force-confold dist-upgrade"
)

// exec runs the CLI with a mock runner holding the given canned responses.
func exec(t *testing.T, responses map[string]command.Result, errs map[string]error, args ...string) (int, string, string, *command.MockRunner) {
	t.Helper()
	// Every guest action starts with a probe of the guest type: 300 is a QEMU VM.
	const probe = "pvesh get /nodes/pve/qemu/300/status/current --output-format json"
	if _, found := responses[probe]; !found {
		withProbe := map[string]command.Result{probe: ok(`{"name":"vm","status":"running"}`)}
		for k, v := range responses {
			withProbe[k] = v
		}
		responses = withProbe
	}
	runner := &command.MockRunner{Responses: responses, Errors: errs}
	client := proxmox.Client{Runner: runner, Node: "pve"}
	opts, rest, err := ParseGlobalOptions(args)
	if err != nil {
		t.Fatalf("ParseGlobalOptions: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := RunWithClient(rest, opts, client, &stdout, &stderr)
	return code, stdout.String(), stderr.String(), runner
}

// actionCalls returns the commands the runner saw, without the guest type probe.
func actionCalls(runner *command.MockRunner) []string {
	var calls []string
	for _, call := range runner.Calls {
		if !strings.HasSuffix(call, "/status/current --output-format json") {
			calls = append(calls, call)
		}
	}
	return calls
}

func ok(stdout string) command.Result { return command.Result{Stdout: []byte(stdout)} }

func TestParseGlobalOptionsYes(t *testing.T) {
	for _, args := range [][]string{{"--yes", "vm", "stop", "1"}, {"vm", "stop", "1", "-y"}} {
		opts, rest, err := ParseGlobalOptions(args)
		if err != nil || !opts.Yes || len(rest) != 3 {
			t.Fatalf("args=%v opts=%+v rest=%v err=%v", args, opts, rest, err)
		}
	}
	if opts, _, _ := ParseGlobalOptions([]string{"vm", "list"}); opts.Yes {
		t.Fatal("--yes must be off by default")
	}
}

func TestCommandTimeout(t *testing.T) {
	tests := []struct {
		args []string
		want time.Duration
	}{
		{[]string{"status"}, 30 * time.Second},
		{[]string{"vm", "list"}, 30 * time.Second},
		{[]string{"snapshot", "create", "300"}, 5 * time.Minute},
		{[]string{"snapshot", "rollback", "300", "a"}, 30 * time.Minute},
		{[]string{"backup", "restore", "x"}, 30 * time.Minute},
		{[]string{"updates", "upgrade"}, 30 * time.Minute},
		{[]string{"vm", "stop", "300"}, 2 * time.Minute},
		{[]string{"backup", "delete", "x"}, 2 * time.Minute},
	}
	for _, tt := range tests {
		if got := commandTimeout(tt.args); got != tt.want {
			t.Errorf("commandTimeout(%v) = %s, want %s", tt.args, got, tt.want)
		}
	}
}

func TestTakeOption(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		value   string
		found   bool
		rest    int
		wantErr bool
	}{
		{"separate", []string{"300", "--storage", "nas"}, "nas", true, 1, false},
		{"equals", []string{"--storage=nas", "300"}, "nas", true, 1, false},
		{"absent", []string{"300"}, "", false, 1, false},
		{"missing value", []string{"300", "--storage"}, "", false, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, found, rest, err := takeOption(tt.args, "--storage")
			if (err != nil) != tt.wantErr || value != tt.value || found != tt.found || (!tt.wantErr && len(rest) != tt.rest) {
				t.Fatalf("value=%q found=%t rest=%v err=%v", value, found, rest, err)
			}
		})
	}
}

func TestVMStartDoesNotNeedYes(t *testing.T) {
	code, stdout, _, runner := exec(t,
		map[string]command.Result{"pvesh create /nodes/pve/qemu/300/status/start": {}}, nil,
		"vm", "start", "300")
	if code != 0 || !strings.Contains(stdout, "Guest 300: start requested") {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	if len(actionCalls(runner)) != 1 {
		t.Fatalf("calls=%v", runner.Calls)
	}
}

func TestVMStopRequiresYes(t *testing.T) {
	code, _, stderr, runner := exec(t, nil, nil, "vm", "stop", "300")
	if code != 2 || !strings.Contains(stderr, "without --yes") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if len(runner.Calls) != 0 {
		t.Fatalf("nothing must run without --yes, got %v", runner.Calls)
	}

	code, stdout, _, runner := exec(t,
		map[string]command.Result{"pvesh create /nodes/pve/qemu/300/status/stop": {}}, nil,
		"--yes", "--json", "vm", "stop", "300")
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil || payload["action"] != "stop" || payload["vmid"] != float64(300) {
		t.Fatalf("payload=%v err=%v", payload, err)
	}
	if len(actionCalls(runner)) != 1 {
		t.Fatalf("calls=%v", runner.Calls)
	}
}

func TestVMPowerErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		code int
	}{
		{"invalid vmid", []string{"vm", "start", "abc"}, 1},
		{"missing vmid", []string{"vm", "start"}, 2},
		{"extra arg", []string{"vm", "start", "300", "301"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, _, runner := exec(t, nil, nil, tt.args...)
			if code != tt.code || len(runner.Calls) != 0 {
				t.Fatalf("code=%d calls=%v", code, runner.Calls)
			}
		})
	}

	// Neither a QEMU VM nor a container: the client reports it, exit code 1.
	code, _, stderr, _ := exec(t, nil, nil, "vm", "start", "999")
	if code != 1 || !strings.Contains(stderr, "Unable to start guest 999") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestSnapshotDeleteAndRollback(t *testing.T) {
	tests := []struct {
		action string
		call   string
		out    string
	}{
		{"delete", "pvesh delete /nodes/pve/qemu/300/snapshot/snap1", `Snapshot "snap1" deleted for guest 300`},
		{"rollback", "pvesh create /nodes/pve/qemu/300/snapshot/snap1/rollback", `Guest 300 rolled back to snapshot "snap1"`},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			code, _, stderr, runner := exec(t, nil, nil, "snapshot", tt.action, "300", "snap1")
			if code != 2 || !strings.Contains(stderr, "without --yes") || len(runner.Calls) != 0 {
				t.Fatalf("code=%d stderr=%q calls=%v", code, stderr, runner.Calls)
			}

			code, stdout, stderr, runner := exec(t, map[string]command.Result{tt.call: {}}, nil, "snapshot", tt.action, "300", "snap1", "--yes")
			if code != 0 || !strings.Contains(stdout, tt.out) {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			if len(actionCalls(runner)) != 1 || actionCalls(runner)[0] != tt.call {
				t.Fatalf("calls=%v", runner.Calls)
			}
		})
	}
}

func TestSnapshotChangeRejectsInvalidInput(t *testing.T) {
	tests := [][]string{
		{"snapshot", "delete", "abc", "snap1", "--yes"},
		{"snapshot", "delete", "300", "bad name", "--yes"},
		{"snapshot", "rollback", "300", "--yes"},
		{"snapshot", "delete", "300", "../x", "--yes"},
	}
	for _, args := range tests {
		code, _, _, runner := exec(t, nil, nil, args...)
		if code == 0 || len(runner.Calls) != 0 {
			t.Fatalf("args=%v code=%d calls=%v", args, code, runner.Calls)
		}
	}
}

func backupResponses() map[string]command.Result {
	return map[string]command.Result{
		"pvesh get /nodes/pve/storage --output-format json":                                    ok(`[{"storage":"backup-nas","type":"nfs","used":1,"total":10,"enabled":true,"active":true}]`),
		"pvesh get /nodes/pve/storage/backup-nas/content -content backup --output-format json": ok(`[{"volid":"` + volQEMU + `","vmid":300,"format":"vma.zst","size":1234,"ctime":1700000000}]`),
		"pvesh get /cluster/backup --output-format json":                                       ok(`[{"id":"daily","storage":"backup-nas","schedule":"02:30","enabled":true,"vmid":"300,301","mode":"snapshot"}]`),
	}
}

func TestBackupListAndJobs(t *testing.T) {
	code, stdout, stderr, _ := exec(t, backupResponses(), nil, "backup", "list")
	if code != 0 || !strings.Contains(stdout, volQEMU) || !strings.Contains(stdout, "2023-11-14") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	code, stdout, _, _ = exec(t, backupResponses(), nil, "--json", "backup", "list")
	var volumes []proxmox.BackupVolume
	if err := json.Unmarshal([]byte(stdout), &volumes); err != nil || len(volumes) != 1 || volumes[0].VMID != 300 {
		t.Fatalf("volumes=%v err=%v", volumes, err)
	}

	code, stdout, stderr, _ = exec(t, backupResponses(), nil, "backup", "jobs")
	if code != 0 || !strings.Contains(stdout, "daily") || !strings.Contains(stdout, "02:30") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, _, _ = exec(t, backupResponses(), nil, "--json", "backup", "jobs")
	var jobs []proxmox.BackupJob
	if err := json.Unmarshal([]byte(stdout), &jobs); err != nil || len(jobs) != 1 || jobs[0].ID != "daily" {
		t.Fatalf("jobs=%v err=%v", jobs, err)
	}
}

func TestBackupRun(t *testing.T) {
	call := "pvesh create /nodes/pve/vzdump -vmid 300 -mode snapshot -storage backup-nas"
	for _, args := range [][]string{
		{"backup", "run", "300", "--storage", "backup-nas"},
		{"backup", "run", "--storage=backup-nas", "300"},
	} {
		code, stdout, stderr, runner := exec(t, map[string]command.Result{call: ok(`"` + testUPID + `"`)}, nil, args...)
		if code != 0 || !strings.Contains(stdout, "Task: "+testUPID) || !strings.Contains(stdout, "background") {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", args, code, stdout, stderr)
		}
		if len(runner.Calls) != 1 || runner.Calls[0] != call {
			t.Fatalf("calls=%v", runner.Calls)
		}
	}

	code, stdout, _, _ := exec(t, map[string]command.Result{call: ok(`"` + testUPID + `"`)}, nil, "--json", "backup", "run", "300", "--storage", "backup-nas")
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); code != 0 || err != nil || payload["upid"] != testUPID {
		t.Fatalf("payload=%v err=%v", payload, err)
	}
}

func TestBackupRunRejectsInvalidInput(t *testing.T) {
	tests := [][]string{
		{"backup", "run"},
		{"backup", "run", "abc"},
		{"backup", "run", "300", "--storage", "--delete"},
		{"backup", "run", "300", "--storage"},
	}
	for _, args := range tests {
		code, _, _, runner := exec(t, nil, nil, args...)
		if code == 0 || len(runner.Calls) != 0 {
			t.Fatalf("args=%v code=%d calls=%v", args, code, runner.Calls)
		}
	}
}

func TestBackupDelete(t *testing.T) {
	code, _, stderr, runner := exec(t, nil, nil, "backup", "delete", volQEMU)
	if code != 2 || !strings.Contains(stderr, "without --yes") || len(runner.Calls) != 0 {
		t.Fatalf("code=%d stderr=%q calls=%v", code, stderr, runner.Calls)
	}

	call := "pvesh delete /nodes/pve/storage/backup-nas/content/" + volQEMU
	code, stdout, stderr, runner := exec(t, map[string]command.Result{call: {}}, nil, "backup", "delete", volQEMU, "--yes")
	if code != 0 || !strings.Contains(stdout, "deleted") || len(runner.Calls) != 1 {
		t.Fatalf("code=%d stdout=%q stderr=%q calls=%v", code, stdout, stderr, runner.Calls)
	}
}

func TestBackupVolumeRejectsPathTraversal(t *testing.T) {
	for _, volid := range []string{"local:backup/../../etc/passwd", "no-storage", "--yes:x", "a:b c"} {
		code, _, _, runner := exec(t, nil, nil, "backup", "delete", volid, "--yes")
		if code == 0 || len(runner.Calls) != 0 {
			t.Fatalf("volid=%q code=%d calls=%v", volid, code, runner.Calls)
		}
	}
}

func TestBackupRestore(t *testing.T) {
	code, _, stderr, runner := exec(t, backupResponses(), nil, "backup", "restore", volQEMU)
	if code != 2 || len(runner.Calls) != 0 {
		t.Fatalf("code=%d stderr=%q calls=%v", code, stderr, runner.Calls)
	}

	responses := backupResponses()
	responses["pvesh get /nodes/pve/qemu/300/status/current --output-format json"] = ok(`{"status":"stopped"}`)
	responses["qmrestore "+volQEMU+" 300 --force"] = command.Result{}
	code, stdout, stderr, runner := exec(t, responses, nil, "backup", "restore", volQEMU, "--yes")
	if code != 0 || !strings.Contains(stdout, "restored to guest 300") {
		t.Fatalf("code=%d stdout=%q stderr=%q calls=%v", code, stdout, stderr, runner.Calls)
	}
	if last := runner.Calls[len(runner.Calls)-1]; last != "qmrestore "+volQEMU+" 300 --force" {
		t.Fatalf("calls=%v", runner.Calls)
	}
}

func TestBackupRestoreRefusesRunningGuestAndUnknownBackup(t *testing.T) {
	responses := backupResponses()
	responses["pvesh get /nodes/pve/qemu/300/status/current --output-format json"] = ok(`{"status":"running"}`)
	code, _, stderr, runner := exec(t, responses, nil, "backup", "restore", volQEMU, "--yes")
	if code != 1 || !strings.Contains(stderr, "running") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	for _, call := range runner.Calls {
		if strings.HasPrefix(call, "qmrestore") {
			t.Fatalf("restore must not run: %v", runner.Calls)
		}
	}

	code, _, stderr, _ = exec(t, backupResponses(), nil, "backup", "restore", "backup-nas:backup/unknown.vma", "--yes")
	if code != 1 || !strings.Contains(stderr, "not found") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestBackupUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"backup"}, {"backup", "bogus"}, {"backup", "list", "extra"}, {"backup", "jobs", "extra"}, {"backup", "delete"}} {
		code, _, stderr, _ := exec(t, nil, nil, args...)
		if code != 2 || !strings.Contains(stderr, "Usage:") {
			t.Fatalf("args=%v code=%d stderr=%q", args, code, stderr)
		}
	}
}

func TestUpdatesListAndUpgrade(t *testing.T) {
	list := ok("Listing...\nlibssl3/stable 3.0.2 amd64 [upgradable from: 3.0.1]\n")
	code, stdout, stderr, _ := exec(t, map[string]command.Result{"apt list --upgradable": list}, nil, "updates", "list")
	if code != 0 || !strings.Contains(stdout, "libssl3") || !strings.Contains(stdout, "3.0.2") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, _, _ = exec(t, map[string]command.Result{"apt list --upgradable": list}, nil, "--json", "updates", "list")
	var updates []proxmox.PackageUpdate
	if err := json.Unmarshal([]byte(stdout), &updates); err != nil || len(updates) != 1 {
		t.Fatalf("updates=%v err=%v", updates, err)
	}

	code, _, stderr, runner := exec(t, nil, nil, "updates", "upgrade")
	if code != 2 || !strings.Contains(stderr, "without --yes") || len(runner.Calls) != 0 {
		t.Fatalf("code=%d stderr=%q calls=%v", code, stderr, runner.Calls)
	}

	code, stdout, _, runner = exec(t, map[string]command.Result{upgrade: ok("upgraded libssl3\n")}, nil, "updates", "upgrade", "--yes")
	if code != 0 || !strings.Contains(stdout, "upgraded libssl3") || len(runner.Calls) != 1 {
		t.Fatalf("code=%d stdout=%q calls=%v", code, stdout, runner.Calls)
	}
}

func TestUpdatesUpgradeFailurePrintsOutput(t *testing.T) {
	code, _, stderr, _ := exec(t,
		map[string]command.Result{upgrade: {Stdout: []byte("partial output")}},
		map[string]error{upgrade: errors.New("exit status 100")},
		"updates", "upgrade", "--yes")
	if code != 1 || !strings.Contains(stderr, "partial output") || !strings.Contains(stderr, "Unable to run dist-upgrade") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestUpdatesUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"updates"}, {"updates", "bogus"}, {"updates", "upgrade", "extra", "--yes"}} {
		code, _, _, _ := exec(t, nil, nil, args...)
		if code != 2 {
			t.Fatalf("args=%v code=%d", args, code)
		}
	}
}

func userResponses() map[string]command.Result {
	return map[string]command.Result{
		"pvesh get /access/users --output-format json":              ok(`[{"userid":"root@pam","enable":true,"email":"r@x"},{"userid":"bob@pve","enable":true}]`),
		"pvesh get /access/users/root@pam/tfa --output-format json": ok(`[{"id":"1","type":"totp","description":"phone"}]`),
		"pvesh get /access/users/bob@pve/tfa --output-format json":  ok(`[]`),
	}
}

func TestUserList(t *testing.T) {
	code, stdout, stderr, _ := exec(t, userResponses(), nil, "user", "list")
	if code != 0 || !strings.Contains(stdout, "root@pam") || !strings.Contains(stdout, "bob@pve") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	code, stdout, _, _ = exec(t, userResponses(), nil, "--json", "user", "list")
	var users []proxmox.User
	if err := json.Unmarshal([]byte(stdout), &users); err != nil || len(users) != 2 {
		t.Fatalf("users=%v err=%v", users, err)
	}
	if !users[0].TFAEnabled || users[1].TFAEnabled {
		t.Fatalf("TFA flags wrong: %+v", users)
	}
}

func TestUserListIgnoresTFALookupFailure(t *testing.T) {
	responses := map[string]command.Result{
		"pvesh get /access/users --output-format json": ok(`[{"userid":"root@pam"}]`),
	}
	code, stdout, _, _ := exec(t, responses, nil, "user", "list")
	if code != 0 || !strings.Contains(stdout, "root@pam") {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
}

func TestTFAListAndDisable(t *testing.T) {
	code, stdout, stderr, _ := exec(t, userResponses(), nil, "tfa", "list", "root@pam")
	if code != 0 || !strings.Contains(stdout, "totp") || !strings.Contains(stdout, "phone") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	code, _, stderr, runner := exec(t, nil, nil, "tfa", "disable", "root@pam")
	if code != 2 || !strings.Contains(stderr, "without --yes") || len(runner.Calls) != 0 {
		t.Fatalf("code=%d stderr=%q calls=%v", code, stderr, runner.Calls)
	}

	code, stdout, _, runner = exec(t, map[string]command.Result{"pveum user tfa delete root@pam": {}}, nil, "tfa", "disable", "root@pam", "--yes")
	if code != 0 || !strings.Contains(stdout, "TFA disabled for root@pam") || len(runner.Calls) != 1 {
		t.Fatalf("code=%d stdout=%q calls=%v", code, stdout, runner.Calls)
	}
}

func TestTFARejectsInvalidInput(t *testing.T) {
	tests := []struct {
		args []string
		code int
	}{
		{[]string{"tfa", "list", "../x@pam"}, 1},
		{[]string{"tfa", "disable", "root", "--yes"}, 1},
		{[]string{"tfa", "list"}, 2},
		{[]string{"tfa", "bogus", "root@pam"}, 2},
		{[]string{"user"}, 2},
		{[]string{"user", "bogus"}, 2},
	}
	for _, tt := range tests {
		code, _, _, runner := exec(t, nil, nil, tt.args...)
		if code != tt.code || len(runner.Calls) != 0 {
			t.Fatalf("args=%v code=%d calls=%v", tt.args, code, runner.Calls)
		}
	}
}

func TestUsageListsNewCommands(t *testing.T) {
	for _, want := range []string{"vm start", "vm stop", "snapshot delete", "snapshot rollback", "backup list", "backup restore", "updates upgrade", "user list", "tfa disable", "--yes"} {
		if !strings.Contains(usage, want) {
			t.Errorf("usage is missing %q", want)
		}
	}
}

func TestVMRebootAndShutdownRequireYes(t *testing.T) {
	for _, action := range []string{"reboot", "shutdown"} {
		t.Run(action, func(t *testing.T) {
			code, _, stderr, runner := exec(t, nil, nil, "vm", action, "300")
			if code != 2 || !strings.Contains(stderr, "without --yes") || len(runner.Calls) != 0 {
				t.Fatalf("code=%d stderr=%q calls=%v", code, stderr, runner.Calls)
			}

			call := "pvesh create /nodes/pve/qemu/300/status/" + action
			code, stdout, stderr, runner := exec(t, map[string]command.Result{call: {}}, nil, "vm", action, "300", "--yes")
			if code != 0 || !strings.Contains(stdout, "Guest 300: "+action+" requested") || len(actionCalls(runner)) != 1 || actionCalls(runner)[0] != call {
				t.Fatalf("code=%d stdout=%q stderr=%q calls=%v", code, stdout, stderr, runner.Calls)
			}
		})
	}
}

func TestVMRebootJSONAndInvalidVMID(t *testing.T) {
	call := "pvesh create /nodes/pve/qemu/300/status/reboot"
	code, stdout, _, _ := exec(t, map[string]command.Result{call: ok(`"` + testUPID + `"`)}, nil, "--json", "--yes", "vm", "reboot", "300")
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil || code != 0 || payload["action"] != "reboot" || payload["upid"] != testUPID {
		t.Fatalf("code=%d payload=%v err=%v", code, payload, err)
	}
	if code, _, _, runner := exec(t, nil, nil, "vm", "shutdown", "abc", "--yes"); code != 1 || len(runner.Calls) != 0 {
		t.Fatalf("code=%d calls=%v", code, runner.Calls)
	}
}

func TestTUIDemoNeedsNoRootAndNoConfigFile(t *testing.T) {
	previousRoot, previousDemo := isRoot, runDemoTUI
	defer func() { isRoot, runDemoTUI = previousRoot, previousDemo }()
	isRoot = func() bool { return false }
	t.Setenv("HOME", t.TempDir())

	called := 0
	runDemoTUI = func() error { called++; return nil }

	var stdout, stderr bytes.Buffer
	code := RunWithClient([]string{"tui", "--demo"}, Options{}, proxmox.Client{}, &stdout, &stderr)
	if code != 0 || called != 1 {
		t.Fatalf("code=%d called=%d stderr=%q", code, called, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".pve-console")); err == nil {
		t.Fatal("the demo must not write the configuration file")
	}
}

func TestTUIDemoErrorsAndUnexpectedArguments(t *testing.T) {
	previousRoot, previousDemo := isRoot, runDemoTUI
	defer func() { isRoot, runDemoTUI = previousRoot, previousDemo }()
	isRoot = func() bool { return true }

	runDemoTUI = func() error { return errors.New("no terminal") }
	var stdout, stderr bytes.Buffer
	if code := RunWithClient([]string{"tui", "--demo"}, Options{}, proxmox.Client{}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "no terminal") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}

	runDemoTUI = func() error { t.Fatal("must not start"); return nil }
	for _, args := range [][]string{{"tui", "--bogus"}, {"tui", "--demo", "extra"}, {"tui", "demo"}} {
		stderr.Reset()
		if code := RunWithClient(args, Options{}, proxmox.Client{}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "unexpected argument") {
			t.Fatalf("args=%v code=%d stderr=%q", args, code, stderr.String())
		}
	}
}

func TestUsageMentionsTheDemo(t *testing.T) {
	if !strings.Contains(usage, "tui [--demo]") || !strings.Contains(usage, "fake data") {
		t.Fatal("the usage must document --demo")
	}
}
