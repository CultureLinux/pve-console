package health

import "testing"

func TestParseDF(t *testing.T) {
	data := []byte(`Filesystem 1024-blocks Used Available Capacity Mounted on
/dev/root 100 70 30 70% /
/dev/sda1 100 95 5 95% /boot
`)

	checks, err := parseDF(data, FilesystemOptions{Warning: 0.80, Critical: 0.90})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(checks))
	}
	if checks[0].Status != StatusOK {
		t.Fatalf("expected root ok, got %s", checks[0].Status)
	}
	if checks[1].Status != StatusFail {
		t.Fatalf("expected boot fail, got %s", checks[1].Status)
	}
}
