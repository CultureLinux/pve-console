# PVC

`pvm` is a lightweight Proxmox CLI manager intended to run locally on a Proxmox VE node.

The project is currently in Sprint 2: local Proxmox read commands backed by `pvesh` JSON.

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
./bin/pvm version
./bin/pvm status
./bin/pvm vm list
./bin/pvm vm status 300
./bin/pvm storage list
```

Global options:

```bash
./bin/pvm --debug version
./bin/pvm --verbose version
./bin/pvm --json status
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

- `pvm status`
- `pvm status --json`
- `pvm vm list`
- `pvm vm list --json`
- `pvm vm status <vmid>`
- `pvm vm status <vmid> --json`
- `pvm storage list`
- `pvm storage list --json`
