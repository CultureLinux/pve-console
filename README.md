# PVC

`pvm` is a lightweight Proxmox CLI manager intended to run locally on a Proxmox VE node.

The project is currently in Sprint 1: runtime foundations, command execution abstraction, validations, and the first `pvm version` command.

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
```

Global options:

```bash
./bin/pvm --debug version
./bin/pvm --verbose version
```

Future Sprint 2 commands:

```bash
./bin/pvm status
./bin/pvm vm list
./bin/pvm storage list
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
