package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"pvc/internal/proxmox"
)

// The secret of these tests: 20 zero bytes, which is 32 times "A" in base32.
var testSecret = strings.Repeat("A", 32)

// enrollModel is a model on the users view, ready to enroll alice@pve, with a
// fixed secret. It changes a package variable: tests using it do not run in parallel.
func enrollModel(t *testing.T, runner *fakeRunner) Model {
	t.Helper()
	previous := enrollSource
	enrollSource = bytes.NewReader(make([]byte, 20))
	t.Cleanup(func() { enrollSource = previous })

	m := newTestModel(runner)
	m.width, m.height = 120, 40
	m.viewMode = viewUsers
	m.data.users = []proxmox.User{{UserID: "alice@pve"}, {UserID: "bob@pve"}}
	return m
}

func typeDigits(t *testing.T, m Model, digits string) Model {
	t.Helper()
	for _, r := range digits {
		m, _ = press(t, m, string(r))
	}
	return m
}

func TestEnrollFullFlow(t *testing.T) {
	runner := &fakeRunner{stdout: map[string]string{
		"-type recovery":            `{"id":"20","recovery":["aaaa-1111","bbbb-2222"]}`,
		"get /access/tfa/alice@pve": `[{"id":"10","type":"totp","description":"pve-console"}]`,
	}}
	m := enrollModel(t, runner)

	m, cmd := press(t, m, "n")
	if cmd != nil || m.modalMode != modalEnroll || m.enroll.userID != "alice@pve" || m.enroll.phase != enrollCode {
		t.Fatalf("mode=%q enroll=%+v", m.modalMode, m.enroll)
	}
	if want := "otpauth://totp/pve:alice@pve?secret=" + testSecret; m.enroll.uri != want {
		t.Fatalf("uri = %q", m.enroll.uri)
	}
	screen := m.View()
	for _, want := range []string{"Enroll TOTP", "alice@pve", groupKey(testSecret), "▀", "verify"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("screen lacks %q:\n%s", want, screen)
		}
	}

	m = typeDigits(t, m, "123456")
	m, cmd = press(t, m, "enter")
	if cmd == nil || m.enroll.phase != enrollAdding {
		t.Fatalf("phase=%v cmd=%t", m.enroll.phase, cmd != nil)
	}
	if got, _ := press(t, m, "esc"); got.modalMode != modalEnroll {
		t.Fatal("esc must not close the modal while Proxmox checks the code")
	}

	m, cmd = execute(t, m, cmd)
	want := "pvesh create /access/tfa/alice@pve -type totp -totp otpauth://totp/pve:alice@pve?secret=" + testSecret + " -value 123456 -description pve-console"
	if !runner.called(want) {
		t.Fatalf("calls = %v", runner.calls)
	}
	if m.enroll.phase != enrollDone || cmd == nil {
		t.Fatalf("phase=%v cmd=%t", m.enroll.phase, cmd != nil)
	}
	if m.enroll.secret != "" || m.enroll.uri != "" || m.enroll.qr.height() != 0 {
		t.Fatal("the secret must be forgotten once the factor is saved")
	}
	if !m.data.users[0].TFAEnabled || m.data.users[1].TFAEnabled {
		t.Fatalf("users = %+v", m.data.users)
	}
	m, _ = execute(t, m, cmd)
	if len(m.tfaEntries) != 1 || m.tfaUserID != "alice@pve" {
		t.Fatalf("tfa = %q %v", m.tfaUserID, m.tfaEntries)
	}

	m, cmd = press(t, m, "c")
	if cmd == nil || m.enroll.phase != enrollCreatingCodes {
		t.Fatalf("phase=%v", m.enroll.phase)
	}
	m, _ = execute(t, m, cmd)
	if m.enroll.phase != enrollRecovery || len(m.enroll.recovery) != 2 {
		t.Fatalf("phase=%v codes=%v", m.enroll.phase, m.enroll.recovery)
	}
	if screen := m.View(); !strings.Contains(screen, "aaaa-1111") || !strings.Contains(screen, "bbbb-2222") {
		t.Fatalf("codes not shown:\n%s", screen)
	}
	if log := fmt.Sprint(m.entries); strings.Contains(log, "aaaa-1111") || strings.Contains(log, testSecret) {
		t.Fatalf("a secret reached the log: %s", log)
	}

	m, _ = press(t, m, "enter")
	if m.modalMode != modalNone || m.enroll.userID != "" || m.enroll.recovery != nil {
		t.Fatalf("mode=%q enroll=%+v", m.modalMode, m.enroll)
	}
}

func TestEnrollWrongCodeKeepsTheSecret(t *testing.T) {
	boom := errors.New("400 Parameter verification failed: invalid totp token")
	runner := &fakeRunner{failing: map[string]error{"-type totp": boom}}
	m := enrollModel(t, runner)
	m, _ = press(t, m, "n")
	m = typeDigits(t, m, "000000")
	m, cmd := press(t, m, "enter")
	m, _ = execute(t, m, cmd)

	if m.enroll.phase != enrollCode || m.enroll.code != "" || m.enroll.secret != testSecret {
		t.Fatalf("enroll = %+v", m.enroll)
	}
	if !errors.Is(m.enroll.err, boom) {
		t.Fatalf("err = %v", m.enroll.err)
	}
	if m.data.users[0].TFAEnabled {
		t.Fatal("nothing was enrolled")
	}
	if screen := m.View(); !strings.Contains(screen, "invalid totp token") {
		t.Fatalf("the refusal is not shown:\n%s", screen)
	}

	// The same secret takes another code.
	runner.failing = nil
	m = typeDigits(t, m, "654321")
	m, cmd = press(t, m, "enter")
	m, _ = execute(t, m, cmd)
	if m.enroll.phase != enrollDone {
		t.Fatalf("phase = %v", m.enroll.phase)
	}
}

func TestEnrollCodeInput(t *testing.T) {
	runner := &fakeRunner{}
	m := enrollModel(t, runner)
	m, _ = press(t, m, "n")

	m = typeDigits(t, m, "12a3-4 5678")
	if m.enroll.code != "123456" {
		t.Fatalf("code = %q: only 6 digits are kept", m.enroll.code)
	}
	m, _ = press(t, m, "backspace", "backspace")
	if m.enroll.code != "1234" {
		t.Fatalf("code = %q", m.enroll.code)
	}
	m, cmd := press(t, m, "enter")
	if cmd != nil || m.enroll.err == nil || m.enroll.phase != enrollCode {
		t.Fatalf("a short code must not be sent: %+v", m.enroll)
	}
	if runner.called("/access/tfa") {
		t.Fatal("nothing must reach Proxmox")
	}
	m, _ = press(t, m, "5")
	if m.enroll.err != nil {
		t.Fatal("typing clears the message")
	}
}

func TestEnrollCancelForgetsTheSecret(t *testing.T) {
	m := enrollModel(t, &fakeRunner{})
	m, _ = press(t, m, "n", "1", "esc")
	if m.modalMode != modalNone || m.enroll.secret != "" || m.enroll.code != "" {
		t.Fatalf("mode=%q enroll=%+v", m.modalMode, m.enroll)
	}
}

func TestEnrollNeedsAUser(t *testing.T) {
	m := enrollModel(t, &fakeRunner{})
	m.data.users = nil
	m, cmd := press(t, m, "n")
	if cmd != nil || m.modalMode != modalNone {
		t.Fatalf("mode=%q", m.modalMode)
	}
	// On the guests view, n still creates a snapshot.
	m.viewMode = viewGuests
	m, _ = press(t, m, "n")
	if m.modalMode != modalSnapshot {
		t.Fatalf("mode=%q", m.modalMode)
	}
}

func TestEnrollLateMessagesAreIgnored(t *testing.T) {
	m := enrollModel(t, &fakeRunner{})
	// No enrollment open.
	got, cmd := deliver(t, m, enrollMsg{userid: "alice@pve"})
	if cmd != nil || got.modalMode != modalNone || got.data.users == nil || got.data.users[0].TFAEnabled {
		t.Fatalf("late enrollMsg changed the model: %+v", got.enroll)
	}
	got, _ = deliver(t, m, recoveryMsg{userid: "alice@pve", codes: []string{"x"}})
	if got.enroll.recovery != nil {
		t.Fatal("late recoveryMsg changed the model")
	}
	// Another user's answer, or an answer in the wrong phase.
	m, _ = press(t, m, "n")
	got, cmd = deliver(t, m, enrollMsg{userid: "bob@pve"})
	if cmd != nil || got.enroll.phase != enrollCode {
		t.Fatalf("phase = %v", got.enroll.phase)
	}
	got, _ = deliver(t, m, recoveryMsg{userid: "alice@pve", codes: []string{"x"}})
	if got.enroll.phase != enrollCode || got.enroll.recovery != nil {
		t.Fatalf("enroll = %+v", got.enroll)
	}
}

func TestEnrollRecoveryRefused(t *testing.T) {
	boom := errors.New("a recovery entry already exists")
	runner := &fakeRunner{failing: map[string]error{"-type recovery": boom}}
	m := enrollModel(t, runner)
	m, _ = press(t, m, "n")
	m = typeDigits(t, m, "123456")
	m, cmd := press(t, m, "enter")
	m, _ = execute(t, m, cmd)
	m, cmd = press(t, m, "c")
	m, _ = execute(t, m, cmd)

	if m.enroll.phase != enrollDone || !errors.Is(m.enroll.err, boom) {
		t.Fatalf("enroll = %+v", m.enroll)
	}
	if screen := m.View(); !strings.Contains(screen, "already exists") {
		t.Fatalf("the refusal is not shown:\n%s", screen)
	}
	// c is only for the done phase.
	m, _ = press(t, m, "enter")
	if m.modalMode != modalNone {
		t.Fatal("enter closes")
	}
}

func TestEnrollModalFitsTheScreen(t *testing.T) {
	for _, size := range [][2]int{{60, 14}, {80, 24}, {100, 30}, {120, 40}, {200, 60}} {
		m := enrollModel(t, &fakeRunner{})
		m.width, m.height = size[0], size[1]
		m, _ = press(t, m, "n")
		screen := m.View()
		lines := strings.Split(screen, "\n")
		if len(lines) != size[1] {
			t.Errorf("%dx%d: %d lines", size[0], size[1], len(lines))
		}
		for i, line := range lines {
			if got := textWidth(line); got != size[0] {
				t.Errorf("%dx%d: line %d is %d columns wide", size[0], size[1], i, got)
				break
			}
		}
		hasQR := strings.Contains(screen, "▀")
		if tooSmall := size[1] < m.enroll.qr.height()+enrollChrome; hasQR == tooSmall {
			t.Errorf("%dx%d: QR shown = %v", size[0], size[1], hasQR)
		}
		if size[1] >= 24 && !strings.Contains(screen, groupKey(testSecret)) && size[1] < m.enroll.qr.height()+enrollChrome {
			t.Errorf("%dx%d: without the QR code the key must be there", size[0], size[1])
		}
	}
}

func TestEnrollHints(t *testing.T) {
	m := enrollModel(t, &fakeRunner{})
	m, _ = press(t, m, "n")
	hints, _ := m.hints()
	if len(hints) == 0 || hints[0].key != "0-9" {
		t.Fatalf("hints = %v", hints)
	}
	m.enroll.phase = enrollAdding
	if hints, _ := m.hints(); hints[0].key != "…" {
		t.Fatalf("hints = %v", hints)
	}
}

func TestDemoEnroll(t *testing.T) {
	r := newDemoRunner()
	ctx := context.Background()
	create := func(args ...string) error {
		_, err := r.Run(ctx, "pvesh", append([]string{"create", "/access/tfa/alice@pve"}, args...)...)
		return err
	}
	if _, err := r.Run(ctx, "pvesh", "create", "/access/tfa", "-userid", "alice@pve", "-type", "totp", "-value", "123456"); err == nil {
		t.Fatal("the user belongs in the path: /access/tfa has no create handler")
	}
	if err := create("-type", "totp", "-value", "000000"); err == nil {
		t.Fatal("000000 must be refused")
	}
	if err := create("-type", "totp", "-value", "123456", "-description", "pve-console"); err != nil {
		t.Fatal(err)
	}
	list, err := r.Run(ctx, "pvesh", "get", "/access/tfa/alice@pve", "--output-format", "json")
	if err != nil || !strings.Contains(string(list.Stdout), "pve-console") {
		t.Fatalf("list = %s, %v", list.Stdout, err)
	}
	reply, err := r.Run(ctx, "pvesh", "create", "/access/tfa/alice@pve", "-type", "recovery", "-description", "x")
	if err != nil || !strings.Contains(string(reply.Stdout), "7f3k-2m9q") {
		t.Fatalf("reply = %s, %v", reply.Stdout, err)
	}
	if err := create("-type", "recovery"); err == nil {
		t.Fatal("a second set of recovery codes must be refused")
	}
	if err := create("-type", "yubico"); err == nil {
		t.Fatal("an unknown type must fail")
	}
}

func TestEnrollQRFitsAThirtyRowTerminal(t *testing.T) {
	m := enrollModel(t, &fakeRunner{})
	m.height = 30
	m, _ = press(t, m, "n")
	if h := m.enroll.qr.height(); h != 19 {
		t.Fatalf("QR height = %d, want 19 (version 4)", h)
	}
	if !strings.Contains(m.View(), "▀") {
		t.Fatal("the QR code must show in 30 rows")
	}
}
