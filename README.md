# PVC

`pve-console` is a lightweight Proxmox CLI manager intended to run locally on a Proxmox VE node.

The project currently provides local Proxmox read commands, health checks, and an interactive terminal UI backed by `pvesh` JSON.

## Requirements

On Windows:

```powershell
winget install GoLang.Go
```

Ensure `go` is available in `PATH`, or call it directly from:

```powershell
C:\Program Files\Go\bin\go.exe
```

## Build

The target platform is Proxmox VE 9.x on Debian amd64.

```bash
make build
```

This produces:

```text
bin/pve-console
```

Equivalent PowerShell command from Windows:

```powershell
$env:GOOS="linux"
$env:GOARCH="amd64"
$env:CGO_ENABLED="0"
& 'C:\Program Files\Go\bin\go.exe' build -o bin/pve-console ./cmd/pvm
```

## Run

```bash
./bin/pve-console
./bin/pve-console tui
./bin/pve-console version
./bin/pve-console status
./bin/pve-console vm list
./bin/pve-console vm status 300
./bin/pve-console storage list
./bin/pve-console check
./bin/pve-console snapshot list 300
./bin/pve-console snapshot create 300 before-upgrade --description "Before upgrade"
```

Global options:

```bash
./bin/pve-console --debug version
./bin/pve-console --verbose version
./bin/pve-console --json status
./bin/pve-console --json check
```

## Development

```bash
make test
make fmt
make vet
```

Core runtime foundations:

- external commands go through `internal/command.Runner`
- `ExecRunner` uses `exec.CommandContext` with separated arguments
- command execution has a default timeout
- stdout, stderr, exit code and duration are captured
- VMID and snapshot names have strict validation helpers

Implemented Proxmox read commands:

- `pve-console`
- `pve-console tui`
- `pve-console status`
- `pve-console status --json`
- `pve-console vm list`
- `pve-console vm list --json`
- `pve-console vm status <vmid>`
- `pve-console vm status <vmid> --json`
- `pve-console storage list`
- `pve-console storage list --json`
- `pve-console snapshot list <vmid>`
- `pve-console snapshot list <vmid> --json`
- `pve-console snapshot create <vmid> [name]`
- `pve-console snapshot create <vmid> [name] --description <text>`

Implemented health checks:

- `pve-console check`
- `pve-console check --json`
- node version/kernel presence
- filesystem usage from `df -P`
- storage usage and active state
- guest running/stopped summary

`pve-console check` exit codes:

- `0`: OK or INFO only
- `1`: WARN
- `2`: FAIL
- `3`: internal error

TUI shortcuts:

- `j/k` or arrow keys: select guest
- guest view `tab`: cycle all/QEMU/LXC
- `enter`: open selected guest configuration
- `esc`: close configuration view or quit
- `/`: search by VMID or name
- `f`: cycle all/running/stopped
- `t`: cycle sort by ID/name/status
- `c`: clear search
- `r`: refresh
- `s`: load snapshots for the selected guest
- `n`: create a named snapshot for the selected guest
- snapshot prompt `tab`: toggle RAM snapshot, enabled by default for QEMU
- `u`: toggle users view
- `p`: package updates view
- `b`: backup view, showing existing backup volumes by default
- backup view `tab`: toggle existing volumes / scheduled jobs
- backup volumes show VMID, VM name, readable date, and size
- `g`: return to VM/LXC view
- users view `s`: load TFA entries for the selected user
- users view `x`: disable TFA after typing the selected userid
- `q`: quit
