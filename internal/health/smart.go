package health

import (
	"context"
	"regexp"
	"strings"

	"pvc/internal/command"
)

var (
	smartDevicePattern = regexp.MustCompile(`^/dev/[A-Za-z0-9/_.-]+$`)
	smartTypePattern   = regexp.MustCompile(`^[A-Za-z0-9,+_-]+$`)
)

// smartDevice is a device listed by "smartctl --scan".
type smartDevice struct {
	path string
	kind string
}

// CheckSMART reports the SMART health of every disk found by smartctl. smartctl
// not being installed is reported once, as information.
func CheckSMART(ctx context.Context, runner command.Runner) []CheckResult {
	scan, err := runner.Run(ctx, "smartctl", "--scan")
	if err != nil {
		if toolMissing(err) {
			return []CheckResult{{Name: "smart", Status: StatusInfo, Message: "smartctl is not installed: disk health is not checked"}}
		}
		return []CheckResult{{Name: "smart", Status: StatusWarn, Message: "unable to scan disks: " + errText(err)}}
	}

	devices := parseSmartScan(string(scan.Stdout))
	if len(devices) == 0 {
		return []CheckResult{{Name: "smart", Status: StatusInfo, Message: "no disk reported by smartctl"}}
	}

	checks := make([]CheckResult, 0, len(devices))
	for _, device := range devices {
		args := []string{"-H"}
		if device.kind != "" {
			args = append(args, "-d", device.kind)
		}
		args = append(args, device.path)

		// smartctl exits with a bit field: a non-zero code does not mean the command
		// failed, so the output decides. Only an empty output is an error.
		result, err := runner.Run(ctx, "smartctl", args...)
		check := CheckResult{
			Name:    smartCheckName(device),
			Details: map[string]any{"device": device.path},
		}
		if device.kind != "" {
			check.Details["type"] = device.kind
		}
		if err != nil && len(result.Stdout) == 0 {
			check.Status = StatusWarn
			check.Message = device.path + ": unable to read SMART status: " + errText(err)
		} else {
			check.Status, check.Message = parseSmartHealth(string(result.Stdout), device.path)
		}
		checks = append(checks, check)
	}
	return checks
}

// parseSmartScan reads the lines of "smartctl --scan", e.g.
// "/dev/sda -d scsi # /dev/sda, SCSI device". Anything that is not a plain
// device path is ignored, because the path is passed to a command.
func parseSmartScan(data string) []smartDevice {
	var devices []smartDevice
	for _, line := range strings.Split(data, "\n") {
		line, _, _ = strings.Cut(line, "#")
		fields := strings.Fields(line)
		if len(fields) == 0 || !smartDevicePattern.MatchString(fields[0]) || strings.Contains(fields[0], "..") {
			continue
		}
		device := smartDevice{path: fields[0]}
		if len(fields) >= 3 && fields[1] == "-d" && smartTypePattern.MatchString(fields[2]) {
			device.kind = fields[2]
		}
		devices = append(devices, device)
	}
	return devices
}

func smartCheckName(device smartDevice) string {
	name := strings.ReplaceAll(strings.TrimPrefix(device.path, "/dev/"), "/", "-")
	if strings.Contains(device.kind, ",") {
		name += "-" + device.kind
	}
	return "smart-" + name
}

// parseSmartHealth turns the output of "smartctl -H" into a status.
func parseSmartHealth(output string, device string) (Status, string) {
	for _, line := range strings.Split(output, "\n") {
		lower := strings.ToLower(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(lower, "smart overall-health self-assessment test result:"):
			result := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			switch {
			case strings.Contains(strings.ToUpper(result), "PASSED"):
				return StatusOK, device + ": SMART PASSED"
			case strings.Contains(strings.ToUpper(result), "FAILED"):
				return StatusFail, device + ": SMART FAILED, replace the disk"
			}
			return StatusWarn, device + ": SMART " + result
		case strings.HasPrefix(lower, "smart health status:"):
			result := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			if strings.EqualFold(result, "OK") {
				return StatusOK, device + ": SMART OK"
			}
			return StatusFail, device + ": SMART " + result
		}
	}
	return StatusInfo, device + ": SMART status not available"
}
