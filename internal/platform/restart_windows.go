//go:build windows

package platform

import "errors"

// Restart is not available on Windows: pve-console only runs on a Proxmox node.
func Restart(exe string, args []string) error {
	return errors.New("restarting in place is not supported on this platform")
}
