package cli

import (
	"fmt"
	"io"

	"pvc/internal/version"
)

const usage = `pvm - Proxmox CLI Manager

Usage:
  pvm version

Commands:
  version   Print version information
`

func Run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return 0
	}

	switch args[0] {
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, version.String())
		return 0
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n\n", args[0])
		fmt.Fprint(stderr, usage)
		return 2
	}
}
