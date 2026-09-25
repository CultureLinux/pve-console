package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"version"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if got := strings.TrimSpace(stdout.String()); got != "pvm dev" {
		t.Fatalf("expected version output %q, got %q", "pvm dev", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestRunVersionWithDebug(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"--debug", "version"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if got := strings.TrimSpace(stdout.String()); got != "pvm dev" {
		t.Fatalf("expected version output %q, got %q", "pvm dev", got)
	}
	if !strings.Contains(stderr.String(), "debug: command=version") {
		t.Fatalf("expected debug output, got %q", stderr.String())
	}
}

func TestRunWithoutCommandPrintsUsage(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run(nil, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("expected usage in stdout, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestParseGlobalOptions(t *testing.T) {
	opts, rest, err := ParseGlobalOptions([]string{"--debug", "vm", "list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.Debug {
		t.Fatal("expected debug enabled")
	}
	if !opts.Verbose {
		t.Fatal("expected debug to enable verbose")
	}
	if len(rest) != 2 || rest[0] != "vm" || rest[1] != "list" {
		t.Fatalf("unexpected rest args: %#v", rest)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"wat"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unknown command: wat") {
		t.Fatalf("expected unknown command error, got %q", stderr.String())
	}
}
