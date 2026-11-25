//go:build !windows

package platform

import (
	"os"
	"syscall"
)

// Restart replaces the running process with exe, run with args (args[0] is the
// program name, as in os.Args) and the same environment. It returns only if it
// failed. Call it once the terminal is restored: the new process starts from scratch.
func Restart(exe string, args []string) error {
	return syscall.Exec(exe, args, os.Environ())
}
