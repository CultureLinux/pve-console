package ui

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"pvc/internal/proxmox"
)

// passwdState is the change of a password in progress. The zero value means none.
type passwdState struct {
	userID  string
	realm   string
	pass    string
	confirm string
	field   int // 0: new password, 1: confirmation
	saving  bool
	err     error
}

type passwdMsg struct {
	userid string
	err    error
}

// startPasswd opens the password change of the selected user.
func (m Model) startPasswd() Model {
	user, ok := m.selectedUserModel()
	if !ok {
		return m
	}
	realm, ok := proxmox.PasswordRealm(user.UserID)
	if !ok {
		m.message = fmt.Sprintf("the password of realm %q is not managed by Proxmox", realm)
		return m
	}
	m.passwd = passwdState{userID: user.UserID, realm: realm}
	m.modalMode = modalPasswd
	m.message = "changing the password of " + user.UserID
	return m
}

// closePasswd closes the password change and forgets what was typed.
func (m *Model) closePasswd() {
	m.passwd = passwdState{}
	m.closeModal()
}

// handlePasswdKey handles the keys of the password modal.
func (m Model) handlePasswdKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	p := m.passwd
	if p.saving {
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.closePasswd()
	case "tab", "down", "up", "shift+tab":
		m.passwd.field = 1 - p.field
	case "backspace":
		if p.field == 0 {
			m.passwd.pass = dropLastRune(p.pass)
		} else {
			m.passwd.confirm = dropLastRune(p.confirm)
		}
		m.passwd.err = nil
	case "enter":
		return m.submitPasswd()
	default:
		// A paste arrives as several runes at once. Control characters are dropped.
		typed := strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, string(msg.Runes))
		if typed == "" {
			return m, nil
		}
		if p.field == 0 {
			m.passwd.pass = p.pass + typed
		} else {
			m.passwd.confirm = p.confirm + typed
		}
		m.passwd.err = nil
	}
	return m, nil
}

func dropLastRune(text string) string {
	_, size := utf8.DecodeLastRuneInString(text)
	return text[:len(text)-size]
}

// submitPasswd moves to the confirmation, then checks both entries and sends the change.
func (m Model) submitPasswd() (Model, tea.Cmd) {
	p := m.passwd
	if err := proxmox.ValidatePassword(p.pass); err != nil {
		m.passwd.field = 0
		m.passwd.err = err
		return m, nil
	}
	if p.field == 0 {
		m.passwd.field = 1
		return m, nil
	}
	if p.pass != p.confirm {
		m.passwd.err = fmt.Errorf("the two passwords differ")
		m.passwd.confirm = ""
		return m, nil
	}
	m.passwd.saving = true
	m.passwd.err = nil
	m.message = "changing the password of " + p.userID
	return m, m.setPassword(p.userID, p.pass)
}

func (m Model) setPassword(userid string, password string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), shortActionTimeout)
		defer cancel()
		return passwdMsg{userid: userid, err: m.client.SetPassword(ctx, userid, password)}
	}
}

// applyPasswd applies the result of the password change.
func (m Model) applyPasswd(msg passwdMsg) Model {
	if m.modalMode != modalPasswd || m.passwd.userID != msg.userid || !m.passwd.saving {
		return m
	}
	if msg.err != nil {
		// What was typed stays, so that a typo elsewhere is not a reason to start over.
		m.passwd.saving = false
		m.passwd.err = msg.err
		m.message = "password refused for " + msg.userid
		return m.logError("change password of "+msg.userid, msg.err)
	}
	m.closePasswd()
	m.message = "password changed for " + msg.userid
	return m.logDone("change password of "+msg.userid, "done")
}

// passwdHints are the key hints of the password modal.
func (m Model) passwdHints() []hint {
	if m.passwd.saving {
		return []hint{{"…", "please wait"}}
	}
	return []hint{{"enter", "next / apply"}, {"tab", "switch field"}, {"esc", "cancel"}}
}

// masked draws a typed password as dots, with its length.
func masked(password string) string {
	count := utf8.RuneCountInString(password)
	return strings.Repeat("•", count)
}

// passwdModal draws the password change.
func (m Model) passwdModal(width int) string {
	p := m.passwd
	w := modalWidth(width, 62, 46)
	fieldW := max(10, w-4-10)
	body := []string{
		kv("User", p.userID),
		"",
		field("New", masked(p.pass), p.field == 0 && !p.saving, fieldW),
		field("Confirm", masked(p.confirm), p.field == 1 && !p.saving, fieldW),
		"",
	}
	switch {
	case p.saving:
		body = append(body, styleMuted.Render("Changing the password…"))
	case p.err != nil:
		body = append(body, styleDanger.Render(ellipsis(oneLine(p.err.Error()), w-6)))
	default:
		body = append(body, styleMuted.Render(fmt.Sprintf("8 to 64 characters. Typed: %d / %d.", utf8.RuneCountInString(p.pass), utf8.RuneCountInString(p.confirm))))
	}
	if p.realm == "pam" {
		body = append(body, styleWarn.Render("Realm pam: the Linux account of this node only."))
	}
	var footer []string
	for _, h := range m.passwdHints() {
		footer = append(footer, hintKey(h.key, h.label))
	}
	return modalFrame(modalKindWarn, "Change password", "", body, []string{strings.Join(footer, "   ")}, w)
}
