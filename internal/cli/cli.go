package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"pvc/internal/command"
	"pvc/internal/config"
	"pvc/internal/health"
	"pvc/internal/platform"
	"pvc/internal/proxmox"
	"pvc/internal/ui"
	"pvc/internal/validate"
	"pvc/internal/version"
)

var isRoot = platform.IsRoot

const usage = `pve-console - Proxmox CLI Manager

Usage:
  pve-console [global options] <command>
  pve-console
  pve-console tui [--demo]
  pve-console version
  pve-console status [--json]
  pve-console vm list [--json]
  pve-console vm status <vmid> [--json]
  pve-console vm start <vmid>
  pve-console vm stop <vmid> --yes
  pve-console vm shutdown <vmid> --yes
  pve-console vm reboot <vmid> --yes
  pve-console storage list [--json]
  pve-console check [--json]
  pve-console snapshot list <vmid> [--json]
  pve-console snapshot create <vmid> [name] [--description text]
  pve-console snapshot delete <vmid> <name> --yes
  pve-console snapshot rollback <vmid> <name> --yes
  pve-console backup list [--json]
  pve-console backup jobs [--json]
  pve-console backup run <vmid> [--storage name]
  pve-console backup delete <volid> --yes
  pve-console backup restore <volid> --yes
  pve-console updates list [--json]
  pve-console updates upgrade --yes
  pve-console user list [--json]
  pve-console tfa list <userid> [--json]
  pve-console tfa disable <userid> --yes
  pve-console task status <upid> [--json]

Global options:
  -v, --verbose     Enable verbose output
  --debug           Enable debug output
  --json            Print JSON where supported
  --timeout <dur>   Timeout per external command, e.g. 90s or 5m (default 30s; longer for slow commands)
  --wait            Wait for the Proxmox task started by the command and report its result
  -y, --yes         Confirm a destructive command (required by stop, shutdown, reboot, delete, rollback, restore, upgrade, tfa disable)

Commands:
  tui            Open interactive terminal UI (--demo: fake data, no node needed)
  version        Print version information
  status         Print node, guest and storage summary
  vm list        List QEMU VMs and LXC containers
  vm status      Print guest status
  storage list   List Proxmox storages
  check          Run health checks
  snapshot       List, create, delete or roll back snapshots
  backup         List backups and jobs, run, delete or restore a backup
  updates        List package updates or run dist-upgrade
  user           List users
  tfa            List or disable a user's TFA entries
  task           Show the status of a Proxmox task
`

const defaultTimeout = 30 * time.Second

type Options struct {
	Verbose bool
	Debug   bool
	JSON    bool
	// Yes confirms destructive commands (--yes).
	Yes bool
	// Wait makes commands that start a Proxmox task wait for its end (--wait).
	Wait    bool
	Timeout time.Duration
	// TimeoutSet is true when the timeout was given with --timeout.
	TimeoutSet bool
}

func Run(args []string, stdout io.Writer, stderr io.Writer) int {
	opts, rest, err := ParseGlobalOptions(args)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n\n", err)
		fmt.Fprint(stderr, usage)
		return 2
	}

	if !opts.TimeoutSet {
		opts.Timeout = commandTimeout(rest)
	}

	runner := command.NewExecRunner(opts.Timeout)
	client := proxmox.NewClient(runner)

	return RunWithClient(rest, opts, client, stdout, stderr)
}

func RunWithOptions(args []string, opts Options, stdout io.Writer, stderr io.Writer) int {
	runner := command.NewExecRunner(opts.Timeout)
	client := proxmox.NewClient(runner)
	return RunWithClient(args, opts, client, stdout, stderr)
}

func RunWithClient(args []string, opts Options, client proxmox.Client, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		return runTUI(client, nil, stderr)
	}

	ctx := context.Background()

	switch args[0] {
	case "version", "--version":
		if opts.Debug {
			fmt.Fprintf(stderr, "debug: command=version verbose=%t timeout=%s\n", opts.Verbose, opts.Timeout)
		}
		fmt.Fprintln(stdout, version.String())
		return 0
	case "tui":
		return runTUI(client, args[1:], stderr)
	case "status":
		return runStatus(ctx, client, opts, stdout, stderr)
	case "vm":
		return runVM(ctx, client, args[1:], opts, stdout, stderr)
	case "storage":
		return runStorage(ctx, client, args[1:], opts, stdout, stderr)
	case "check":
		return runCheck(ctx, client, opts, stdout, stderr)
	case "snapshot":
		return runSnapshot(ctx, client, args[1:], opts, stdout, stderr)
	case "backup":
		return runBackup(ctx, client, args[1:], opts, stdout, stderr)
	case "updates":
		return runUpdates(ctx, client, args[1:], opts, stdout, stderr)
	case "user":
		return runUser(ctx, client, args[1:], opts, stdout, stderr)
	case "tfa":
		return runTFA(ctx, client, args[1:], opts, stdout, stderr)
	case "task":
		return runTask(ctx, client, args[1:], opts, stdout, stderr)
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n\n", args[0])
		fmt.Fprint(stderr, usage)
		return 2
	}
}

func runSnapshot(ctx context.Context, client proxmox.Client, args []string, opts Options, stdout io.Writer, stderr io.Writer) int {
	if len(args) >= 1 && (args[0] == "delete" || args[0] == "rollback") {
		return runSnapshotChange(ctx, client, args[0], args[1:], opts, stdout, stderr)
	}

	if len(args) >= 2 && args[0] == "list" {
		vmid, err := validate.VMID(args[1])
		if err != nil {
			return printError(stderr, "Invalid VMID.", err)
		}
		snapshots, err := client.Snapshots(ctx, vmid)
		if err != nil {
			return printError(stderr, fmt.Sprintf("Unable to list snapshots for guest %d.", vmid), err)
		}
		if opts.JSON {
			return printJSON(stdout, stderr, snapshots)
		}
		printSnapshots(stdout, snapshots)
		return 0
	}

	if len(args) >= 2 && args[0] == "create" {
		vmid, err := validate.VMID(args[1])
		if err != nil {
			return printError(stderr, "Invalid VMID.", err)
		}

		name := ""
		rest := args[2:]
		if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
			name = rest[0]
			rest = rest[1:]
		}
		if name == "" {
			name = "manual-" + time.Now().Format("20060102-150405")
		}
		if err := validate.SnapshotName(name); err != nil {
			return printError(stderr, "Invalid snapshot name.", err)
		}

		description, err := parseDescription(rest)
		if err != nil {
			return printError(stderr, "Invalid snapshot options.", err)
		}

		upid, err := client.CreateSnapshot(ctx, vmid, name, description, false)
		if err != nil {
			return printError(stderr, fmt.Sprintf("Unable to create snapshot for guest %d.", vmid), err)
		}
		task, code := awaitTask(ctx, client, upid, opts, stderr)
		if code != 0 {
			return code
		}

		if opts.JSON {
			payload := struct {
				VMID        int           `json:"vmid"`
				Name        string        `json:"name"`
				Description string        `json:"description,omitempty"`
				UPID        string        `json:"upid,omitempty"`
				Task        *proxmox.Task `json:"task,omitempty"`
			}{VMID: vmid, Name: name, Description: description, UPID: upid, Task: task}
			return printJSON(stdout, stderr, payload)
		}

		fmt.Fprintf(stdout, "Snapshot %q created for guest %d\n", name, vmid)
		printTaskLine(stdout, upid, task)
		return 0
	}

	fmt.Fprint(stderr, usage)
	return 2
}

// runDemoTUI starts the TUI on fake data (a variable so tests do not open a terminal).
var runDemoTUI = ui.RunDemo

func runTUI(client proxmox.Client, args []string, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--demo" {
		// Demo mode needs no root, no node and does not write the config file.
		if err := runDemoTUI(); err != nil {
			return printError(stderr, "Unable to run the demo TUI.", err)
		}
		return 0
	}
	if len(args) != 0 {
		fmt.Fprintf(stderr, "unexpected argument for tui: %s\n\n", strings.Join(args, " "))
		fmt.Fprint(stderr, usage)
		return 2
	}
	if !isRoot() {
		fmt.Fprintln(stderr, "Unable to run TUI: pve-console must be run as root.")
		return 3
	}
	created := initConfig(stderr)
	if err := ui.Run(client, created); err != nil {
		return printError(stderr, "Unable to run TUI.", err)
	}
	return 0
}

// initConfig creates the per-user config file on first run. A failure (for
// example a read-only HOME) is reported as a warning and never blocks the TUI.
func initConfig(stderr io.Writer) bool {
	_, created, err := config.Ensure(version.Version)
	if err != nil {
		fmt.Fprintf(stderr, "warning: configuration not initialized: %v\n", err)
		return false
	}
	return created
}

func runCheck(ctx context.Context, client proxmox.Client, opts Options, stdout io.Writer, stderr io.Writer) int {
	report, err := health.Run(ctx, client, health.DefaultOptions())
	if err != nil {
		return printErrorWithCode(stderr, "Unable to run health checks.", err, 3)
	}

	if opts.JSON {
		if code := printJSON(stdout, stderr, report); code != 0 {
			return 3
		}
		return health.ExitCode(report.Status)
	}

	printHealthReport(stdout, report)
	return health.ExitCode(report.Status)
}

func ParseGlobalOptions(args []string) (Options, []string, error) {
	opts := Options{Timeout: defaultTimeout}
	rest := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-v" || arg == "--verbose":
			opts.Verbose = true
		case arg == "--debug":
			opts.Debug = true
			opts.Verbose = true
		case arg == "--json":
			opts.JSON = true
		case arg == "-y" || arg == "--yes":
			opts.Yes = true
		case arg == "--wait":
			opts.Wait = true
		case arg == "--timeout" || strings.HasPrefix(arg, "--timeout="):
			value := strings.TrimPrefix(arg, "--timeout=")
			if arg == "--timeout" {
				if i+1 >= len(args) {
					return Options{}, nil, fmt.Errorf("--timeout requires a duration such as 90s or 5m")
				}
				i++
				value = args[i]
			}
			timeout, err := time.ParseDuration(value)
			if err != nil || timeout <= 0 {
				return Options{}, nil, fmt.Errorf("invalid --timeout %q: expected a positive duration such as 90s or 5m", value)
			}
			opts.Timeout = timeout
			opts.TimeoutSet = true
		default:
			rest = append(rest, arg)
		}
	}

	return opts, rest, nil
}

func runStatus(ctx context.Context, client proxmox.Client, opts Options, stdout io.Writer, stderr io.Writer) int {
	node, err := client.NodeInfo(ctx)
	if err != nil {
		return printError(stderr, "Unable to detect Proxmox node.", err)
	}

	guests, err := client.Guests(ctx)
	if err != nil {
		return printError(stderr, "Unable to list guests.", err)
	}

	storages, err := client.Storages(ctx)
	if err != nil {
		return printError(stderr, "Unable to list storages.", err)
	}

	payload := struct {
		Node     proxmox.NodeInfo  `json:"node"`
		Guests   []proxmox.Guest   `json:"guests"`
		Storages []proxmox.Storage `json:"storages"`
	}{Node: node, Guests: guests, Storages: storages}

	if opts.JSON {
		return printJSON(stdout, stderr, payload)
	}

	fmt.Fprintf(stdout, "NODE\t%s\n", node.Name)
	if node.ProxmoxVersion != "" {
		fmt.Fprintf(stdout, "PROXMOX\t%s\n", node.ProxmoxVersion)
	}
	if node.Kernel != "" {
		fmt.Fprintf(stdout, "KERNEL\t%s\n", node.Kernel)
	}
	fmt.Fprintln(stdout)
	printGuests(stdout, guests)
	fmt.Fprintln(stdout)
	printStorages(stdout, storages)
	return 0
}

func runVM(ctx context.Context, client proxmox.Client, args []string, opts Options, stdout io.Writer, stderr io.Writer) int {
	if len(args) >= 1 && (args[0] == "start" || args[0] == "stop" || args[0] == "reboot" || args[0] == "shutdown") {
		return runGuestPower(ctx, client, args[0], args[1:], opts, stdout, stderr)
	}

	if len(args) == 1 && args[0] == "list" {
		guests, err := client.Guests(ctx)
		if err != nil {
			return printError(stderr, "Unable to list guests.", err)
		}
		if opts.JSON {
			return printJSON(stdout, stderr, guests)
		}
		printGuests(stdout, guests)
		return 0
	}

	if len(args) == 2 && args[0] == "status" {
		vmid, err := validate.VMID(args[1])
		if err != nil {
			return printError(stderr, "Invalid VMID.", err)
		}
		guest, err := client.GuestStatus(ctx, vmid)
		if err != nil {
			return printError(stderr, fmt.Sprintf("Unable to get status for guest %d.", vmid), err)
		}
		if opts.JSON {
			return printJSON(stdout, stderr, guest)
		}
		printGuest(stdout, guest)
		return 0
	}

	fmt.Fprint(stderr, usage)
	return 2
}

func runStorage(ctx context.Context, client proxmox.Client, args []string, opts Options, stdout io.Writer, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "list" {
		fmt.Fprint(stderr, usage)
		return 2
	}

	storages, err := client.Storages(ctx)
	if err != nil {
		return printError(stderr, "Unable to list storages.", err)
	}
	if opts.JSON {
		return printJSON(stdout, stderr, storages)
	}
	printStorages(stdout, storages)
	return 0
}

func printGuests(w io.Writer, guests []proxmox.Guest) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tTYPE\tSTATUS")
	for _, guest := range guests {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", guest.VMID, guest.Name, guest.Type, guest.Status)
	}
	tw.Flush()
}

func printGuest(w io.Writer, guest proxmox.Guest) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "ID\t%d\n", guest.VMID)
	fmt.Fprintf(tw, "NAME\t%s\n", guest.Name)
	fmt.Fprintf(tw, "TYPE\t%s\n", guest.Type)
	fmt.Fprintf(tw, "STATUS\t%s\n", guest.Status)
	fmt.Fprintf(tw, "CPU\t%.2f\n", guest.CPU)
	fmt.Fprintf(tw, "MEMORY\t%d\n", guest.Memory)
	fmt.Fprintf(tw, "MAXMEM\t%d\n", guest.MaxMem)
	fmt.Fprintf(tw, "UPTIME\t%d\n", guest.Uptime)
	tw.Flush()
}

func printStorages(w io.Writer, storages []proxmox.Storage) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tTYPE\tUSED\tTOTAL\tUSAGE\tACTIVE")
	for _, storage := range storages {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%.0f%%\t%t\n", storage.Name, storage.Type, storage.Used, storage.Total, storage.Usage*100, bool(storage.Active))
	}
	tw.Flush()
}

func printSnapshots(w io.Writer, snapshots []proxmox.Snapshot) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tVMSTATE\tDESCRIPTION")
	for _, snapshot := range snapshots {
		fmt.Fprintf(tw, "%s\t%t\t%s\n", snapshot.Name, bool(snapshot.VMState), snapshot.Description)
	}
	tw.Flush()
}

func parseDescription(args []string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	if len(args) == 2 && args[0] == "--description" {
		return args[1], nil
	}
	return "", fmt.Errorf("expected --description <text>")
}

func printHealthReport(w io.Writer, report health.Report) {
	fmt.Fprintf(w, "NODE %s\n", report.Node)
	fmt.Fprintf(w, "STATUS %s\n\n", strings.ToUpper(string(report.Status)))

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, check := range report.Checks {
		fmt.Fprintf(tw, "[%s]\t%s\t%s\n", strings.ToUpper(string(check.Status)), check.Name, check.Message)
	}
	tw.Flush()
}

func printJSON(stdout io.Writer, stderr io.Writer, value any) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return printError(stderr, "Unable to encode JSON.", err)
	}
	return 0
}

func printError(stderr io.Writer, message string, err error) int {
	return printErrorWithCode(stderr, message, err, 1)
}

func printErrorWithCode(stderr io.Writer, message string, err error, code int) int {
	fmt.Fprintln(stderr, message)
	fmt.Fprintf(stderr, "\n%s\n", err)
	return code
}
