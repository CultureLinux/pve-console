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
	"pvc/internal/health"
	"pvc/internal/proxmox"
	"pvc/internal/ui"
	"pvc/internal/validate"
	"pvc/internal/version"
)

const usage = `pvm - Proxmox CLI Manager

Usage:
  pvm [global options] <command>
  pvm
  pvm version
  pvm status [--json]
  pvm vm list [--json]
  pvm vm status <vmid> [--json]
  pvm storage list [--json]
  pvm check [--json]
  pvm snapshot list <vmid> [--json]
  pvm snapshot create <vmid> [name] [--description text]

Global options:
  -v, --verbose   Enable verbose output
  --debug         Enable debug output

Commands:
  tui            Open interactive terminal UI
  version        Print version information
  status         Print node, guest and storage summary
  vm list        List QEMU VMs and LXC containers
  vm status      Print guest status
  storage list   List Proxmox storages
  check          Run health checks
  snapshot       List or create snapshots
`

type Options struct {
	Verbose bool
	Debug   bool
	JSON    bool
	Timeout time.Duration
}

func Run(args []string, stdout io.Writer, stderr io.Writer) int {
	opts, rest, err := ParseGlobalOptions(args)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n\n", err)
		fmt.Fprint(stderr, usage)
		return 2
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
		return runTUI(client, stderr)
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
		return runTUI(client, stderr)
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

		if err := client.CreateSnapshot(ctx, vmid, name, description, false); err != nil {
			return printError(stderr, fmt.Sprintf("Unable to create snapshot for guest %d.", vmid), err)
		}

		if opts.JSON {
			payload := struct {
				VMID        int    `json:"vmid"`
				Name        string `json:"name"`
				Description string `json:"description,omitempty"`
			}{VMID: vmid, Name: name, Description: description}
			return printJSON(stdout, stderr, payload)
		}

		fmt.Fprintf(stdout, "Snapshot %q created for guest %d\n", name, vmid)
		return 0
	}

	fmt.Fprint(stderr, usage)
	return 2
}

func runTUI(client proxmox.Client, stderr io.Writer) int {
	if err := ui.Run(client); err != nil {
		return printError(stderr, "Unable to run TUI.", err)
	}
	return 0
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
	opts := Options{Timeout: 30 * time.Second}
	rest := make([]string, 0, len(args))

	for _, arg := range args {
		switch arg {
		case "-v", "--verbose":
			opts.Verbose = true
		case "--debug":
			opts.Debug = true
			opts.Verbose = true
		case "--json":
			opts.JSON = true
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
