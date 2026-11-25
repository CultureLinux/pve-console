package cli

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"pvc/internal/proxmox"
	"pvc/internal/ui"
	"pvc/internal/update"
)

// tuiHarness replaces what runTUI starts, and records how it was called.
type tuiHarness struct {
	updater   ui.Updater
	runs      int
	restarts  [][]string
	restartAt string
}

func newTUIHarness(t *testing.T) *tuiHarness {
	t.Helper()
	h := &tuiHarness{}
	previousRoot, previousRun, previousRestart, previousExe := isRoot, runRealTUI, restartProcess, executablePath
	t.Cleanup(func() {
		isRoot, runRealTUI, restartProcess, executablePath = previousRoot, previousRun, previousRestart, previousExe
	})

	isRoot = func() bool { return true }
	executablePath = func() (string, error) { return "/usr/local/sbin/pve-console", nil }
	runRealTUI = func(_ proxmox.Client, _ bool, updater ui.Updater) (bool, error) {
		h.runs++
		h.updater = updater
		return false, nil
	}
	restartProcess = func(exe string, args []string) error {
		h.restartAt = exe
		h.restarts = append(h.restarts, args)
		return nil
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv(noUpdateCheckEnv, "")
	return h
}

func runTUIArgs(args ...string) (int, string) {
	var stdout, stderr bytes.Buffer
	code := RunWithClient(append([]string{"tui"}, args...), Options{}, proxmox.Client{}, &stdout, &stderr)
	return code, stderr.String()
}

func (h *tuiHarness) disabled(t *testing.T) bool {
	t.Helper()
	u, ok := h.updater.(update.Updater)
	if !ok {
		t.Fatalf("the updater is %T", h.updater)
	}
	return u.Disabled
}

func TestTUILooksForUpdatesByDefault(t *testing.T) {
	h := newTUIHarness(t)
	if code, stderr := runTUIArgs(); code != 0 || h.runs != 1 {
		t.Fatalf("code=%d runs=%d stderr=%q", code, h.runs, stderr)
	}
	u := h.updater.(update.Updater)
	if u.Disabled || u.Exe != "/usr/local/sbin/pve-console" {
		t.Fatalf("updater = %+v", u)
	}
	if u.Client.Repo == "" || !strings.HasPrefix(u.Client.APIBase, "https://") || !strings.HasPrefix(u.Client.DownloadBase, "https://") {
		t.Fatalf("the client talks to GitHub over https: %+v", u.Client)
	}
	if u.Client.GOOS == "" || u.Client.GOARCH == "" {
		t.Fatal("the platform must be set")
	}
}

func TestTUIUpdateCheckCanBeTurnedOff(t *testing.T) {
	tests := []struct {
		name string
		env  string
		args []string
		off  bool
	}{
		{"flag", "", []string{"--no-update-check"}, true},
		{"environment", "1", nil, true},
		{"environment with any text", "yes", nil, true},
		{"environment set to 0", "0", nil, false},
		{"environment empty", "", nil, false},
		{"both", "1", []string{"--no-update-check"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTUIHarness(t)
			t.Setenv(noUpdateCheckEnv, tt.env)
			if code, stderr := runTUIArgs(tt.args...); code != 0 {
				t.Fatalf("code=%d stderr=%q", code, stderr)
			}
			if got := h.disabled(t); got != tt.off {
				t.Fatalf("Disabled = %t, want %t", got, tt.off)
			}
		})
	}
}

func TestTUIWithoutThePathOfTheProgramCannotUpdate(t *testing.T) {
	h := newTUIHarness(t)
	executablePath = func() (string, error) { return "", errors.New("no /proc") }
	if code, _ := runTUIArgs(); code != 0 || !h.disabled(t) {
		t.Fatalf("code=%d: without its own path the program must not try to replace itself", code)
	}
}

func TestTUIRestartsItselfAfterAnUpdate(t *testing.T) {
	h := newTUIHarness(t)
	runRealTUI = func(_ proxmox.Client, _ bool, _ ui.Updater) (bool, error) { return true, nil }
	if code, stderr := runTUIArgs(); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if len(h.restarts) != 1 || h.restartAt != "/usr/local/sbin/pve-console" {
		t.Fatalf("restarts=%v at %q", h.restarts, h.restartAt)
	}
	if len(h.restarts[0]) != len(os.Args) || h.restarts[0][0] != os.Args[0] {
		t.Fatalf("the same arguments must be used: %v", h.restarts[0])
	}
}

func TestTUIDoesNotRestartWithoutBeingAsked(t *testing.T) {
	h := newTUIHarness(t)
	if code, _ := runTUIArgs(); code != 0 || len(h.restarts) != 0 {
		t.Fatalf("code=%d restarts=%d", code, len(h.restarts))
	}
}

func TestTUIReportsAFailedRestart(t *testing.T) {
	h := newTUIHarness(t)
	runRealTUI = func(_ proxmox.Client, _ bool, _ ui.Updater) (bool, error) { return true, nil }
	restartProcess = func(string, []string) error { return errors.New("exec format error") }
	code, stderr := runTUIArgs()
	if code != 1 || !strings.Contains(stderr, "could not restart") || !strings.Contains(stderr, "exec format error") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	_ = h
}

func TestTUIReportsAnError(t *testing.T) {
	newTUIHarness(t)
	runRealTUI = func(_ proxmox.Client, _ bool, _ ui.Updater) (bool, error) { return false, errors.New("no terminal") }
	if code, stderr := runTUIArgs(); code != 1 || !strings.Contains(stderr, "no terminal") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestTUIDemoIgnoresTheUpdateFlag(t *testing.T) {
	h := newTUIHarness(t)
	previous := runDemoTUI
	t.Cleanup(func() { runDemoTUI = previous })
	called := 0
	runDemoTUI = func() error { called++; return nil }
	if code, stderr := runTUIArgs("--demo", "--no-update-check"); code != 0 || called != 1 || h.runs != 0 {
		t.Fatalf("code=%d called=%d runs=%d stderr=%q", code, called, h.runs, stderr)
	}
}

func TestTUIStillRefusesUnknownArguments(t *testing.T) {
	h := newTUIHarness(t)
	for _, args := range [][]string{{"--bogus"}, {"--no-update-check", "extra"}, {"update"}} {
		if code, stderr := runTUIArgs(args...); code != 2 || !strings.Contains(stderr, "unexpected argument") {
			t.Fatalf("args=%v code=%d stderr=%q", args, code, stderr)
		}
	}
	if h.runs != 0 {
		t.Fatal("must not start")
	}
}

func TestUsageMentionsTheFlag(t *testing.T) {
	if !strings.Contains(usage, "--no-update-check") || !strings.Contains(usage, noUpdateCheckEnv) {
		t.Fatal("the usage must tell how to turn the check off")
	}
}
