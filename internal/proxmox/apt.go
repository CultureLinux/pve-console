package proxmox

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"pvc/internal/command"
)

// rebootRequiredFile is created by Debian's update-notifier-common when a
// package asks for a reboot. Proxmox does not always install it.
const rebootRequiredFile = "/var/run/reboot-required"

// UpdateIndex refreshes the package index (apt-get update). onLine, if set,
// receives the output line by line while the command runs.
func (c Client) UpdateIndex(ctx context.Context, onLine func(string)) (string, error) {
	output, err := c.runApt(ctx, onLine, "update")
	if err != nil {
		return output, fmt.Errorf("unable to refresh the package index: %w", err)
	}
	return output, nil
}

// DistUpgrade runs apt dist-upgrade and returns its output.
func (c Client) DistUpgrade(ctx context.Context) (string, error) {
	return c.DistUpgradeWith(ctx, nil)
}

// DistUpgradeWith is DistUpgrade, with the output reported line by line while
// the command runs when onLine is set and the runner supports it.
func (c Client) DistUpgradeWith(ctx context.Context, onLine func(string)) (string, error) {
	output, err := c.runApt(ctx, onLine, "-y", "-o", "Dpkg::Options::=--force-confold", "dist-upgrade")
	if err != nil {
		return output, fmt.Errorf("unable to run apt dist-upgrade: %w", err)
	}
	return output, nil
}

// runApt runs apt-get with the given arguments and returns what it printed.
func (c Client) runApt(ctx context.Context, onLine func(string), args ...string) (string, error) {
	// env is used because Runner has no environment support: without a
	// non-interactive frontend and an explicit conffile policy, dpkg may ask
	// questions nobody can answer.
	full := append([]string{"DEBIAN_FRONTEND=noninteractive", "apt-get"}, args...)

	if lines, ok := c.Runner.(command.LineRunner); ok && onLine != nil {
		// Both outputs are merged in Stdout, in the order they were written.
		result, err := lines.RunLines(ctx, onLine, "env", full...)
		return strings.TrimSpace(string(result.Stdout)), err
	}

	result, err := c.Runner.Run(ctx, "env", full...)
	output := strings.TrimSpace(string(result.Stdout))
	if stderr := strings.TrimSpace(string(result.Stderr)); stderr != "" {
		if output != "" {
			output += "\n"
		}
		output += stderr
	}
	if onLine != nil && output != "" {
		for _, line := range strings.Split(output, "\n") {
			onLine(strings.TrimRight(line, "\r"))
		}
	}
	return output, err
}

// kernelSetup matches the line apt prints when it configures a Proxmox kernel
// package, and captures its release: "Setting up proxmox-kernel-6.14.8-3-pve-signed (6.14.8-3) ...".
var kernelSetup = regexp.MustCompile(`(?m)^Setting up (?:proxmox|pve)-kernel-([0-9][0-9A-Za-z.+~_-]*-pve)(?:-signed)? `)

// installedKernels lists the kernel releases an apt run configured, each once.
func installedKernels(output string) []string {
	var releases []string
	seen := map[string]bool{}
	for _, match := range kernelSetup.FindAllStringSubmatch(output, -1) {
		if !seen[match[1]] {
			seen[match[1]] = true
			releases = append(releases, match[1])
		}
	}
	return releases
}

// RebootNeeded tells why the node should be rebooted after an upgrade whose
// output is given, or returns "" when nothing says so. It looks at two signs:
// the reboot-required flag file, and a new kernel that is not the running one.
// A sign that cannot be read counts as absent: no warning is better than a
// false one.
func (c Client) RebootNeeded(ctx context.Context, upgradeOutput string) string {
	var reasons []string

	if _, err := c.Runner.Run(ctx, "test", "-e", rebootRequiredFile); err == nil {
		reasons = append(reasons, "the system asks for it ("+rebootRequiredFile+")")
	}

	if kernels := installedKernels(upgradeOutput); len(kernels) > 0 {
		if result, err := c.Runner.Run(ctx, "uname", "-r"); err == nil {
			running := strings.TrimSpace(string(result.Stdout))
			for _, release := range kernels {
				if running != "" && release != running {
					reasons = append(reasons, fmt.Sprintf("kernel %s installed, kernel %s running", release, running))
				}
			}
		}
	}
	return strings.Join(reasons, "; ")
}
