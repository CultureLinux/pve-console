package cli

import (
	"fmt"
	"io"
	"time"

	"pvc/internal/version"
)

const usage = `pvm - Proxmox CLI Manager

Usage:
  pvm [global options] <command>
  pvm version

Global options:
  -v, --verbose   Enable verbose output
  --debug         Enable debug output

Commands:
  version   Print version information
`

type Options struct {
	Verbose bool
	Debug   bool
	Timeout time.Duration
}

func Run(args []string, stdout io.Writer, stderr io.Writer) int {
	opts, rest, err := ParseGlobalOptions(args)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n\n", err)
		fmt.Fprint(stderr, usage)
		return 2
	}

	return RunWithOptions(rest, opts, stdout, stderr)
}

func RunWithOptions(args []string, opts Options, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return 0
	}

	switch args[0] {
	case "version", "--version":
		if opts.Debug {
			fmt.Fprintf(stderr, "debug: command=version verbose=%t timeout=%s\n", opts.Verbose, opts.Timeout)
		}
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

func ParseGlobalOptions(args []string) (Options, []string, error) {
	opts := Options{Timeout: 30 * time.Second}
	rest := make([]string, 0, len(args))

	for _, arg := range args {
		switch arg {
		case "-v", "--verbose":
			opts.Verbose = true
		case "--debug":
			opts.Debug = true
			opts.Verbose = true
		default:
			rest = append(rest, arg)
		}
	}

	return opts, rest, nil
}
