package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureCreatesConfigOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	cfg, created, err := Ensure("0.1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created {
		t.Fatal("expected config to be created")
	}
	if cfg.Version != "0.1.0" {
		t.Fatalf("unexpected config: %#v", cfg)
	}

	data, err := os.ReadFile(filepath.Join(home, FileName))
	if err != nil {
		t.Fatalf("expected config file: %v", err)
	}
	if !strings.Contains(string(data), `"version": "0.1.0"`) {
		t.Fatalf("unexpected config file: %q", data)
	}

	_, created, err = Ensure("0.1.0")
	if err != nil {
		t.Fatalf("unexpected second error: %v", err)
	}
	if created {
		t.Fatal("expected existing config to skip creation")
	}
}
