# PVC

`pvm` is a lightweight Proxmox CLI manager intended to run locally on a Proxmox VE node.

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
bin/pvm
```

Equivalent PowerShell command from Windows:

```powershell
$env:GOOS="linux"
$env:GOARCH="amd64"
$env:CGO_ENABLED="0"
& 'C:\Program Files\Go\bin\go.exe' build -o bin/pvm ./cmd/pvm
```

## Run

```bash
./bin/pvm
./bin/pvm tui
./bin/pvm version
./bin/pvm status
./bin/pvm vm list
./bin/pvm vm status 300
./bin/pvm storage list
./bin/pvm check
./bin/pvm snapshot list 300
./bin/pvm snapshot create 300 before-upgrade --description "Before upgrade"
```

Global options:

```bash
./bin/pvm --debug version
./bin/pvm --verbose version
./bin/pvm --json status
./bin/pvm --json check
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

- `pvm`
- `pvm tui`
- `pvm status`
- `pvm status --json`
- `pvm vm list`
- `pvm vm list --json`
- `pvm vm status <vmid>`
- `pvm vm status <vmid> --json`
- `pvm storage list`
- `pvm storage list --json`
- `pvm snapshot list <vmid>`
- `pvm snapshot list <vmid> --json`
- `pvm snapshot create <vmid> [name]`
- `pvm snapshot create <vmid> [name] --description <text>`

Implemented health checks:

- `pvm check`
- `pvm check --json`
- node version/kernel presence
- filesystem usage from `df -P`
- storage usage and active state
- guest running/stopped summary

`pvm check` exit codes:

- `0`: OK or INFO only
- `1`: WARN
- `2`: FAIL
- `3`: internal error

TUI shortcuts:

- `j/k` or arrow keys: select guest
- `/`: search by VMID or name
- `f`: cycle all/running/stopped
- `t`: cycle sort by ID/name/status
- `c`: clear search
- `r`: refresh
- `s`: load snapshots for the selected guest
- `n`: create a timestamped snapshot for the selected guest
- `q`: quit
