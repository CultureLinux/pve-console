package cli

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// takeOption removes "--name value" or "--name=value" from args. It returns the
// value and whether the option was present.
func takeOption(args []string, name string) (string, bool, []string, error) {
	value := ""
	found := false
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == name:
			if i+1 >= len(args) {
				return "", false, nil, fmt.Errorf("%s requires a value", name)
			}
			i++
			value, found = args[i], true
		case strings.HasPrefix(arg, name+"="):
			value, found = strings.TrimPrefix(arg, name+"="), true
		default:
			rest = append(rest, arg)
		}
	}
	return value, found, rest, nil
}

// refuseWithoutYes reports a destructive action that was not confirmed with
// --yes and returns the usage exit code.
func refuseWithoutYes(stderr io.Writer, action string) int {
	fmt.Fprintf(stderr, "Refusing to %s without --yes.\n", action)
	return 2
}

// commandTimeout returns the default timeout of a command. Slow operations get
// the same budget as in the TUI; --timeout overrides it.
func commandTimeout(args []string) time.Duration {
	if len(args) < 2 {
		return defaultTimeout
	}
	switch args[0] + " " + args[1] {
	case "snapshot create":
		return 5 * time.Minute
	case "snapshot rollback", "backup restore", "updates upgrade":
		return 30 * time.Minute
	case "vm start", "vm stop", "vm shutdown", "vm reboot", "snapshot delete", "backup delete":
		return 2 * time.Minute
	}
	return defaultTimeout
}
