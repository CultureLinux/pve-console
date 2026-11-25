//go:build !windows

package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRestartReplacesTheProcess re-runs the test binary as a helper: it execs a
// script and the script's output proves the process was replaced, with the arguments.
func TestRestartReplacesTheProcess(t *testing.T) {
	if os.Getenv("PVC_RESTART_HELPER") == "1" {
		script := os.Getenv("PVC_RESTART_SCRIPT")
		err := Restart(script, []string{"restarted-program", "tui", "--demo"})
		// Only reached when Restart failed.
		os.Stderr.WriteString("restart failed: " + err.Error() + "\n")
		os.Exit(3)
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "next")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho \"started as $0: $*\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestRestartReplacesTheProcess")
	cmd.Env = append(os.Environ(), "PVC_RESTART_HELPER=1", "PVC_RESTART_SCRIPT="+script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "started as ") || !strings.Contains(string(out), ": tui --demo") {
		t.Fatalf("the new process must run the program with the arguments:\n%s", out)
	}
}

func TestRestartReportsAnError(t *testing.T) {
	if err := Restart("/nonexistent/program", []string{"x"}); err == nil {
		t.Fatal("restarting a program that does not exist must fail, not exit")
	}
}
