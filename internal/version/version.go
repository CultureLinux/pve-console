package version

import "fmt"

var (
	Name    = "pve-console"
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func String() string {
	if Commit == "none" && Date == "unknown" {
		return fmt.Sprintf("%s %s", Name, Version)
	}

	return fmt.Sprintf("%s %s (%s, %s)", Name, Version, Commit, Date)
}
