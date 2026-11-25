package health

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"pvc/internal/command"
)

// CheckZFS reports the health and the usage of every ZFS pool. Nothing is
// reported when ZFS is not installed or there is no pool.
func CheckZFS(ctx context.Context, runner command.Runner, opts HardwareOptions) []CheckResult {
	result, err := runner.Run(ctx, "zpool", "list", "-H", "-o", "name,health,capacity")
	if err != nil {
		if toolMissing(err) {
			return nil
		}
		return []CheckResult{{Name: "zfs", Status: StatusWarn, Message: "unable to list ZFS pools: " + errText(err)}}
	}

	var checks []CheckResult
	for _, line := range strings.Split(string(result.Stdout), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		name, health := fields[0], strings.ToUpper(fields[1])
		capacity, err := strconv.ParseFloat(strings.TrimSuffix(fields[2], "%"), 64)
		if err != nil {
			checks = append(checks, CheckResult{Name: "zfs-" + name, Status: StatusWarn, Message: fmt.Sprintf("%s: unreadable capacity %q", name, fields[2])})
			continue
		}

		// A pool that is not ONLINE has lost redundancy or is unusable.
		status := StatusOK
		if health != "ONLINE" {
			status = StatusFail
		}
		status = worse(status, usageStatus(capacity/100, opts))

		checks = append(checks, CheckResult{
			Name:    "zfs-" + name,
			Status:  status,
			Message: fmt.Sprintf("%s %s, %.0f%% used", name, health, capacity),
			Details: map[string]any{"pool": name, "health": health, "usage": capacity / 100},
		})
	}
	return checks
}
