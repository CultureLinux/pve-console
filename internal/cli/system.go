package cli

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"

	"pvc/internal/proxmox"
	"pvc/internal/validate"
)

func runUpdates(ctx context.Context, client proxmox.Client, args []string, opts Options, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "list" {
		updates, err := client.PackageUpdates(ctx)
		if err != nil {
			return printError(stderr, "Unable to list package updates.", err)
		}
		if opts.JSON {
			return printJSON(stdout, stderr, updates)
		}
		printUpdates(stdout, updates)
		return 0
	}

	if len(args) >= 1 && args[0] == "upgrade" {
		yes, rest := opts.Yes, args[1:]
		if len(rest) != 0 {
			fmt.Fprint(stderr, usage)
			return 2
		}
		if !yes {
			return refuseWithoutYes(stderr, "run apt dist-upgrade")
		}
		output, err := client.DistUpgrade(ctx)
		if err != nil {
			if output != "" {
				fmt.Fprintln(stderr, output)
			}
			return printError(stderr, "Unable to run dist-upgrade.", err)
		}
		if opts.JSON {
			return printJSON(stdout, stderr, struct {
				Output string `json:"output"`
			}{Output: output})
		}
		if output != "" {
			fmt.Fprintln(stdout, output)
		}
		return 0
	}

	fmt.Fprint(stderr, usage)
	return 2
}

func runUser(ctx context.Context, client proxmox.Client, args []string, opts Options, stdout io.Writer, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "list" {
		fmt.Fprint(stderr, usage)
		return 2
	}
	users, err := client.Users(ctx)
	if err != nil {
		return printError(stderr, "Unable to list users.", err)
	}
	// Same rule as the TUI: a failing TFA lookup leaves the flag unset.
	for i := range users {
		if entries, err := client.UserTFA(ctx, users[i].UserID); err == nil && len(entries) > 0 {
			users[i].TFAEnabled = true
		}
	}
	if opts.JSON {
		return printJSON(stdout, stderr, users)
	}
	printUsers(stdout, users)
	return 0
}

func runTFA(ctx context.Context, client proxmox.Client, args []string, opts Options, stdout io.Writer, stderr io.Writer) int {
	yes := opts.Yes
	if len(args) != 2 || (args[0] != "list" && args[0] != "disable") {
		fmt.Fprint(stderr, usage)
		return 2
	}
	userid := args[1]
	if err := validate.UserID(userid); err != nil {
		return printError(stderr, "Invalid user.", err)
	}

	if args[0] == "list" {
		entries, err := client.UserTFA(ctx, userid)
		if err != nil {
			return printError(stderr, fmt.Sprintf("Unable to list TFA entries for %s.", userid), err)
		}
		if opts.JSON {
			return printJSON(stdout, stderr, entries)
		}
		printTFAEntries(stdout, entries)
		return 0
	}

	if !yes {
		return refuseWithoutYes(stderr, fmt.Sprintf("disable TFA for %s", userid))
	}
	if err := client.DisableUserTFA(ctx, userid); err != nil {
		return printError(stderr, fmt.Sprintf("Unable to disable TFA for %s.", userid), err)
	}
	if opts.JSON {
		return printJSON(stdout, stderr, struct {
			UserID string `json:"userid"`
			Action string `json:"action"`
		}{UserID: userid, Action: "disable-tfa"})
	}
	fmt.Fprintf(stdout, "TFA disabled for %s\n", userid)
	return 0
}

func printUpdates(w io.Writer, updates []proxmox.PackageUpdate) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tCURRENT\tCANDIDATE\tSUITE")
	for _, update := range updates {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", update.Name, update.Current, update.Candidate, update.Suite)
	}
	tw.Flush()
}

func printUsers(w io.Writer, users []proxmox.User) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "USERID\tENABLED\tTFA\tEMAIL")
	for _, user := range users {
		fmt.Fprintf(tw, "%s\t%t\t%t\t%s\n", user.UserID, bool(user.Enable), user.TFAEnabled, user.Email)
	}
	tw.Flush()
}

func printTFAEntries(w io.Writer, entries []proxmox.TFAEntry) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTYPE\tDESCRIPTION")
	for _, entry := range entries {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", entry.ID, entry.Type, entry.Description)
	}
	tw.Flush()
}
