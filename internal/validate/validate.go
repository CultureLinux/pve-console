package validate

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	snapshotNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,79}$`)
	storageNamePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)
	vmidListPattern     = regexp.MustCompile(`^[1-9][0-9]*(,[1-9][0-9]*)*$`)
	schedulePattern     = regexp.MustCompile(`^[A-Za-z0-9*][A-Za-z0-9 :,*/.-]{0,99}$`)
	volIDPattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}:[A-Za-z0-9][A-Za-z0-9_./:+-]{0,299}$`)
	upidPattern         = regexp.MustCompile(`^UPID:[A-Za-z0-9][A-Za-z0-9.-]{0,63}:[0-9A-Fa-f]{8}:[0-9A-Fa-f]{8}:[0-9A-Fa-f]{8}:[A-Za-z0-9_-]{1,32}:[A-Za-z0-9_./:-]{0,100}:[A-Za-z0-9_.@!-]{1,64}:$`)
	userIDPattern       = regexp.MustCompile(`^[^\s:/@-][^\s:/@]{0,63}@[A-Za-z][A-Za-z0-9._-]{0,31}$`)
)

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

// StorageName checks a Proxmox storage identifier.
func StorageName(name string) error {
	if !storageNamePattern.MatchString(name) {
		return fmt.Errorf("invalid storage name %q: use letters, numbers, dot, underscore or dash", name)
	}

	return nil
}

// VMIDList checks a comma separated list of VMIDs such as "100,101".
func VMIDList(list string) error {
	if !vmidListPattern.MatchString(list) {
		return fmt.Errorf("invalid VMID list %q: expected positive integers separated by commas", list)
	}

	return nil
}

// Schedule checks a backup schedule (systemd calendar style, e.g. "02:30" or "mon..fri 01:00").
// It must not start with a dash so it cannot be read as a pvesh option.
func Schedule(schedule string) error {
	if !schedulePattern.MatchString(schedule) {
		return fmt.Errorf("invalid schedule %q: use letters, numbers, space and : , * / . -", schedule)
	}

	return nil
}

// VolID checks a Proxmox volume id such as "backup-nas:backup/vzdump-qemu-300.vma.zst".
// It is used in API paths, so it must not contain path traversal.
func VolID(volid string) error {
	if !volIDPattern.MatchString(volid) || strings.Contains(volid, "..") || strings.Contains(volid, "//") {
		return fmt.Errorf("invalid volume id %q: expected <storage>:<path>", volid)
	}

	return nil
}

// UserID checks a Proxmox user id of the form "name@realm".
func UserID(userid string) error {
	if !userIDPattern.MatchString(userid) {
		return fmt.Errorf("invalid user id %q: expected name@realm", userid)
	}

	return nil
}

// UPID checks a Proxmox task id: UPID:<node>:<pid>:<pstart>:<starttime>:<type>:<id>:<user>:
// It is used in API paths, so anything else is refused.
func UPID(upid string) error {
	if !upidPattern.MatchString(upid) || strings.Contains(upid, "..") {
		return fmt.Errorf("invalid task id %q: expected UPID:<node>:<pid>:<pstart>:<start>:<type>:<id>:<user>:", upid)
	}

	return nil
}
