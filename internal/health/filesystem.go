package health

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"strings"

	"pvc/internal/command"
)

type FilesystemOptions struct {
	Warning  float64
	Critical float64
}

func CheckFilesystems(ctx context.Context, runner command.Runner, opts FilesystemOptions) []CheckResult {
	if opts.Warning <= 0 {
		opts.Warning = 0.80
	}
	if opts.Critical <= 0 {
		opts.Critical = 0.90
	}

	result, err := runner.Run(ctx, "df", "-P")
	if err != nil {
		return []CheckResult{{Name: "filesystems", Status: StatusWarn, Message: err.Error()}}
	}

	checks, err := parseDF(result.Stdout, opts)
	if err != nil {
		return []CheckResult{{Name: "filesystems", Status: StatusWarn, Message: err.Error()}}
	}

	return checks
}

func parseDF(data []byte, opts FilesystemOptions) ([]CheckResult, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	line := 0
	var checks []CheckResult
	for scanner.Scan() {
		line++
		if line == 1 {
			continue
		}

		fields := strings.Fields(scanner.Text())
		if len(fields) < 6 {
			continue
		}

		mount := fields[5]
		usageText := strings.TrimSuffix(fields[4], "%")
		usagePercent, err := strconv.ParseFloat(usageText, 64)
		if err != nil {
			return nil, fmt.Errorf("unable to parse df usage %q", fields[4])
		}

		usage := usagePercent / 100
		status := StatusOK
		if usage >= opts.Critical {
			status = StatusFail
		} else if usage >= opts.Warning {
			status = StatusWarn
		}

		checks = append(checks, CheckResult{
			Name:    "fs-" + mount,
			Status:  status,
			Message: fmt.Sprintf("%s %.0f%% used", mount, usagePercent),
			Details: map[string]any{
				"filesystem": fields[0],
				"mount":      mount,
				"usage":      usage,
			},
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if len(checks) == 0 {
		return []CheckResult{{Name: "filesystems", Status: StatusInfo, Message: "no filesystems found"}}, nil
	}

	return checks, nil
}
