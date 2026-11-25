package health

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"pvc/internal/command"
)

var (
	mdStatusPattern   = regexp.MustCompile(`\[(\d+)/(\d+)\]\s+\[([U_]+)\]`)
	mdProgressPattern = regexp.MustCompile(`\b(resync|recovery|reshape|check)\s*=\s*([0-9.]+)%`)
)

// mdArray is one array of /proc/mdstat.
type mdArray struct {
	name     string
	state    string
	total    int
	working  int
	faulty   []string
	activity string
	progress float64
}

// CheckMDRaid reports the state of the Linux software RAID arrays. Nothing is
// reported when /proc/mdstat is missing or lists no array.
func CheckMDRaid(ctx context.Context, runner command.Runner) []CheckResult {
	result, err := runner.Run(ctx, "cat", "/proc/mdstat")
	if err != nil {
		if toolMissing(err) || strings.Contains(string(result.Stderr), "No such file") {
			return nil
		}
		return []CheckResult{{Name: "raid", Status: StatusWarn, Message: "unable to read /proc/mdstat: " + errText(err)}}
	}

	var checks []CheckResult
	for _, array := range parseMDStat(string(result.Stdout)) {
		checks = append(checks, mdArrayCheck(array))
	}
	return checks
}

func parseMDStat(data string) []mdArray {
	var arrays []mdArray
	var current *mdArray

	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && strings.HasPrefix(fields[0], "md") && fields[1] == ":" {
			arrays = append(arrays, mdArray{name: fields[0], state: fields[2], total: -1})
			current = &arrays[len(arrays)-1]
			for _, field := range fields[3:] {
				if strings.HasSuffix(field, "(F)") {
					current.faulty = append(current.faulty, strings.SplitN(field, "[", 2)[0])
				}
			}
			continue
		}
		if current == nil {
			continue
		}
		if strings.TrimSpace(line) == "" {
			current = nil
			continue
		}
		if match := mdStatusPattern.FindStringSubmatch(line); match != nil {
			current.total, _ = strconv.Atoi(match[1])
			current.working, _ = strconv.Atoi(match[2])
			if strings.Contains(match[3], "_") && current.working >= current.total {
				current.working = strings.Count(match[3], "U")
			}
		}
		if match := mdProgressPattern.FindStringSubmatch(line); match != nil {
			current.activity = match[1]
			current.progress, _ = strconv.ParseFloat(match[2], 64)
		}
	}
	return arrays
}

func mdArrayCheck(array mdArray) CheckResult {
	check := CheckResult{
		Name:   "raid-" + array.name,
		Status: StatusOK,
		Details: map[string]any{
			"array": array.name,
			"state": array.state,
		},
	}
	if array.total >= 0 {
		check.Details["devices"] = array.total
		check.Details["working"] = array.working
	}
	if array.activity != "" {
		check.Details["activity"] = array.activity
		check.Details["progress"] = array.progress
	}

	degraded := array.total >= 0 && array.working < array.total
	switch {
	case array.state == "inactive":
		check.Status = StatusFail
		check.Message = fmt.Sprintf("%s is inactive", array.name)
	case len(array.faulty) > 0:
		check.Status = StatusFail
		check.Message = fmt.Sprintf("%s has faulty devices: %s", array.name, strings.Join(array.faulty, ", "))
		check.Details["faulty"] = array.faulty
	case degraded && array.activity == "recovery":
		check.Status = StatusWarn
		check.Message = fmt.Sprintf("%s is degraded (%d/%d devices), recovering %.1f%%", array.name, array.working, array.total, array.progress)
	case degraded:
		check.Status = StatusFail
		check.Message = fmt.Sprintf("%s is degraded (%d/%d devices)", array.name, array.working, array.total)
	case array.activity != "":
		check.Status = StatusInfo
		check.Message = fmt.Sprintf("%s is healthy, %s %.1f%%", array.name, array.activity, array.progress)
	default:
		check.Message = fmt.Sprintf("%s %s", array.name, array.state)
		if array.total >= 0 {
			check.Message = fmt.Sprintf("%s %s (%d/%d devices)", array.name, array.state, array.working, array.total)
		}
	}
	return check
}
