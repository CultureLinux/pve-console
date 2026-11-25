package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"pvc/internal/command"
	"pvc/internal/proxmox"
)

const testPassword = "correct-horse-battery"

func passwdModel(runner *fakeRunner) Model {
	m := newTestModel(runner)
	m.width, m.height = 120, 40
	m.viewMode = viewUsers
	m.data.users = []proxmox.User{{UserID: "alice@pve"}, {UserID: "root@pam"}, {UserID: "carl@ldap"}}
	return m
}

func typeText(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		m, _ = press(t, m, string(r))
	}
	return m
}

func TestPasswdFullFlow(t *testing.T) {
	runner := &fakeRunner{}
	m := passwdModel(runner)

	m, cmd := press(t, m, "P")
	if cmd != nil || m.modalMode != modalPasswd || m.passwd.userID != "alice@pve" || m.passwd.realm != "pve" {
		t.Fatalf("mode=%q passwd=%+v", m.modalMode, m.passwd)
	}
	m = typeText(t, m, testPassword)
	m, _ = press(t, m, "enter")
	if m.passwd.field != 1 {
		t.Fatalf("field = %d: enter goes to the confirmation", m.passwd.field)
	}
	m = typeText(t, m, testPassword)
	screen := m.View()
	for _, want := range []string{"Change password", "alice@pve", strings.Repeat("•", len(testPassword)), "21 / 21"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("screen lacks %q:\n%s", want, screen)
		}
	}
	if strings.Contains(screen, testPassword) || strings.Contains(screen, "correct") {
		t.Fatalf("the password is on screen:\n%s", screen)
	}

	m, cmd = press(t, m, "enter")
	if cmd == nil || !m.passwd.saving {
		t.Fatalf("saving=%t cmd=%t", m.passwd.saving, cmd != nil)
	}
	if got, _ := press(t, m, "esc", "x", "enter"); got.modalMode != modalPasswd || got.passwd.pass != testPassword {
		t.Fatal("keys are ignored while Proxmox changes the password")
	}
	m, _ = execute(t, m, cmd)
	if !runner.called("pvesh set /access/password -userid alice@pve -password " + testPassword) {
		t.Fatalf("calls = %v", runner.calls)
	}
	if m.modalMode != modalNone || m.passwd.pass != "" || m.passwd.confirm != "" || m.passwd.userID != "" {
		t.Fatalf("mode=%q passwd=%+v: the password must be forgotten", m.modalMode, m.passwd)
	}
	if m.message != "password changed for alice@pve" {
		t.Fatalf("message = %q", m.message)
	}
	if log := fmt.Sprint(m.entries); strings.Contains(log, testPassword) || !strings.Contains(log, "change password of alice@pve") {
		t.Fatalf("log = %s", log)
	}
}

func TestPasswdChecksBeforeSending(t *testing.T) {
	runner := &fakeRunner{}
	m := passwdModel(runner)
	m, _ = press(t, m, "P")

	// Too short: stays on the first field.
	m = typeText(t, m, "short")
	m, cmd := press(t, m, "enter")
	if cmd != nil || m.passwd.err == nil || m.passwd.field != 0 {
		t.Fatalf("passwd = %+v", m.passwd)
	}
	m, _ = press(t, m, "backspace", "backspace", "backspace", "backspace", "backspace")
	if m.passwd.pass != "" || m.passwd.err != nil {
		t.Fatalf("passwd = %+v", m.passwd)
	}

	// Two different passwords.
	m = typeText(t, m, testPassword)
	m, _ = press(t, m, "tab")
	m = typeText(t, m, testPassword+"x")
	m, cmd = press(t, m, "enter")
	if cmd != nil || m.passwd.err == nil || m.passwd.confirm != "" || m.passwd.pass != testPassword {
		t.Fatalf("passwd = %+v", m.passwd)
	}
	if screen := m.View(); !strings.Contains(screen, "differ") {
		t.Fatalf("the mismatch is not shown:\n%s", screen)
	}
	if runner.called("/access/password") {
		t.Fatal("nothing must reach Proxmox")
	}

	// Tab and the arrows switch fields.
	m, _ = press(t, m, "tab")
	if m.passwd.field != 0 {
		t.Fatal("tab goes back to the first field")
	}
	m, _ = press(t, m, "down")
	if m.passwd.field != 1 {
		t.Fatal("down goes to the confirmation")
	}
}

func TestPasswdKeepsWhatWasTypedWhenRefused(t *testing.T) {
	secretArgs := []string{"set", "/access/password", "-userid", "alice@pve", "-password", testPassword}
	runner := &fakeRunner{failing: map[string]error{"/access/password": command.CommandError{
		Result: command.Result{Command: "pvesh", Args: secretArgs, ExitCode: 255, Stderr: []byte("400 Parameter verification failed.\npassword: '" + testPassword + "' is refused")},
		Err:    errors.New("exit status 255"),
	}}}
	m := passwdModel(runner)
	m, _ = press(t, m, "P")
	m = typeText(t, m, testPassword)
	m, _ = press(t, m, "enter")
	m = typeText(t, m, testPassword)
	m, cmd := press(t, m, "enter")
	m, _ = execute(t, m, cmd)

	if m.modalMode != modalPasswd || m.passwd.saving || m.passwd.err == nil || m.passwd.pass != testPassword {
		t.Fatalf("mode=%q passwd=%+v", m.modalMode, m.passwd)
	}
	for name, text := range map[string]string{
		"log":    fmt.Sprintf("%+v", m.entries),
		"modal":  m.View(),
		"error":  fmt.Sprint(m.passwd.err),
		"status": m.message,
	} {
		if strings.Contains(text, testPassword) {
			t.Errorf("the password is in the %s: %s", name, text)
		}
	}
	if log := fmt.Sprintf("%+v", m.entries); !strings.Contains(log, "-password ***") {
		t.Errorf("the log should show the masked command: %s", log)
	}

	// Another try works without typing everything again.
	runner.failing = nil
	m, cmd = press(t, m, "enter")
	m, _ = execute(t, m, cmd)
	if m.modalMode != modalNone {
		t.Fatalf("mode = %q", m.modalMode)
	}
}

func TestPasswdRealms(t *testing.T) {
	m := passwdModel(&fakeRunner{})
	m.selectedUser = 1
	m, _ = press(t, m, "P")
	if m.modalMode != modalPasswd || m.passwd.realm != "pam" {
		t.Fatalf("mode=%q", m.modalMode)
	}
	if screen := m.View(); !strings.Contains(screen, "Linux account") {
		t.Fatalf("the pam warning is missing:\n%s", screen)
	}
	m, _ = press(t, m, "esc")

	m.selectedUser = 2
	m, _ = press(t, m, "P")
	if m.modalMode != modalNone || !strings.Contains(m.message, "ldap") {
		t.Fatalf("mode=%q message=%q", m.modalMode, m.message)
	}
}

func TestPasswdOnlyInTheUsersView(t *testing.T) {
	m := passwdModel(&fakeRunner{})
	m.viewMode = viewGuests
	m, _ = press(t, m, "P")
	if m.modalMode != modalNone {
		t.Fatalf("mode = %q", m.modalMode)
	}
	m.viewMode = viewUsers
	m.data.users = nil
	m, _ = press(t, m, "P")
	if m.modalMode != modalNone {
		t.Fatalf("mode = %q", m.modalMode)
	}
}

func TestPasswdTyping(t *testing.T) {
	m := passwdModel(&fakeRunner{})
	m, _ = press(t, m, "P")

	// Spaces and accents are characters; a paste arrives in one message.
	m = typeText(t, m, "é a")
	m, _ = press(t, m, "backspace", "backspace")
	if m.passwd.pass != "é" {
		t.Fatalf("pass = %q", m.passwd.pass)
	}
	m, _ = deliver(t, m, keyMsg("pasted-text\n"))
	if m.passwd.pass != "épasted-text" {
		t.Fatalf("pass = %q: control characters are dropped", m.passwd.pass)
	}
	m, _ = press(t, m, "esc")
	if m.modalMode != modalNone || m.passwd.pass != "" {
		t.Fatalf("mode=%q passwd=%+v: cancel forgets the password", m.modalMode, m.passwd)
	}
}

func TestPasswdLateMessagesAreIgnored(t *testing.T) {
	m := passwdModel(&fakeRunner{})
	got, _ := deliver(t, m, passwdMsg{userid: "alice@pve"})
	if got.modalMode != modalNone || got.message != m.message {
		t.Fatalf("mode=%q message=%q", got.modalMode, got.message)
	}
	m, _ = press(t, m, "P")
	got, _ = deliver(t, m, passwdMsg{userid: "alice@pve"})
	if got.modalMode != modalPasswd {
		t.Fatal("an answer without a pending change must be ignored")
	}
	m.passwd.saving = true
	got, _ = deliver(t, m, passwdMsg{userid: "root@pam"})
	if got.modalMode != modalPasswd || !got.passwd.saving {
		t.Fatal("the answer of another user must be ignored")
	}
}

func TestPasswdModalFitsTheScreen(t *testing.T) {
	for _, size := range [][2]int{{60, 14}, {80, 24}, {120, 40}} {
		m := passwdModel(&fakeRunner{})
		m.width, m.height = size[0], size[1]
		m, _ = press(t, m, "P")
		m = typeText(t, m, testPassword)
		lines := strings.Split(m.View(), "\n")
		if len(lines) != size[1] {
			t.Errorf("%dx%d: %d lines", size[0], size[1], len(lines))
		}
		for i, line := range lines {
			if got := textWidth(line); got != size[0] {
				t.Errorf("%dx%d: line %d is %d columns wide", size[0], size[1], i, got)
				break
			}
		}
	}
}

func TestPasswdHints(t *testing.T) {
	m := passwdModel(&fakeRunner{})
	m, _ = press(t, m, "P")
	if hints, _ := m.hints(); hints[0].key != "enter" {
		t.Fatalf("hints = %v", hints)
	}
	m.passwd.saving = true
	if hints, _ := m.hints(); hints[0].key != "…" {
		t.Fatalf("hints = %v", hints)
	}
}

func TestDemoPasswd(t *testing.T) {
	m := New(proxmox.Client{Runner: newDemoRunner(), Node: "pve"})
	if err := m.client.SetPassword(t.Context(), "alice@pve", testPassword); err != nil {
		t.Fatal(err)
	}
	if _, err := newDemoRunner().Run(t.Context(), "pvesh", "set", "/access/password", "-userid", "alice@pve"); err == nil {
		t.Fatal("a missing password must be refused")
	}
}

// failingRunner fails like pvesh does: the error carries the arguments it received.
type failingRunner struct{ stderr string }

func (r failingRunner) Run(_ context.Context, name string, args ...string) (command.Result, error) {
	result := command.Result{Command: name, Args: args, ExitCode: 255, Stderr: []byte(r.stderr)}
	return result, command.CommandError{Result: result, Err: errors.New("exit status 255")}
}

// A refused TOTP must not leave its secret or its code in the log either.
func TestEnrollRefusedLeavesNoSecretInTheLog(t *testing.T) {
	m := enrollModel(t, &fakeRunner{})
	m.client = proxmox.Client{Runner: failingRunner{stderr: "value: invalid totp token 123456"}, Node: "pve"}
	m, _ = press(t, m, "n")
	m = typeDigits(t, m, "123456")
	m, cmd := press(t, m, "enter")
	m, _ = execute(t, m, cmd)

	if m.enroll.phase != enrollCode || m.enroll.err == nil {
		t.Fatalf("enroll = %+v", m.enroll)
	}
	log := fmt.Sprintf("%+v", m.entries)
	if !strings.Contains(log, "-type totp") || !strings.Contains(log, "-description pve-console") {
		t.Fatalf("the log should still show the command: %s", log)
	}
	for _, secret := range []string{testSecret, "123456"} {
		if strings.Contains(log, secret) || strings.Contains(m.message, secret) || strings.Contains(fmt.Sprint(m.enroll.err), secret) {
			t.Errorf("%q reached the log or the status row: %s", secret, log)
		}
	}
}
