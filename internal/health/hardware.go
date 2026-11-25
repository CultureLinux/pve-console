package health

import (
	"context"
	"errors"
	"os/exec"
	"strings"

	"pvc/internal/command"
)

// HardwareOptions holds the thresholds of the hardware checks.
type HardwareOptions struct {
	// Warning and Critical apply to the usage (0 to 1) of ZFS pools and LVM thin pools.
	Warning  float64
	Critical float64
}

// CheckHardware runs the RAID, SMART, ZFS and LVM checks. A tool that is not
// installed is skipped (SMART reports it once), because most nodes only use some of them.
func CheckHardware(ctx context.Context, runner command.Runner, opts HardwareOptions) []CheckResult {
	if opts.Warning <= 0 {
		opts.Warning = 0.80
	}
	if opts.Critical <= 0 {
		opts.Critical = 0.90
	}

	var checks []CheckResult
	checks = append(checks, CheckMDRaid(ctx, runner)...)
	checks = append(checks, CheckSMART(ctx, runner)...)
	checks = append(checks, CheckZFS(ctx, runner, opts)...)
	checks = append(checks, CheckLVM(ctx, runner, opts)...)
	return checks
}

// errText flattens an error message to one short line for a report: tools like
// lvs print several lines of warnings.
func errText(err error) string {
	text := strings.Join(strings.Fields(err.Error()), " ")
	if len(text) > 200 {
		text = text[:200] + "..."
	}
	return text
}

// toolMissing reports whether a command failed because the program is not installed.
func toolMissing(err error) bool {
	return errors.Is(err, exec.ErrNotFound)
}

// usageStatus maps a usage ratio (0 to 1) to a status.
func usageStatus(usage float64, opts HardwareOptions) Status {
	switch {
	case usage >= opts.Critical:
		return StatusFail
	case usage >= opts.Warning:
		return StatusWarn
	}
	return StatusOK
}

// worse returns the more severe of two statuses.
func worse(a Status, b Status) Status {
	if rank(b) > rank(a) {
		return b
	}
	return a
}
