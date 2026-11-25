package proxmox

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"pvc/internal/command"
)

// byGuestType runs try for a QEMU VM and, if that fails, for an LXC container.
// The VMID does not tell which one it is, so one of the two attempts is expected
// to fail with "does not exist". When it fails for another reason (a refused
// snapshot name, a locked VM...), that real error is returned: reporting "not
// found" instead would hide what Proxmox said.
func byGuestType[T any](notFound error, try func(GuestType) (T, error)) (T, error) {
	value, qemuErr := try(GuestTypeQEMU)
	if qemuErr == nil {
		return value, nil
	}
	value, lxcErr := try(GuestTypeLXC)
	if lxcErr == nil {
		return value, nil
	}

	var zero T
	qemuReal, lxcReal := isRealError(qemuErr), isRealError(lxcErr)
	switch {
	case qemuReal && !lxcReal:
		return zero, qemuErr
	case lxcReal && !qemuReal:
		return zero, lxcErr
	case qemuReal && lxcReal:
		if qemuErr.Error() == lxcErr.Error() {
			return zero, qemuErr
		}
		return zero, errors.Join(qemuErr, lxcErr)
	}
	return zero, notFound
}

// guestNotFound is the error for a VMID that is neither a QEMU VM nor an LXC container.
func guestNotFound(vmid int) error {
	return fmt.Errorf("guest %d was not found as QEMU VM or LXC container", vmid)
}

// isRealError tells a failure worth reporting from the expected "that guest
// type does not exist" one. A timeout, a missing program or an unreadable
// answer are real. A command that failed is real when it said something other
// than "does not exist"; one that failed without a word counts as "does not exist".
func isRealError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.Is(err, exec.ErrNotFound) {
		return true
	}
	var commandErr command.CommandError
	if !errors.As(err, &commandErr) {
		return true
	}
	stderr := strings.ToLower(strings.TrimSpace(string(commandErr.Result.Stderr)))
	if stderr == "" {
		return false
	}
	for _, missing := range []string{"does not exist", "not found", "no such"} {
		if strings.Contains(stderr, missing) {
			return false
		}
	}
	return true
}

// resolveGuestType finds out whether vmid is a QEMU VM or an LXC container with
// a read-only probe, before any action. Actions must not use byGuestType: Proxmox
// answers an LXC action on a missing container with a task that fails later, so
// the fallback would "succeed" and hide the real QEMU error.
func (c Client) resolveGuestType(ctx context.Context, node string, vmid int) (GuestType, error) {
	guest, err := byGuestType(guestNotFound(vmid), func(typ GuestType) (Guest, error) {
		return c.guestStatusByType(ctx, node, vmid, typ)
	})
	if err != nil {
		return "", err
	}
	return guest.Type, nil
}
