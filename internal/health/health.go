package health

import (
	"context"
	"fmt"

	"pvc/internal/proxmox"
)

type Status string

const (
	StatusOK   Status = "ok"
	StatusInfo Status = "info"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

type CheckResult struct {
	Name    string         `json:"name"`
	Status  Status         `json:"status"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type Report struct {
	Node   string        `json:"node"`
	Status Status        `json:"status"`
	Checks []CheckResult `json:"checks"`
}

type Options struct {
	StorageWarning     float64
	StorageCritical    float64
	FilesystemWarning  float64
	FilesystemCritical float64
}

func DefaultOptions() Options {
	return Options{
		StorageWarning:     0.80,
		StorageCritical:    0.90,
		FilesystemWarning:  0.80,
		FilesystemCritical: 0.90,
	}
}

func Run(ctx context.Context, client proxmox.Client, opts Options) (Report, error) {
	if opts.StorageWarning <= 0 {
		opts.StorageWarning = 0.80
	}
	if opts.StorageCritical <= 0 {
		opts.StorageCritical = 0.90
	}
	if opts.FilesystemWarning <= 0 {
		opts.FilesystemWarning = 0.80
	}
	if opts.FilesystemCritical <= 0 {
		opts.FilesystemCritical = 0.90
	}

	node, err := client.NodeInfo(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("unable to read node info: %w", err)
	}

	report := Report{
		Node:   node.Name,
		Status: StatusOK,
	}

	report.Checks = append(report.Checks, checkNode(node)...)
	report.Checks = append(report.Checks, CheckFilesystems(ctx, client.Runner, FilesystemOptions{
		Warning:  opts.FilesystemWarning,
		Critical: opts.FilesystemCritical,
	})...)

	guests, err := client.Guests(ctx)
	if err != nil {
		report.Checks = append(report.Checks, CheckResult{Name: "guests", Status: StatusFail, Message: err.Error()})
	} else {
		report.Checks = append(report.Checks, checkGuests(guests))
	}

	storages, err := client.Storages(ctx)
	if err != nil {
		report.Checks = append(report.Checks, CheckResult{Name: "storage", Status: StatusFail, Message: err.Error()})
	} else {
		report.Checks = append(report.Checks, checkStorages(storages, opts)...)
	}

	report.Status = WorstStatus(report.Checks)
	return report, nil
}

func checkNode(node proxmox.NodeInfo) []CheckResult {
	checks := []CheckResult{
		{Name: "proxmox", Status: StatusOK, Message: valueOrUnknown(node.ProxmoxVersion)},
		{Name: "kernel", Status: StatusOK, Message: valueOrUnknown(node.Kernel)},
	}

	if node.ProxmoxVersion == "" {
		checks[0].Status = StatusWarn
	}
	if node.Kernel == "" {
		checks[1].Status = StatusWarn
	}

	return checks
}

func checkGuests(guests []proxmox.Guest) CheckResult {
	running := 0
	stopped := 0
	for _, guest := range guests {
		if guest.Status == "running" {
			running++
		} else {
			stopped++
		}
	}

	status := StatusOK
	if len(guests) == 0 {
		status = StatusInfo
	}

	return CheckResult{
		Name:    "guests",
		Status:  status,
		Message: fmt.Sprintf("%d running, %d stopped", running, stopped),
		Details: map[string]any{
			"running": running,
			"stopped": stopped,
			"total":   len(guests),
		},
	}
}

func checkStorages(storages []proxmox.Storage, opts Options) []CheckResult {
	checks := make([]CheckResult, 0, len(storages))
	for _, storage := range storages {
		status := StatusOK
		if storage.Usage >= opts.StorageCritical {
			status = StatusFail
		} else if storage.Usage >= opts.StorageWarning {
			status = StatusWarn
		}
		if !bool(storage.Active) {
			status = StatusFail
		}

		checks = append(checks, CheckResult{
			Name:    "storage-" + storage.Name,
			Status:  status,
			Message: fmt.Sprintf("%s %.0f%% used", storage.Name, storage.Usage*100),
			Details: map[string]any{
				"type":   storage.Type,
				"used":   storage.Used,
				"total":  storage.Total,
				"usage":  storage.Usage,
				"active": bool(storage.Active),
			},
		})
	}

	return checks
}

func WorstStatus(checks []CheckResult) Status {
	worst := StatusOK
	for _, check := range checks {
		if rank(check.Status) > rank(worst) {
			worst = check.Status
		}
	}
	return worst
}

func ExitCode(status Status) int {
	switch status {
	case StatusOK, StatusInfo:
		return 0
	case StatusWarn:
		return 1
	case StatusFail:
		return 2
	default:
		return 3
	}
}

func rank(status Status) int {
	switch status {
	case StatusOK:
		return 0
	case StatusInfo:
		return 1
	case StatusWarn:
		return 2
	case StatusFail:
		return 3
	default:
		return 4
	}
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}
