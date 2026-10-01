# PVE Console

`pve-console` is a lightweight Proxmox CLI manager intended to run locally on a Proxmox VE node.

The project provides local Proxmox commands, health checks, backup/snapshot actions, package update handling, and an interactive terminal UI backed by `pvesh` JSON.

## Requirements

On Windows:

```powershell
winget install GoLang.Go
```

Ensure `go` is available in `PATH`, or call it directly from:

```powershell
C:\Program Files\Go\bin\go.exe
```

On Linux/Proxmox:

```bash
go version
make --version
git --version
file --version
```

## Build

The target platform is Proxmox VE 9.x on Debian Linux amd64.

From Linux/macOS, or from a shell with GNU Make:

```bash
make build
```

This is equivalent to:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-s -w" -o bin/pve-console ./cmd/pvm
```

From Windows PowerShell:

```powershell
New-Item -ItemType Directory -Force .\bin | Out-Null
$env:GOOS="linux"
$env:GOARCH="amd64"
$env:CGO_ENABLED="0"
& 'C:\Program Files\Go\bin\go.exe' build -ldflags '-s -w' -o .\bin\pve-console .\cmd\pvm
```

The output is a Linux amd64 binary:

```text
bin/pve-console
```

Deploy example on a Proxmox node:

```bash
install -o root -g root -m 0755 bin/pve-console /usr/local/sbin/pve-console
/usr/local/sbin/pve-console version
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

## Public GitHub Publishing

GitLab remains the private source of truth. GitHub receives only the sanitized `public` branch.

Create the public branch without private files:

```bash
git switch --orphan public
git rm -r --cached .
git add .
git reset -- .gitlab-ci.yml copilot
git commit -m "Initial public release"
git push origin public
```

Keep these paths out of `public`:

```gitignore
.gitlab-ci.yml
copilot/
```

The GitLab job `publish:github` runs from the private branch and pushes only `origin/public` to GitHub `main`.

Required GitLab CI variables:

- `GITHUB_REPO_SSH`: GitHub SSH repo URL, for example `git@github.com:<user>/pve9s.git`
- `GITHUB_DEPLOY_KEY`: private SSH key allowed to push to that GitHub repo

Run the manual `publish:github` job after updating and pushing the `public` branch.

## Development

```bash
make test
make fmt
make vet
```

Core runtime foundations:

- external commands go through `internal/command.Runner`
- `ExecRunner` uses `exec.CommandContext` with separated arguments
- command execution has a default timeout unless the caller provides a deadline
- stdout, stderr, exit code and duration are captured
- VMID and snapshot names have strict validation helpers

Implemented Proxmox commands:

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
- TUI guest start/stop actions
- TUI snapshot create/delete/rollback actions
- TUI backup run/delete/restore actions
- TUI backup job schedule/storage/VMID editing
- TUI user and TFA views
- TUI apt update listing and dist-upgrade action

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

- `j/k` or arrow keys: move in the active list
- guest view `tab`: cycle all/QEMU/LXC
- `enter`: open selected guest configuration
- `+`: start selected guest after confirmation
- `-`: stop selected guest after confirmation
- `esc`: close modal/sub-section, then return to guests, then quit
- `/`: search by VMID, name, user, package, backup, etc.
- `f`: cycle all/running/stopped
- `t`: cycle sort by ID/name/status
- `c`: clear search
- `r`: refresh active view
- `s`: load snapshots for the selected guest
- snapshots `up/down`: select a snapshot
- snapshots `d`: delete selected snapshot after confirmation
- snapshots `o`: rollback/restore selected snapshot after confirmation
- `n`: create a named snapshot for the selected guest
- snapshot prompt `tab`: toggle RAM snapshot, enabled by default for QEMU
- `u`: users view
- users view `s`: load TFA entries for the selected user
- users view `x`: disable TFA after typing the selected userid
- `p`: package updates view
- updates view `a`: run `apt-get -y dist-upgrade` and show output in a modal
- `b`: backup view, showing existing backup volumes by default
- backup view `tab`: toggle existing volumes / scheduled jobs
- backup volumes show VMID, VM name, readable date, and size
- backup volumes `d`: delete selected backup after confirmation
- backup volumes `o`: restore selected backup after confirmation
- backup jobs `e`: edit schedule, storage and VMID checklist
- backup views `m`: run a manual backup
- `g`: return to VM/LXC view
- `?` or `h`: help
- `q`: quit