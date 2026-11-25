package cli

import (
	"context"
	"fmt"
	"io"

	"pvc/internal/proxmox"
	"pvc/internal/validate"
)

// runGuestPower starts, stops, reboots or shuts down a guest. Everything except
// start interrupts a running guest, so it needs --yes.
func runGuestPower(ctx context.Context, client proxmox.Client, action string, args []string, opts Options, stdout io.Writer, stderr io.Writer) int {
	yes := opts.Yes
	if len(args) != 1 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	vmid, err := validate.VMID(args[0])
	if err != nil {
		return printError(stderr, "Invalid VMID.", err)
	}
	if action != "start" && !yes {
		return refuseWithoutYes(stderr, fmt.Sprintf("%s guest %d", action, vmid))
	}

	var upid string
	switch action {
	case "start":
		upid, err = client.StartGuest(ctx, vmid)
	case "stop":
		upid, err = client.StopGuest(ctx, vmid)
	case "reboot":
		upid, err = client.RebootGuest(ctx, vmid)
	case "shutdown":
		upid, err = client.ShutdownGuest(ctx, vmid)
	default:
		fmt.Fprint(stderr, usage)
		return 2
	}
	if err != nil {
		return printError(stderr, fmt.Sprintf("Unable to %s guest %d.", action, vmid), err)
	}
	task, code := awaitTask(ctx, client, upid, opts, stderr)
	if code != 0 {
		return code
	}

	if opts.JSON {
		return printJSON(stdout, stderr, struct {
			VMID   int           `json:"vmid"`
			Action string        `json:"action"`
			Status string        `json:"status"`
			UPID   string        `json:"upid,omitempty"`
			Task   *proxmox.Task `json:"task,omitempty"`
		}{VMID: vmid, Action: action, Status: doneWord(task), UPID: upid, Task: task})
	}
	fmt.Fprintf(stdout, "Guest %d: %s %s\n", vmid, action, doneWord(task))
	printTaskLine(stdout, upid, task)
	return 0
}

// runSnapshotChange deletes a snapshot or rolls a guest back to it. Both are destructive.
func runSnapshotChange(ctx context.Context, client proxmox.Client, action string, args []string, opts Options, stdout io.Writer, stderr io.Writer) int {
	yes := opts.Yes
	if len(args) != 2 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	vmid, err := validate.VMID(args[0])
	if err != nil {
		return printError(stderr, "Invalid VMID.", err)
	}
	name := args[1]
	if err := validate.SnapshotName(name); err != nil {
		return printError(stderr, "Invalid snapshot name.", err)
	}
	if !yes {
		return refuseWithoutYes(stderr, fmt.Sprintf("%s snapshot %q of guest %d", action, name, vmid))
	}

	var upid string
	if action == "delete" {
		upid, err = client.DeleteSnapshot(ctx, vmid, name)
	} else {
		upid, err = client.RollbackSnapshot(ctx, vmid, name)
	}
	if err != nil {
		return printError(stderr, fmt.Sprintf("Unable to %s snapshot %q of guest %d.", action, name, vmid), err)
	}
	task, code := awaitTask(ctx, client, upid, opts, stderr)
	if code != 0 {
		return code
	}

	if opts.JSON {
		return printJSON(stdout, stderr, struct {
			VMID   int           `json:"vmid"`
			Name   string        `json:"name"`
			Action string        `json:"action"`
			UPID   string        `json:"upid,omitempty"`
			Task   *proxmox.Task `json:"task,omitempty"`
		}{VMID: vmid, Name: name, Action: action, UPID: upid, Task: task})
	}
	if action == "delete" {
		fmt.Fprintf(stdout, "Snapshot %q deleted for guest %d\n", name, vmid)
	} else {
		fmt.Fprintf(stdout, "Guest %d rolled back to snapshot %q\n", vmid, name)
	}
	printTaskLine(stdout, upid, task)
	return 0
}
