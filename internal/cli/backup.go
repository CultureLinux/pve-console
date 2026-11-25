package cli

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"pvc/internal/proxmox"
	"pvc/internal/validate"
)

func runBackup(ctx context.Context, client proxmox.Client, args []string, opts Options, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	switch args[0] {
	case "list":
		if len(args) != 1 {
			break
		}
		storages, err := client.Storages(ctx)
		if err != nil {
			return printError(stderr, "Unable to list storages.", err)
		}
		volumes, err := client.BackupVolumes(ctx, storages)
		if err != nil {
			return printError(stderr, "Unable to list backups.", err)
		}
		if opts.JSON {
			return printJSON(stdout, stderr, volumes)
		}
		printBackupVolumes(stdout, volumes)
		return 0
	case "jobs":
		if len(args) != 1 {
			break
		}
		jobs, err := client.BackupJobs(ctx)
		if err != nil {
			return printError(stderr, "Unable to list backup jobs.", err)
		}
		if opts.JSON {
			return printJSON(stdout, stderr, jobs)
		}
		printBackupJobs(stdout, jobs)
		return 0
	case "run":
		return runBackupRun(ctx, client, args[1:], opts, stdout, stderr)
	case "delete", "restore":
		return runBackupVolumeChange(ctx, client, args[0], args[1:], opts, stdout, stderr)
	}

	fmt.Fprint(stderr, usage)
	return 2
}

// runBackupRun starts a vzdump task. The task keeps running after the command returns.
func runBackupRun(ctx context.Context, client proxmox.Client, args []string, opts Options, stdout io.Writer, stderr io.Writer) int {
	storage, _, args, err := takeOption(args, "--storage")
	if err != nil {
		return printError(stderr, "Invalid backup options.", err)
	}
	if len(args) != 1 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	vmid, err := validate.VMID(args[0])
	if err != nil {
		return printError(stderr, "Invalid VMID.", err)
	}
	if storage != "" {
		if err := validate.StorageName(storage); err != nil {
			return printError(stderr, "Invalid storage.", err)
		}
	}

	upid, err := client.RunBackup(ctx, vmid, storage)
	if err != nil {
		return printError(stderr, fmt.Sprintf("Unable to start backup for guest %d.", vmid), err)
	}
	task, code := awaitTask(ctx, client, upid, opts, stderr)
	if code != 0 {
		return code
	}

	if opts.JSON {
		return printJSON(stdout, stderr, struct {
			VMID    int           `json:"vmid"`
			Storage string        `json:"storage,omitempty"`
			UPID    string        `json:"upid,omitempty"`
			Task    *proxmox.Task `json:"task,omitempty"`
		}{VMID: vmid, Storage: storage, UPID: upid, Task: task})
	}
	if task != nil {
		fmt.Fprintf(stdout, "Backup completed for guest %d\n", vmid)
	} else {
		fmt.Fprintf(stdout, "Backup started for guest %d (running in background)\n", vmid)
	}
	printTaskLine(stdout, upid, task)
	return 0
}

// runBackupVolumeChange deletes a backup or restores it over its VMID. Both need --yes.
func runBackupVolumeChange(ctx context.Context, client proxmox.Client, action string, args []string, opts Options, stdout io.Writer, stderr io.Writer) int {
	yes := opts.Yes
	if len(args) != 1 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	volid := args[0]
	if err := validate.VolID(volid); err != nil {
		return printError(stderr, "Invalid backup volume.", err)
	}
	if !yes {
		return refuseWithoutYes(stderr, fmt.Sprintf("%s backup %s", action, volid))
	}

	var err error
	vmid := 0
	upid := ""
	if action == "delete" {
		upid, err = client.DeleteBackupVolume(ctx, volid)
	} else {
		var volume proxmox.BackupVolume
		volume, err = findBackupVolume(ctx, client, volid)
		if err == nil {
			vmid = volume.VMID
			err = client.RestoreBackupVolume(ctx, volume)
		}
	}
	if err != nil {
		return printError(stderr, fmt.Sprintf("Unable to %s backup %s.", action, volid), err)
	}

	task, code := awaitTask(ctx, client, upid, opts, stderr)
	if code != 0 {
		return code
	}

	if opts.JSON {
		return printJSON(stdout, stderr, struct {
			VolID  string        `json:"volid"`
			VMID   int           `json:"vmid,omitempty"`
			Action string        `json:"action"`
			UPID   string        `json:"upid,omitempty"`
			Task   *proxmox.Task `json:"task,omitempty"`
		}{VolID: volid, VMID: vmid, Action: action, UPID: upid, Task: task})
	}
	if action == "delete" {
		fmt.Fprintf(stdout, "Backup %s deleted\n", volid)
	} else {
		fmt.Fprintf(stdout, "Backup %s restored to guest %d\n", volid, vmid)
	}
	printTaskLine(stdout, upid, task)
	return 0
}

func findBackupVolume(ctx context.Context, client proxmox.Client, volid string) (proxmox.BackupVolume, error) {
	storages, err := client.Storages(ctx)
	if err != nil {
		return proxmox.BackupVolume{}, err
	}
	volumes, err := client.BackupVolumes(ctx, storages)
	if err != nil {
		return proxmox.BackupVolume{}, err
	}
	for _, volume := range volumes {
		if volume.VolID == volid {
			return volume, nil
		}
	}
	return proxmox.BackupVolume{}, fmt.Errorf("backup %s not found", volid)
}

func printBackupVolumes(w io.Writer, volumes []proxmox.BackupVolume) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "VOLID\tVMID\tFORMAT\tSIZE\tCREATED")
	for _, volume := range volumes {
		created := "-"
		if volume.CTime > 0 {
			created = time.Unix(volume.CTime, 0).UTC().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(tw, "%s\t%d\t%s\t%d\t%s\n", volume.VolID, volume.VMID, volume.Format, volume.Size, created)
	}
	tw.Flush()
}

func printBackupJobs(w io.Writer, jobs []proxmox.BackupJob) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSTORAGE\tSCHEDULE\tENABLED\tVMID\tMODE")
	for _, job := range jobs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%t\t%s\t%s\n", job.ID, job.Storage, job.Schedule, bool(job.Enabled), job.VMID, job.Mode)
	}
	tw.Flush()
}
