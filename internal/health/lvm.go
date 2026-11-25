package health

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"pvc/internal/command"
)

// CheckLVM reports the usage of LVM thin pools (data and metadata, such as
// Proxmox "local-lvm") and volume groups with a missing physical volume.
// Nothing is reported when LVM is not installed.
func CheckLVM(ctx context.Context, runner command.Runner, opts HardwareOptions) []CheckResult {
	var checks []CheckResult

	lvs, err := runner.Run(ctx, "lvs", "--noheadings", "--separator", ";", "-o", "vg_name,lv_name,lv_attr,data_percent,metadata_percent")
	if err != nil {
		if toolMissing(err) {
			return nil
		}
		return []CheckResult{{Name: "lvm", Status: StatusWarn, Message: "unable to list logical volumes: " + errText(err)}}
	}
	checks = append(checks, parseThinPools(string(lvs.Stdout), opts)...)

	vgs, err := runner.Run(ctx, "vgs", "--noheadings", "--separator", ";", "-o", "vg_name,vg_attr")
	if err != nil {
		if !toolMissing(err) {
			checks = append(checks, CheckResult{Name: "lvm", Status: StatusWarn, Message: "unable to list volume groups: " + errText(err)})
		}
		return checks
	}
	return append(checks, parsePartialVGs(string(vgs.Stdout))...)
}

func parseThinPools(data string, opts HardwareOptions) []CheckResult {
	var checks []CheckResult
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Split(strings.TrimSpace(line), ";")
		if len(fields) < 5 || !strings.HasPrefix(strings.TrimSpace(fields[2]), "t") {
			continue
		}
		vg, lv := strings.TrimSpace(fields[0]), strings.TrimSpace(fields[1])
		dataUsed := parsePercent(fields[3])
		metaUsed := parsePercent(fields[4])

		status := worse(usageStatus(dataUsed/100, opts), usageStatus(metaUsed/100, opts))
		checks = append(checks, CheckResult{
			Name:    "lvm-" + vg + "/" + lv,
			Status:  status,
			Message: fmt.Sprintf("thin pool %s/%s data %.0f%%, metadata %.0f%%", vg, lv, dataUsed, metaUsed),
			Details: map[string]any{"vg": vg, "lv": lv, "data_usage": dataUsed / 100, "metadata_usage": metaUsed / 100},
		})
	}
	return checks
}

// parsePartialVGs reports the volume groups whose attributes flag a missing
// physical volume (4th attribute character "p").
func parsePartialVGs(data string) []CheckResult {
	var checks []CheckResult
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Split(strings.TrimSpace(line), ";")
		if len(fields) < 2 {
			continue
		}
		vg, attr := strings.TrimSpace(fields[0]), strings.TrimSpace(fields[1])
		if len(attr) >= 4 && attr[3] == 'p' {
			checks = append(checks, CheckResult{
				Name:    "lvm-vg-" + vg,
				Status:  StatusFail,
				Message: fmt.Sprintf("volume group %s has a missing physical volume", vg),
				Details: map[string]any{"vg": vg, "attr": attr},
			})
		}
	}
	return checks
}

func parsePercent(value string) float64 {
	percent, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0
	}
	return percent
}
