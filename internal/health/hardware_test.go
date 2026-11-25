package health

import (
	"context"
	"errors"
	"strings"
	"testing"

	"pvc/internal/command"
	"pvc/internal/proxmox"
)

func ok(stdout string) command.Result { return command.Result{Stdout: []byte(stdout)} }

func byName(t *testing.T, checks []CheckResult, name string) CheckResult {
	t.Helper()
	for _, check := range checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("no check named %q in %+v", name, checks)
	return CheckResult{}
}

const mdHealthy = `Personalities : [raid1] [raid6]
md0 : active raid1 sdb1[1] sda1[0]
      1048576 blocks super 1.2 [2/2] [UU]

unused devices: <none>
`

func TestCheckMDRaidStates(t *testing.T) {
	tests := []struct {
		name    string
		mdstat  string
		want    Status
		message string
	}{
		{"healthy", mdHealthy, StatusOK, "2/2 devices"},
		{"degraded", "md0 : active raid1 sda1[0]\n      1048576 blocks super 1.2 [2/1] [U_]\n\n", StatusFail, "degraded (1/2 devices)"},
		{"recovering", "md0 : active raid1 sdc1[2] sda1[0]\n      1048576 blocks super 1.2 [2/1] [U_]\n      [==>..................]  recovery = 12.6% (132096/1048576) finish=0.1min speed=1000K/sec\n\n", StatusWarn, "recovering 12.6%"},
		{"faulty device", "md0 : active raid1 sdb1[1](F) sda1[0]\n      1048576 blocks super 1.2 [2/1] [U_]\n\n", StatusFail, "faulty devices: sdb1"},
		{"inactive", "md127 : inactive sdc[0](S)\n      1048576 blocks super 1.2\n\n", StatusFail, "inactive"},
		{"scrub", "md0 : active raid1 sdb1[1] sda1[0]\n      1048576 blocks super 1.2 [2/2] [UU]\n      [=>...................]  check =  8.0% (84000/1048576) finish=1.0min speed=2000K/sec\n\n", StatusInfo, "check 8.0%"},
		{"resync with all devices up", "md0 : active raid1 sdb1[1] sda1[0]\n      1048576 blocks super 1.2 [2/2] [UU]\n      [=>...................]  resync = 40.0% (1/2) finish=1.0min speed=2000K/sec\n\n", StatusInfo, "resync 40.0%"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &command.MockRunner{Responses: map[string]command.Result{"cat /proc/mdstat": ok(tt.mdstat)}}
			checks := CheckMDRaid(context.Background(), runner)
			if len(checks) != 1 {
				t.Fatalf("expected 1 check, got %+v", checks)
			}
			if checks[0].Status != tt.want || !strings.Contains(checks[0].Message, tt.message) {
				t.Fatalf("got %s %q, want %s containing %q", checks[0].Status, checks[0].Message, tt.want, tt.message)
			}
		})
	}
}

func TestCheckMDRaidSeveralArrays(t *testing.T) {
	data := mdHealthy + "md1 : active raid1 sdc1[0]\n      1048576 blocks super 1.2 [2/1] [U_]\n\n"
	checks := CheckMDRaid(context.Background(), &command.MockRunner{Responses: map[string]command.Result{"cat /proc/mdstat": ok(data)}})
	if len(checks) != 2 || byName(t, checks, "raid-md0").Status != StatusOK || byName(t, checks, "raid-md1").Status != StatusFail {
		t.Fatalf("unexpected checks: %+v", checks)
	}
}

func TestCheckMDRaidReportsNothingWithoutArrays(t *testing.T) {
	runners := map[string]*command.MockRunner{
		"no array":       {Responses: map[string]command.Result{"cat /proc/mdstat": ok("Personalities :\nunused devices: <none>\n")}},
		"no file":        {Responses: map[string]command.Result{"cat /proc/mdstat": {Stderr: []byte("cat: /proc/mdstat: No such file or directory")}}, Errors: map[string]error{"cat /proc/mdstat": errors.New("exit status 1")}},
		"cat is missing": {Missing: []string{"cat"}},
		"empty":          {Responses: map[string]command.Result{"cat /proc/mdstat": {}}},
	}
	for name, runner := range runners {
		t.Run(name, func(t *testing.T) {
			if checks := CheckMDRaid(context.Background(), runner); len(checks) != 0 {
				t.Fatalf("expected no check, got %+v", checks)
			}
		})
	}
}

func TestCheckMDRaidWarnsOnUnexpectedError(t *testing.T) {
	runner := &command.MockRunner{Errors: map[string]error{"cat /proc/mdstat": errors.New("permission denied")}}
	checks := CheckMDRaid(context.Background(), runner)
	if len(checks) != 1 || checks[0].Status != StatusWarn || !strings.Contains(checks[0].Message, "permission denied") {
		t.Fatalf("unexpected checks: %+v", checks)
	}
}

const smartScan = `/dev/sda -d scsi # /dev/sda, SCSI device
/dev/nvme0 -d nvme # /dev/nvme0, NVMe device
`

func TestCheckSMARTStatuses(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   Status
	}{
		{"sata passed", "=== START OF READ SMART DATA SECTION ===\nSMART overall-health self-assessment test result: PASSED\n", StatusOK},
		{"sata failed", "SMART overall-health self-assessment test result: FAILED!\n", StatusFail},
		{"scsi ok", "SMART Health Status: OK\n", StatusOK},
		{"scsi failing", "SMART Health Status: FAILURE PREDICTION THRESHOLD EXCEEDED: ascq=0x5\n", StatusFail},
		{"unknown result", "SMART overall-health self-assessment test result: ???\n", StatusWarn},
		{"unavailable", "SMART support is: Unavailable - device lacks SMART capability.\n", StatusInfo},
		{"nothing recognised", "Smartctl open device: /dev/sda failed\n", StatusInfo},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &command.MockRunner{Responses: map[string]command.Result{
				"smartctl --scan":              ok("/dev/sda -d scsi # /dev/sda, SCSI device\n"),
				"smartctl -H -d scsi /dev/sda": ok(tt.output),
			}}
			checks := CheckSMART(context.Background(), runner)
			if len(checks) != 1 || checks[0].Name != "smart-sda" || checks[0].Status != tt.want {
				t.Fatalf("got %+v, want status %s", checks, tt.want)
			}
		})
	}
}

func TestCheckSMARTCoversEveryDisk(t *testing.T) {
	runner := &command.MockRunner{Responses: map[string]command.Result{
		"smartctl --scan":                ok(smartScan),
		"smartctl -H -d scsi /dev/sda":   ok("SMART overall-health self-assessment test result: PASSED\n"),
		"smartctl -H -d nvme /dev/nvme0": ok("SMART overall-health self-assessment test result: FAILED!\n"),
	}}
	checks := CheckSMART(context.Background(), runner)
	if len(checks) != 2 || byName(t, checks, "smart-sda").Status != StatusOK || byName(t, checks, "smart-nvme0").Status != StatusFail {
		t.Fatalf("unexpected checks: %+v", checks)
	}
	if WorstStatus(checks) != StatusFail {
		t.Fatal("one failing disk must fail the whole group")
	}
}

func TestCheckSMARTUsesOutputEvenWhenSmartctlExitsNonZero(t *testing.T) {
	// smartctl sets bits in its exit code for warnings: the output still counts.
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"smartctl --scan":              ok("/dev/sda -d scsi # x\n"),
			"smartctl -H -d scsi /dev/sda": ok("SMART overall-health self-assessment test result: FAILED!\n"),
		},
		Errors: map[string]error{"smartctl -H -d scsi /dev/sda": errors.New("exit status 8")},
	}
	checks := CheckSMART(context.Background(), runner)
	if len(checks) != 1 || checks[0].Status != StatusFail {
		t.Fatalf("unexpected checks: %+v", checks)
	}
}

func TestCheckSMARTWarnsWhenAMissingOutputIsAnError(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{"smartctl --scan": ok("/dev/sda -d scsi # x\n")},
		Errors:    map[string]error{"smartctl -H -d scsi /dev/sda": errors.New("permission denied")},
	}
	checks := CheckSMART(context.Background(), runner)
	if len(checks) != 1 || checks[0].Status != StatusWarn || !strings.Contains(checks[0].Message, "permission denied") {
		t.Fatalf("unexpected checks: %+v", checks)
	}
}

func TestCheckSMARTToolAndScanProblems(t *testing.T) {
	checks := CheckSMART(context.Background(), &command.MockRunner{Missing: []string{"smartctl"}})
	if len(checks) != 1 || checks[0].Status != StatusInfo || !strings.Contains(checks[0].Message, "not installed") {
		t.Fatalf("missing tool: %+v", checks)
	}

	checks = CheckSMART(context.Background(), &command.MockRunner{Errors: map[string]error{"smartctl --scan": errors.New("boom")}})
	if len(checks) != 1 || checks[0].Status != StatusWarn {
		t.Fatalf("scan error: %+v", checks)
	}

	checks = CheckSMART(context.Background(), &command.MockRunner{Responses: map[string]command.Result{"smartctl --scan": ok("")}})
	if len(checks) != 1 || checks[0].Status != StatusInfo {
		t.Fatalf("no device: %+v", checks)
	}
}

func TestParseSmartScanIgnoresUnsafeDevices(t *testing.T) {
	data := strings.Join([]string{
		"/dev/sda -d scsi # ok",
		"/dev/sdb;rm -rf / -d scsi # injection",
		"--help -d scsi # option",
		"/dev/../etc/passwd # traversal",
		"/dev/sdc -d bad;rm # bad type",
		"/dev/bus/0 -d megaraid,0 # raid controller",
		"",
		"# only a comment",
	}, "\n")
	devices := parseSmartScan(data)
	want := []smartDevice{{"/dev/sda", "scsi"}, {"/dev/sdc", ""}, {"/dev/bus/0", "megaraid,0"}}
	if len(devices) != len(want) {
		t.Fatalf("devices=%+v want %+v", devices, want)
	}
	for i := range want {
		if devices[i] != want[i] {
			t.Fatalf("devices[%d]=%+v want %+v", i, devices[i], want[i])
		}
	}
	if name := smartCheckName(smartDevice{"/dev/bus/0", "megaraid,0"}); name != "smart-bus-0-megaraid,0" {
		t.Fatalf("name=%q", name)
	}
}

func TestCheckZFS(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want Status
	}{
		{"online", "rpool\tONLINE\t12%\n", StatusOK},
		{"degraded", "rpool\tDEGRADED\t12%\n", StatusFail},
		{"faulted", "rpool\tFAULTED\t12%\n", StatusFail},
		{"capacity warning", "rpool\tONLINE\t85%\n", StatusWarn},
		{"capacity critical", "rpool\tONLINE\t95%\n", StatusFail},
		{"degraded and almost full", "rpool\tDEGRADED\t85%\n", StatusFail},
		{"unreadable capacity", "rpool\tONLINE\t-\n", StatusWarn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &command.MockRunner{Responses: map[string]command.Result{"zpool list -H -o name,health,capacity": ok(tt.out)}}
			checks := CheckZFS(context.Background(), runner, HardwareOptions{Warning: 0.80, Critical: 0.90})
			if len(checks) != 1 || checks[0].Name != "zfs-rpool" || checks[0].Status != tt.want {
				t.Fatalf("got %+v, want %s", checks, tt.want)
			}
		})
	}
}

func TestCheckZFSSeveralPoolsAndAbsence(t *testing.T) {
	runner := &command.MockRunner{Responses: map[string]command.Result{"zpool list -H -o name,health,capacity": ok("rpool\tONLINE\t10%\ntank\tDEGRADED\t50%\n")}}
	checks := CheckZFS(context.Background(), runner, HardwareOptions{Warning: 0.80, Critical: 0.90})
	if len(checks) != 2 || byName(t, checks, "zfs-rpool").Status != StatusOK || byName(t, checks, "zfs-tank").Status != StatusFail {
		t.Fatalf("unexpected checks: %+v", checks)
	}

	if checks := CheckZFS(context.Background(), &command.MockRunner{Missing: []string{"zpool"}}, HardwareOptions{}); len(checks) != 0 {
		t.Fatalf("ZFS not installed must report nothing, got %+v", checks)
	}
	if checks := CheckZFS(context.Background(), &command.MockRunner{Responses: map[string]command.Result{"zpool list -H -o name,health,capacity": ok("")}}, HardwareOptions{}); len(checks) != 0 {
		t.Fatalf("no pool must report nothing, got %+v", checks)
	}
	checks = CheckZFS(context.Background(), &command.MockRunner{Errors: map[string]error{"zpool list -H -o name,health,capacity": errors.New("permission denied")}}, HardwareOptions{})
	if len(checks) != 1 || checks[0].Status != StatusWarn {
		t.Fatalf("unexpected error must warn, got %+v", checks)
	}
}

const (
	lvsCall = "lvs --noheadings --separator ; -o vg_name,lv_name,lv_attr,data_percent,metadata_percent"
	vgsCall = "vgs --noheadings --separator ; -o vg_name,vg_attr"
)

func TestCheckLVMThinPools(t *testing.T) {
	tests := []struct {
		name string
		lvs  string
		want Status
	}{
		{"healthy", "  pve;data;twi-aotz--;12.50;1.20\n  pve;root;-wi-ao----;;\n  pve;swap;-wi-ao----;;\n", StatusOK},
		{"data warning", "  pve;data;twi-aotz--;85.00;1.20\n", StatusWarn},
		{"data critical", "  pve;data;twi-aotz--;95.00;1.20\n", StatusFail},
		{"metadata critical", "  pve;data;twi-aotz--;10.00;93.00\n", StatusFail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &command.MockRunner{Responses: map[string]command.Result{lvsCall: ok(tt.lvs), vgsCall: ok("  pve;wz--n-\n")}}
			checks := CheckLVM(context.Background(), runner, HardwareOptions{Warning: 0.80, Critical: 0.90})
			if len(checks) != 1 || checks[0].Name != "lvm-pve/data" || checks[0].Status != tt.want {
				t.Fatalf("got %+v, want %s", checks, tt.want)
			}
		})
	}
}

func TestCheckLVMOnlyReportsThinPools(t *testing.T) {
	runner := &command.MockRunner{Responses: map[string]command.Result{lvsCall: ok("  pve;root;-wi-ao----;;\n"), vgsCall: ok("  pve;wz--n-\n")}}
	if checks := CheckLVM(context.Background(), runner, HardwareOptions{Warning: 0.8, Critical: 0.9}); len(checks) != 0 {
		t.Fatalf("plain volumes must not be reported, got %+v", checks)
	}
}

func TestCheckLVMPartialVolumeGroup(t *testing.T) {
	runner := &command.MockRunner{Responses: map[string]command.Result{lvsCall: ok(""), vgsCall: ok("  pve;wz-pn-\n  data;wz--n-\n")}}
	checks := CheckLVM(context.Background(), runner, HardwareOptions{Warning: 0.8, Critical: 0.9})
	if len(checks) != 1 || checks[0].Name != "lvm-vg-pve" || checks[0].Status != StatusFail {
		t.Fatalf("unexpected checks: %+v", checks)
	}
}

func TestCheckLVMAbsentOrFailing(t *testing.T) {
	opts := HardwareOptions{Warning: 0.8, Critical: 0.9}
	if checks := CheckLVM(context.Background(), &command.MockRunner{Missing: []string{"lvs"}}, opts); len(checks) != 0 {
		t.Fatalf("LVM not installed must report nothing, got %+v", checks)
	}

	runner := &command.MockRunner{Errors: map[string]error{lvsCall: errors.New("permission denied")}}
	checks := CheckLVM(context.Background(), runner, opts)
	if len(checks) != 1 || checks[0].Status != StatusWarn {
		t.Fatalf("lvs error: %+v", checks)
	}

	runner = &command.MockRunner{Responses: map[string]command.Result{lvsCall: ok("")}, Errors: map[string]error{vgsCall: errors.New("boom")}}
	checks = CheckLVM(context.Background(), runner, opts)
	if len(checks) != 1 || checks[0].Status != StatusWarn || !strings.Contains(checks[0].Message, "volume groups") {
		t.Fatalf("vgs error: %+v", checks)
	}
}

func TestCheckHardwareIsEmptyWhenNothingIsInstalled(t *testing.T) {
	runner := &command.MockRunner{Responses: map[string]command.Result{"cat /proc/mdstat": {}}, Missing: []string{"smartctl", "zpool", "lvs", "vgs"}}
	checks := CheckHardware(context.Background(), runner, HardwareOptions{})
	if len(checks) != 1 || checks[0].Name != "smart" || checks[0].Status != StatusInfo {
		t.Fatalf("only the smartctl notice is expected, got %+v", checks)
	}
	if WorstStatus(checks) != StatusInfo || ExitCode(StatusInfo) != 0 {
		t.Fatal("a missing tool must not change the exit code")
	}
}

func TestCheckHardwareDefaultThresholds(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"cat /proc/mdstat":                      {},
			"smartctl --scan":                       {},
			"zpool list -H -o name,health,capacity": ok("rpool\tONLINE\t85%\n"),
		},
		Missing: []string{"lvs", "vgs"},
	}
	checks := CheckHardware(context.Background(), runner, HardwareOptions{})
	if byName(t, checks, "zfs-rpool").Status != StatusWarn {
		t.Fatalf("default thresholds should apply, got %+v", checks)
	}
}

func TestRunIncludesHardwareChecks(t *testing.T) {
	client := proxmox.Client{Runner: &command.MockRunner{
		Responses: map[string]command.Result{
			"pveversion": ok("pve-manager/9.2.20\n"),
			"uname -r":   ok("7.0.14-14-pve\n"),
			"df -P":      ok("Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/root 100 10 90 10% /\n"),
			"pvesh get /nodes/pve/qemu --output-format json":    ok(`[]`),
			"pvesh get /nodes/pve/lxc --output-format json":     ok(`[]`),
			"pvesh get /nodes/pve/storage --output-format json": ok(`[]`),
			"cat /proc/mdstat": ok("md0 : active raid1 sda1[0]\n      1048576 blocks super 1.2 [2/1] [U_]\n\n"),
		},
		Missing: []string{"smartctl", "zpool", "lvs", "vgs"},
	}, Node: "pve"}

	report, err := Run(context.Background(), client, DefaultOptions())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if byName(t, report.Checks, "raid-md0").Status != StatusFail || report.Status != StatusFail || ExitCode(report.Status) != 2 {
		t.Fatalf("a degraded array must fail the report: %+v", report)
	}
}

// The mocks fake a missing binary; this one checks the real runner reports it
// in a way toolMissing recognises, which every hardware check relies on.
func TestToolMissingRecognisesTheRealRunner(t *testing.T) {
	runner := command.NewExecRunner(0)
	_, err := runner.Run(context.Background(), "pve-console-no-such-program")
	if err == nil {
		t.Fatal("expected an error for a program that does not exist")
	}
	if !toolMissing(err) {
		t.Fatalf("toolMissing must recognise %T: %v", err, err)
	}
}

func TestErrTextIsOneShortLine(t *testing.T) {
	err := errors.New("lvs failed: WARNING: Running as a non-root user.\n  /run/lock/lvm: open failed:  Permission denied\n")
	got := errText(err)
	if strings.Contains(got, "\n") || strings.Contains(got, "  ") {
		t.Fatalf("errText must flatten whitespace, got %q", got)
	}
	if !strings.Contains(got, "Permission denied") {
		t.Fatalf("errText lost the message: %q", got)
	}
	if long := errText(errors.New(strings.Repeat("x", 500))); len(long) > 210 {
		t.Fatalf("errText must truncate, got %d chars", len(long))
	}

	runner := &command.MockRunner{Errors: map[string]error{lvsCall: err}}
	checks := CheckLVM(context.Background(), runner, HardwareOptions{Warning: 0.8, Critical: 0.9})
	if len(checks) != 1 || strings.Contains(checks[0].Message, "\n") {
		t.Fatalf("report messages must be one line: %+v", checks)
	}
}
