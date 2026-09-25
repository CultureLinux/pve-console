# PVC

`pvm` is a lightweight Proxmox CLI manager intended to run locally on a Proxmox VE node.

The project is currently in Sprint 0: repository skeleton, Linux amd64 build, and the first `pvm version` command.

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
