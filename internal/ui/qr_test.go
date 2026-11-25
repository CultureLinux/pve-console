package ui

import (
	"strings"
	"testing"
)

func TestNewQR(t *testing.T) {
	t.Parallel()
	text := "otpauth://totp/pve:alice@pve?secret=" + strings.Repeat("A", 32)
	code, err := newQR(text)
	if err != nil {
		t.Fatal(err)
	}
	// Version 4-L is 33 modules; with the margin, 37 modules: 19 terminal rows.
	if code.height() < 15 || code.height() > 23 {
		t.Fatalf("height = %d", code.height())
	}
	if code.width() < 29 || code.width() > 45 {
		t.Fatalf("width = %d", code.width())
	}
	for i, line := range code.lines {
		if got := textWidth(line); got != code.width() {
			t.Fatalf("line %d is %d columns wide, want %d", i, got, code.width())
		}
		if strings.Contains(stripANSI(line), " ") {
			t.Fatalf("line %d has a gap", i)
		}
	}
}

func TestNewQRTooLong(t *testing.T) {
	t.Parallel()
	if _, err := newQR(strings.Repeat("x", 5000)); err == nil {
		t.Fatal("a text too long for a QR code must fail")
	}
}

func TestQRCodeEmpty(t *testing.T) {
	t.Parallel()
	if (qrCode{}).width() != 0 || (qrCode{}).height() != 0 {
		t.Fatal("an empty code has no size")
	}
}
