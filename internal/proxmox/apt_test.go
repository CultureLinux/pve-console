package proxmox

import (
	"context"
	"errors"
	"strings"
	"testing"

	"pvc/internal/command"
)

const (
	updateCall  = "env DEBIAN_FRONTEND=noninteractive apt-get update"
	upgradeCall = "env DEBIAN_FRONTEND=noninteractive apt-get -y -o Dpkg::Options::=--force-confold dist-upgrade"
	flagCall    = "test -e /var/run/reboot-required"
)

// lineRunner is a MockRunner that also reports the output line by line.
type lineRunner struct {
	command.MockRunner
	streamed int
}

func (r *lineRunner) RunLines(ctx context.Context, onLine func(string), name string, args ...string) (command.Result, error) {
	r.streamed++
	result, err := r.Run(ctx, name, args...)
	for _, line := range strings.Split(strings.TrimSpace(string(result.Stdout)), "\n") {
		onLine(line)
	}
	return result, err
}

func TestUpdateIndexRunsAptGetUpdate(t *testing.T) {
	runner := &command.MockRunner{Responses: map[string]command.Result{updateCall: {Stdout: []byte("Hit:1 http://deb.debian.org bookworm InRelease\nReading package lists...\n")}}}
	var lines []string
	output, err := Client{Runner: runner}.UpdateIndex(context.Background(), func(line string) { lines = append(lines, line) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(output, "Reading package lists...") || len(lines) != 2 {
		t.Fatalf("output=%q lines=%q (a runner without lines still reports them, at the end)", output, lines)
	}
}

func TestUpdateIndexFailureKeepsTheCauseAndTheOutput(t *testing.T) {
	cause := command.CommandError{Result: command.Result{Command: "env", Stderr: []byte("E: Failed to fetch 401  Unauthorized")}, Err: errors.New("exit status 100")}
	runner := &command.MockRunner{
		Responses: map[string]command.Result{updateCall: {Stdout: []byte("Err:1 https://enterprise.proxmox.com"), Stderr: []byte("E: Failed to fetch 401  Unauthorized")}},
		Errors:    map[string]error{updateCall: cause},
	}
	output, err := Client{Runner: runner}.UpdateIndex(context.Background(), nil)
	var commandErr command.CommandError
	if err == nil || !errors.As(err, &commandErr) || !strings.Contains(err.Error(), "package index") {
		t.Fatalf("the failure is wrapped, the command error kept: %v", err)
	}
	if !strings.Contains(output, "Err:1") || !strings.Contains(output, "401") {
		t.Fatalf("both outputs are returned: %q", output)
	}
}

func TestDistUpgradeWithUsesLineRunnerWhenAsked(t *testing.T) {
	runner := &lineRunner{MockRunner: command.MockRunner{Responses: map[string]command.Result{upgradeCall: {Stdout: []byte("Unpacking a\nSetting up a\n")}}}}
	var lines []string
	output, err := Client{Runner: runner}.DistUpgradeWith(context.Background(), func(line string) { lines = append(lines, line) })
	if err != nil || runner.streamed != 1 || strings.Join(lines, "|") != "Unpacking a|Setting up a" || output != "Unpacking a\nSetting up a" {
		t.Fatalf("err=%v streamed=%d lines=%q output=%q", err, runner.streamed, lines, output)
	}

	// Without a callback the plain Run is used: the CLI path does not change.
	if _, err := (Client{Runner: runner}).DistUpgrade(context.Background()); err != nil || runner.streamed != 1 {
		t.Fatalf("DistUpgrade must not stream: err=%v streamed=%d", err, runner.streamed)
	}
}

func TestInstalledKernels(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{"signed and unsigned of one release, once", "Setting up proxmox-kernel-6.14.8-3-pve-signed (6.14.8-3) ...\nSetting up proxmox-kernel-6.14.8-3-pve (6.14.8-3) ...", "6.14.8-3-pve"},
		{"old name", "Setting up pve-kernel-5.15.131-2-pve (5.15.131-2) ...", "5.15.131-2-pve"},
		{"meta packages and headers are not kernels", "Setting up proxmox-kernel-6.14 (6.14.8-3) ...\nSetting up proxmox-headers-6.14.8-3-pve (6.14.8-3) ...\nSetting up proxmox-default-kernel (1.1.0) ...", ""},
		{"unpacking is not setting up", "Unpacking proxmox-kernel-6.14.8-3-pve-signed (6.14.8-3) ...", ""},
		{"two releases", "Setting up proxmox-kernel-6.8.12-4-pve-signed (6.8.12-4) ...\nSetting up proxmox-kernel-6.14.8-3-pve-signed (6.14.8-3) ...", "6.8.12-4-pve,6.14.8-3-pve"},
		{"nothing", "Setting up libssl3 (3.0.16-1) ...", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := strings.Join(installedKernels(tt.output), ","); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRebootNeeded(t *testing.T) {
	newKernel := "Setting up proxmox-kernel-6.14.8-3-pve-signed (6.14.8-3) ..."
	uname := func(release string) map[string]command.Result {
		return map[string]command.Result{"uname -r": {Stdout: []byte(release + "\n")}}
	}
	tests := []struct {
		name    string
		output  string
		flag    bool
		running string
		want    []string
		without []string
	}{
		{"nothing says so", "Setting up libssl3 (3.0.16-1) ...", false, "6.14.8-2-pve", nil, []string{"kernel", "asks"}},
		{"flag file", "Setting up libssl3 (3.0.16-1) ...", true, "6.14.8-2-pve", []string{"the system asks for it (/var/run/reboot-required)"}, []string{"kernel"}},
		{"new kernel", newKernel, false, "6.14.8-2-pve", []string{"kernel 6.14.8-3-pve installed, kernel 6.14.8-2-pve running"}, []string{"asks"}},
		{"the new kernel already runs", newKernel, false, "6.14.8-3-pve", nil, []string{"kernel", "asks"}},
		{"both", newKernel, true, "6.14.8-2-pve", []string{"asks for it", "kernel 6.14.8-3-pve installed"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			responses := uname(tt.running)
			errs := map[string]error{}
			if tt.flag {
				responses[flagCall] = command.Result{}
			} else {
				// test -e exits 1 when the file is missing.
				errs[flagCall] = command.CommandError{Result: command.Result{Command: "test", ExitCode: 1}, Err: errors.New("exit status 1")}
			}
			got := Client{Runner: &command.MockRunner{Responses: responses, Errors: errs}}.RebootNeeded(context.Background(), tt.output)
			if len(tt.want) == 0 && got != "" {
				t.Fatalf("no warning expected, got %q", got)
			}
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in %q", want, got)
				}
			}
			for _, not := range tt.without {
				if strings.Contains(got, not) {
					t.Errorf("unexpected %q in %q", not, got)
				}
			}
		})
	}
}

func TestRebootNeededNeverGuessesWhenUnameFails(t *testing.T) {
	runner := &command.MockRunner{Responses: map[string]command.Result{}, Errors: map[string]error{
		flagCall:   errors.New("exit status 1"),
		"uname -r": errors.New("uname missing"),
	}}
	if got := (Client{Runner: runner}).RebootNeeded(context.Background(), "Setting up proxmox-kernel-6.14.8-3-pve-signed (6.14.8-3) ..."); got != "" {
		t.Fatalf("an unreadable running kernel is not a reason: %q", got)
	}
}
