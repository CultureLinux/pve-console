package validate

import (
	"fmt"
	"regexp"
	"strconv"
)

var snapshotNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,79}$`)

func VMID(raw string) (int, error) {
	if raw == "" {
		return 0, fmt.Errorf("VMID is required")
	}

	vmid, err := strconv.Atoi(raw)
	if err != nil || vmid <= 0 {
		return 0, fmt.Errorf("invalid VMID %q: expected a positive integer", raw)
	}

	return vmid, nil
}

func SnapshotName(name string) error {
	if name == "" {
		return fmt.Errorf("snapshot name is required")
	}

	if !snapshotNamePattern.MatchString(name) {
		return fmt.Errorf("invalid snapshot name %q: use 1-80 characters: letters, numbers, dot, underscore or dash", name)
	}

	return nil
}
