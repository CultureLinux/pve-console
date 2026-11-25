package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"pvc/internal/command"
	"pvc/internal/proxmox"
	"pvc/internal/update"
	"pvc/internal/version"
)

// DemoClient returns a client wired to a fake Proxmox node, so the TUI can be
// shown and tried without a node or root rights. Actions work: starting or
// stopping a guest changes its status, tasks run for a few polls and succeed.
func DemoClient() proxmox.Client {
	return proxmox.Client{Runner: newDemoRunner(), Node: "pve"}
}

type demoGuest struct {
	VMID   int    `json:"vmid"`
	Name   string `json:"name"`
	Status string `json:"status"`
	// CPU is a ratio (0.12 = 12 %), Memory and MaxMem are bytes.
	CPU    float64 `json:"cpu"`
	Memory uint64  `json:"mem"`
	MaxMem uint64  `json:"maxmem"`
	Uptime int64   `json:"uptime"`
	lxc    bool
}

// demoAptUPID is the failed package index update of the demo's task panel.
const demoAptUPID = "UPID:pve:0000A1B2:00C4D5E6:67890ABC:aptupdate::root@pam:"

// demoRunner answers the commands of the client with canned data.
type demoRunner struct {
	mu     sync.Mutex
	guests []*demoGuest
	polls  map[string]int
	tasks  int
	// upgraded is true once the demo ran apt dist-upgrade: nothing is left to upgrade.
	upgraded bool
	// failing lists the tasks that end in error (a backup of guest 103).
	failing map[string]bool
	// enrolled lists the factors added during the demo, by user.
	enrolled map[string][]map[string]any
}

func newDemoRunner() *demoRunner {
	const gib = 1 << 30
	return &demoRunner{
		polls:    map[string]int{},
		failing:  map[string]bool{},
		enrolled: map[string][]map[string]any{},
		guests: []*demoGuest{
			{VMID: 100, Name: "wordpress", Status: "running", CPU: 0.12, Memory: 1 * gib, MaxMem: 4 * gib, Uptime: 3*86400 + 7200},
			{VMID: 101, Name: "postgres-primary", Status: "running", CPU: 0.61, Memory: 6 * gib, MaxMem: 8 * gib, Uptime: 12*86400 + 3600},
			{VMID: 102, Name: "minio", Status: "stopped", MaxMem: 2 * gib},
			{VMID: 103, Name: "win11-dev", Status: "stopped", MaxMem: 8 * gib},
			{VMID: 200, Name: "nginx-proxy", Status: "running", CPU: 0.02, Memory: 210 << 20, MaxMem: gib / 2, Uptime: 5400, lxc: true},
			{VMID: 201, Name: "pihole", Status: "running", CPU: 0.01, Memory: 96 << 20, MaxMem: gib / 4, Uptime: 86400 * 21, lxc: true},
			{VMID: 202, Name: "test-ct", Status: "stopped", MaxMem: gib / 2, lxc: true},
		},
	}
}

func (r *demoRunner) find(vmid int) *demoGuest {
	for _, guest := range r.guests {
		if guest.VMID == vmid {
			return guest
		}
	}
	return nil
}

func demoFail(name string, args []string) error {
	return command.CommandError{
		Result: command.Result{Command: name, ExitCode: 255, Stderr: []byte("demo: not available")},
		Err:    fmt.Errorf("demo: %s %s", name, strings.Join(args, " ")),
	}
}

// demoRefusal is a command that Proxmox refuses, with what it printed.
func demoRefusal(name string, args []string, stderr string) error {
	return command.CommandError{
		Result: command.Result{Command: name, Args: args, ExitCode: 255, Stderr: []byte(stderr), Duration: 1200 * time.Millisecond},
		Err:    fmt.Errorf("exit status 255"),
	}
}

// demoMissing is what Proxmox says when a VMID is not of the guest type of the
// path: the client tries QEMU then LXC, so one of the two always says this.
func demoMissing(args []string, parts []string) error {
	dir := "qemu-server"
	if len(parts) > 2 && parts[2] == "lxc" {
		dir = "lxc"
	}
	id := ""
	if len(parts) > 3 {
		id = parts[3]
	}
	return demoRefusal("pvesh", args, "Configuration file 'nodes/pve/"+dir+"/"+id+".conf' does not exist")
}

func demoJSON(value any) (command.Result, error) {
	data, err := json.Marshal(value)
	return command.Result{Stdout: data}, err
}

// Run implements command.Runner.
func (r *demoRunner) Run(ctx context.Context, name string, args ...string) (command.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch name {
	case "pveversion":
		return command.Result{Stdout: []byte("pve-manager/9.0.3/6e5b4b1a (running kernel: 6.14.8-2-pve)\n")}, nil
	case "uname":
		return command.Result{Stdout: []byte("6.14.8-2-pve\n")}, nil
	case "apt":
		if r.upgraded {
			return command.Result{Stdout: []byte("Listing...\n")}, nil
		}
		return command.Result{Stdout: []byte("Listing...\n" +
			"libssl3/stable-security 3.0.16-1 amd64 [upgradable from: 3.0.15-1]\n" +
			"pve-kernel-6.14/stable 6.14.8-3 all [upgradable from: 6.14.8-2]\n" +
			"qemu-server/stable 9.0.4 amd64 [upgradable from: 9.0.3]\n")}, nil
	case "env", "apt-get":
		if contains(args, "update") {
			return command.Result{Stdout: []byte("Hit:1 http://deb.debian.org/debian trixie InRelease\nGet:2 http://security.debian.org/debian-security trixie-security InRelease [48.0 kB]\nHit:3 http://download.proxmox.com/debian/pve trixie InRelease\nFetched 48.0 kB in 1s (52.1 kB/s)\nReading package lists... Done\n")}, nil
		}
		r.upgraded = true
		return command.Result{Stdout: []byte("Reading package lists... Done\nCalculating upgrade... Done\n3 upgraded, 0 newly installed.\nUnpacking libssl3 (3.0.16-1) over (3.0.15-1) ...\nUnpacking proxmox-kernel-6.14.8-3-pve-signed (6.14.8-3) over (6.14.8-2) ...\nUnpacking qemu-server (9.0.4) over (9.0.3) ...\nSetting up libssl3 (3.0.16-1) ...\nSetting up proxmox-kernel-6.14.8-3-pve-signed (6.14.8-3) ...\nSetting up qemu-server (9.0.4) ...\n")}, nil
	case "test":
		// No /var/run/reboot-required: the demo's reboot comes from the new kernel.
		return command.Result{}, demoRefusal(name, args, "")
	case "qmrestore", "pct", "pveum":
		return command.Result{}, nil
	case "pvesh":
		return r.pvesh(args)
	}
	return command.Result{}, demoFail(name, args)
}

// RunLines implements command.LineRunner: the output of a command comes line
// by line, a little slowly, so the demo shows an upgrade as it happens.
func (r *demoRunner) RunLines(ctx context.Context, onLine func(string), name string, args ...string) (command.Result, error) {
	result, err := r.Run(ctx, name, args...)
	for _, line := range strings.Split(strings.TrimRight(string(result.Stdout), "\n"), "\n") {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(demoLineDelay):
		}
		onLine(line)
	}
	return result, err
}

// demoLineDelay is the pause between two lines of a streamed demo command.
var demoLineDelay = 150 * time.Millisecond

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func (r *demoRunner) upid(kind string, vmid string) command.Result {
	r.tasks++
	upid := fmt.Sprintf("UPID:pve:%08X:00C4D5E6:67890ABC:%s:%s:root@pam:", 0x1000+r.tasks, kind, vmid)
	return command.Result{Stdout: []byte(`"` + upid + `"` + "\n")}
}

func (r *demoRunner) pvesh(args []string) (command.Result, error) {
	if len(args) < 2 {
		return command.Result{}, demoFail("pvesh", args)
	}
	verb, path := args[0], args[1]
	parts := strings.Split(strings.Trim(path, "/"), "/")

	switch verb {
	case "get":
		return r.get(path, parts, args)
	case "create":
		return r.create(path, parts, args)
	case "delete":
		return r.upid("demo", ""), nil
	case "set":
		if path == "/access/password" && (option(args, "-userid") == "" || option(args, "-password") == "") {
			return command.Result{}, demoRefusal("pvesh", args, "400 Parameter verification failed.\npassword: property is missing and it is not optional")
		}
		return command.Result{}, nil
	}
	return command.Result{}, demoFail("pvesh", args)
}

func (r *demoRunner) get(path string, parts []string, args []string) (command.Result, error) {
	switch {
	case path == "/nodes/pve/qemu" || path == "/nodes/pve/lxc":
		list := []*demoGuest{}
		for _, guest := range r.guests {
			if guest.lxc == (path == "/nodes/pve/lxc") {
				list = append(list, guest)
			}
		}
		return demoJSON(list)
	case path == "/cluster/resources":
		resources := []map[string]any{}
		for _, guest := range r.guests {
			kind := "qemu"
			if guest.lxc {
				kind = "lxc"
			}
			resources = append(resources, map[string]any{"id": fmt.Sprintf("%s/%d", kind, guest.VMID), "type": kind, "vmid": guest.VMID, "node": "pve", "cpu": guest.CPU})
		}
		return demoJSON(resources)
	case path == "/nodes/pve/storage":
		return demoJSON([]map[string]any{
			{"storage": "local", "type": "dir", "used": 21 << 30, "total": 94 << 30, "enabled": 1, "active": 1},
			{"storage": "local-lvm", "type": "lvmthin", "used": 180 << 30, "total": 250 << 30, "enabled": 1, "active": 1},
			{"storage": "backup-nas", "type": "nfs", "used": 3700 << 30, "total": 4000 << 30, "enabled": 1, "active": 1},
		})
	case path == "/cluster/backup":
		return demoJSON([]map[string]any{
			{"id": "backup-daily", "storage": "backup-nas", "schedule": "02:30", "enabled": 1, "vmid": "100,101,200", "mode": "snapshot", "compress": "zstd"},
			{"id": "backup-weekly", "storage": "backup-nas", "schedule": "sun 04:00", "enabled": 0, "vmid": "102,103", "mode": "stop"},
		})
	case path == "/cluster/tasks":
		// What the web interface shows in its task panel: a package index update
		// that failed, a backup that worked, and a task still running.
		now := time.Now().Unix()
		return demoJSON([]map[string]any{
			{"upid": demoAptUPID, "node": "pve", "type": "aptupdate", "user": "root@pam", "starttime": now - 7260, "endtime": now - 7255, "status": "command 'apt-get update' failed: exit code 100"},
			{"upid": "UPID:pve:0000A1B3:00C4D5E6:67890ABD:vzdump:100:root@pam:", "node": "pve", "type": "vzdump", "id": "100", "user": "root@pam", "starttime": now - 3600, "endtime": now - 3500, "status": "OK"},
			{"upid": "UPID:pve:0000A1B4:00C4D5E6:67890ABE:vncproxy:101:root@pam:", "node": "pve", "type": "vncproxy", "id": "101", "user": "root@pam", "starttime": now - 60},
		})
	case path == "/access/users":
		return demoJSON([]map[string]any{
			{"userid": "root@pam", "enable": 1, "email": "admin@example.test", "comment": "Administrator"},
			{"userid": "alice@pve", "enable": 1, "email": "alice@example.test", "firstname": "Alice", "lastname": "Martin"},
			{"userid": "backup@pve", "enable": 0, "comment": "service account"},
		})
	case len(parts) == 4 && parts[0] == "access" && parts[2] != "" && parts[3] == "tfa":
		entries := []map[string]any{}
		if parts[2] == "root@pam" {
			entries = append(entries, map[string]any{"id": "1", "type": "totp", "description": "phone", "created": 1700000000})
		}
		return demoJSON(append(entries, r.enrolled[parts[2]]...))
	case len(parts) == 5 && parts[2] == "storage" && parts[4] == "content":
		if parts[3] != "backup-nas" {
			return demoJSON([]map[string]any{})
		}
		return demoJSON([]map[string]any{
			{"volid": "backup-nas:backup/vzdump-qemu-100-2026_01_08-02_30_00.vma.zst", "vmid": 100, "format": "vma.zst", "size": 3 << 30, "ctime": 1767839400},
			{"volid": "backup-nas:backup/vzdump-qemu-101-2026_01_08-02_41_10.vma.zst", "vmid": 101, "format": "vma.zst", "size": 14 << 30, "ctime": 1767840070},
			{"volid": "backup-nas:backup/vzdump-lxc-200-2026_01_08-02_58_00.tar.zst", "vmid": 200, "format": "tar.zst", "size": 600 << 20, "ctime": 1767841080},
		})
	case len(parts) == 5 && parts[2] == "tasks" && parts[4] == "status":
		upid := parts[3]
		r.polls[upid]++
		if r.polls[upid] < 3 {
			return demoJSON(map[string]any{"status": "running", "type": "demo", "user": "root@pam"})
		}
		if r.failing[upid] {
			return demoJSON(map[string]any{"status": "stopped", "exitstatus": "job errors", "type": "vzdump", "user": "root@pam"})
		}
		return demoJSON(map[string]any{"status": "stopped", "exitstatus": "OK", "type": "demo", "user": "root@pam"})
	case len(parts) == 5 && parts[2] == "tasks" && parts[4] == "log" && parts[3] == demoAptUPID:
		return demoJSON([]map[string]any{
			{"n": 1, "t": "Err:1 http://download.proxmox.com/debian/pve trixie InRelease"},
			{"n": 2, "t": "  401  Unauthorized [IP: 203.0.113.10 80]"},
			{"n": 3, "t": "E: Failed to fetch http://download.proxmox.com/debian/pve/dists/trixie/InRelease  401  Unauthorized"},
			{"n": 4, "t": "TASK ERROR: command 'apt-get update' failed: exit code 100"},
		})
	case len(parts) == 5 && parts[2] == "tasks" && parts[4] == "log":
		if r.failing[parts[3]] {
			return demoJSON([]map[string]any{
				{"n": 1, "t": "INFO: starting new backup job: vzdump 103 --storage backup-nas --mode snapshot"},
				{"n": 2, "t": "INFO: Starting Backup of VM 103 (qemu)"},
				{"n": 3, "t": "ERROR: Backup of VM 103 failed - no such logical volume pve/vm-103-disk-0"},
				{"n": 4, "t": "INFO: Backup job finished with errors"},
				{"n": 5, "t": "TASK ERROR: job errors"},
			})
		}
		return demoJSON([]map[string]any{{"n": 1, "t": "demo task"}})
	}

	// /nodes/pve/{qemu|lxc}/<vmid>/{status/current|config|snapshot}
	if len(parts) >= 5 && parts[0] == "nodes" && (parts[2] == "qemu" || parts[2] == "lxc") {
		var vmid int
		if _, err := fmt.Sscanf(parts[3], "%d", &vmid); err != nil {
			return command.Result{}, demoFail("pvesh", args)
		}
		guest := r.find(vmid)
		if guest == nil || guest.lxc != (parts[2] == "lxc") {
			return command.Result{}, demoMissing(args, parts)
		}
		switch strings.Join(parts[4:], "/") {
		case "status/current":
			return demoJSON(guest)
		case "config":
			return demoJSON(demoConfig(guest))
		case "snapshot":
			return demoJSON([]map[string]any{
				{"name": "before-upgrade", "description": "Before the 9.0 upgrade", "snaptime": 1767500000},
				{"name": "clean-install", "description": "Fresh install", "snaptime": 1766000000},
				{"name": "current", "description": "You are here!"},
			})
		}
	}
	return command.Result{}, demoFail("pvesh", args)
}

func demoConfig(guest *demoGuest) map[string]any {
	if guest.lxc {
		return map[string]any{
			"hostname": guest.Name, "cores": 2, "memory": guest.MaxMem >> 20, "swap": 512,
			"ostype": "debian", "unprivileged": 1, "onboot": 1,
			"rootfs": "local-lvm:vm-" + fmt.Sprint(guest.VMID) + "-disk-0,size=8G",
			"net0":   "name=eth0,bridge=vmbr0,ip=dhcp,type=veth",
		}
	}
	return map[string]any{
		"name": guest.Name, "cores": 4, "sockets": 1, "cpu": "host", "memory": guest.MaxMem >> 20, "balloon": 0,
		"scsihw": "virtio-scsi-single", "bios": "ovmf", "machine": "q35", "ostype": "l26", "agent": "1",
		"scsi0": "local-lvm:vm-" + fmt.Sprint(guest.VMID) + "-disk-0,iothread=1,size=64G",
		"net0":  "virtio=BC:24:11:AA:BB:CC,bridge=vmbr0,firewall=1",
		"boot":  "order=scsi0;net0", "onboot": 1, "tags": "demo",
	}
}

// option returns the value that follows a "-name" option of a pvesh call.
func option(args []string, name string) string {
	for i, arg := range args {
		if arg == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func (r *demoRunner) create(path string, parts []string, args []string) (command.Result, error) {
	full := append([]string{"create"}, args[1:]...)
	switch {
	case path == "/access/tfa":
		// What the real pvesh says: the user belongs in the path.
		return command.Result{}, demoRefusal("pvesh", full, "No 'create' handler defined for '/access/tfa'")
	case len(parts) == 3 && parts[0] == "access" && parts[1] == "tfa":
		return r.createTFA(parts[2], args, full)
	case path == "/nodes/pve/vzdump":
		result := r.upid("vzdump", option(args, "-vmid"))
		if option(args, "-vmid") == "103" {
			// The backup of this guest is accepted, then fails: the task ends in error.
			r.failing[strings.Trim(strings.TrimSpace(string(result.Stdout)), `"`)] = true
		}
		return result, nil
	case len(parts) == 5 && parts[4] == "snapshot" && option(args, "-snapname") == "before-upgrade":
		return command.Result{}, demoRefusal("pvesh", full, "400 Parameter verification failed.\nsnapname: snapshot 'before-upgrade' already exists")
	case len(parts) == 6 && parts[4] == "status":
		var vmid int
		if _, err := fmt.Sscanf(parts[3], "%d", &vmid); err != nil {
			return command.Result{}, demoFail("pvesh", []string{"create", path})
		}
		guest := r.find(vmid)
		if guest == nil || guest.lxc != (parts[2] == "lxc") {
			return command.Result{}, demoMissing(full, parts)
		}
		switch parts[5] {
		case "start":
			if guest.VMID == 103 {
				return command.Result{}, demoRefusal("pvesh", []string{"create", path}, "can't lock file '/var/lock/qemu-server/lock-103.conf' - got timeout")
			}
			guest.Status, guest.Uptime = "running", 5
			guest.CPU, guest.Memory = 0.05, guest.MaxMem/8
		case "stop", "shutdown":
			guest.Status, guest.Uptime, guest.CPU, guest.Memory = "stopped", 0, 0, 0
		case "reboot":
			guest.Uptime = 5
		}
		return r.upid("qm"+parts[5], parts[3]), nil
	}
	return r.upid("demo", ""), nil
}

// createTFA adds a factor to a user of the demo. Any 6-digit code is good,
// except 000000, so that the refusal can be shown too.
func (r *demoRunner) createTFA(userid string, args []string, full []string) (command.Result, error) {
	switch option(args, "-type") {
	case "totp":
		if option(args, "-value") == "000000" {
			return command.Result{}, demoRefusal("pvesh", full, "400 Parameter verification failed.\nvalue: invalid totp token")
		}
		id := fmt.Sprintf("%d", 10+len(r.enrolled[userid]))
		r.enrolled[userid] = append(r.enrolled[userid], map[string]any{"id": id, "type": "totp", "description": option(args, "-description"), "created": time.Now().Unix()})
		return demoJSON(map[string]any{"id": id})
	case "recovery":
		for _, entry := range r.enrolled[userid] {
			if entry["type"] == "recovery" {
				return command.Result{}, demoRefusal("pvesh", full, "400 Parameter verification failed.\ntype: a recovery entry already exists")
			}
		}
		r.enrolled[userid] = append(r.enrolled[userid], map[string]any{"id": "20", "type": "recovery", "description": option(args, "-description"), "created": time.Now().Unix()})
		return demoJSON(map[string]any{"id": "20", "recovery": []string{
			"7f3k-2m9q", "a8d4-n5xw", "p2c6-h7vz", "t9b1-j4re", "y3g8-s6ku",
			"m5q2-d7ca", "w4n9-f1xh", "e6v3-b8tj", "k1r7-z2py", "c8h5-u9ld",
		}})
	}
	return command.Result{}, demoFail("pvesh", full)
}

// demoStepDelay is the pause between the steps of the demo update.
var demoStepDelay = 500 * time.Millisecond

// demoUpdater plays a new version on the fake node: nothing is read from the
// network or installed.
type demoUpdater struct{}

// DemoUpdater returns the updater of the demo: a newer version is always
// "available", and installing it only shows the steps.
func DemoUpdater() Updater { return demoUpdater{} }

var demoVersionPattern = regexp.MustCompile(`^([0-9]+)\.([0-9]+)\.([0-9]+)`)

// demoNextVersion is the version after current: the next patch of a release,
// or a fixed one for any other build.

func demoNextVersion(current string) string {
	m := demoVersionPattern.FindStringSubmatch(current)
	if m == nil {
		return "v9.9.9"
	}
	patch, _ := strconv.Atoi(m[3])
	return fmt.Sprintf("v%s.%s.%d", m[1], m[2], patch+1)
}

func (demoUpdater) Check(context.Context) (update.Info, error) {
	latest := demoNextVersion(version.Version)
	return update.Info{
		Kind:      version.KindStable,
		Current:   version.DisplayOf(version.Version, version.Commit),
		Latest:    latest,
		Tag:       latest,
		Available: true,
		Notes:     "### Features\n\n- Guided package upgrade, with a reboot warning\n- One log for errors and results\n\n### Fixes\n\n- The snapshot of a VM with virtiofs no longer saves its RAM",
	}, nil
}

func (demoUpdater) Install(ctx context.Context, info update.Info, progress func(string)) (update.Result, error) {
	for _, step := range []string{
		"downloading " + update.Asset + " (" + info.Tag + ")",
		"checking the SHA-256 sum",
		"checking that the new binary runs",
		"installing",
	} {
		select {
		case <-ctx.Done():
			return update.Result{}, ctx.Err()
		case <-time.After(demoStepDelay):
		}
		progress(step)
	}
	return update.Result{
		Path:     "/usr/local/sbin/pve-console",
		Previous: "/usr/local/sbin/pve-console.prev",
		Version:  "pve-console " + strings.TrimPrefix(info.Tag, "v") + " (demo)",
	}, nil
}
