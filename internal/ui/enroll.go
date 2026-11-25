package ui

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"pvc/internal/proxmox"
)

// enrollSource is where the TOTP secret comes from. Tests replace it.
var enrollSource io.Reader = rand.Reader

// enrollDescription labels the factors created from here in the web interface.
const enrollDescription = "pve-console"

// enrollChrome is the number of rows the modal needs besides the QR code: the
// frame, the title, the key, the code field, a message and the hints.
const enrollChrome = 11

// enrollPhase is the step of the TOTP enrollment.
type enrollPhase int

const (
	// enrollCode waits for the code shown by the authenticator application.
	enrollCode enrollPhase = iota + 1
	// enrollAdding waits for Proxmox to check the code and save the factor.
	enrollAdding
	// enrollDone is shown once the factor is saved.
	enrollDone
	// enrollCreatingCodes waits for Proxmox to create the recovery codes.
	enrollCreatingCodes
	// enrollRecovery shows the recovery codes, once.
	enrollRecovery
)

// enrollState is the TOTP enrollment in progress. The zero value means none.
type enrollState struct {
	userID   string
	secret   string
	uri      string
	qr       qrCode
	qrErr    error
	code     string
	phase    enrollPhase
	err      error
	recovery []string
}

type enrollMsg struct {
	userid string
	err    error
}

type recoveryMsg struct {
	userid string
	codes  []string
	err    error
}

// startEnroll opens the enrollment of the selected user, with a new secret.
func (m Model) startEnroll() (Model, tea.Cmd) {
	user, ok := m.selectedUserModel()
	if !ok {
		return m, nil
	}
	secret, err := proxmox.NewTOTPSecret(enrollSource)
	if err != nil {
		return m.logError("enroll TOTP of "+user.UserID, err), nil
	}
	uri, err := proxmox.TOTPURI(m.client.TOTPIssuer(), user.UserID, secret)
	if err != nil {
		return m.logError("enroll TOTP of "+user.UserID, err), nil
	}
	state := enrollState{userID: user.UserID, secret: secret, uri: uri, phase: enrollCode}
	// Without a QR code the key can still be typed by hand: not a failure.
	state.qr, state.qrErr = newQR(uri)
	m.enroll = state
	m.modalMode = modalEnroll
	m.message = "enrolling a TOTP for " + user.UserID
	return m, nil
}

// withTFA sets the 2FA indicator of a user in the list, so that the list shows
// what was just done without a refresh. The list is copied, not changed.
func (m Model) withTFA(userid string, enabled bool) Model {
	users := slices.Clone(m.data.users)
	for i := range users {
		if users[i].UserID == userid {
			users[i].TFAEnabled = enabled
		}
	}
	m.data.users = users
	return m
}

// closeEnroll closes the enrollment and forgets the secret.
func (m *Model) closeEnroll() {
	m.enroll = enrollState{}
	m.closeModal()
}

// handleEnrollKey handles the keys of the enrollment modal.
func (m Model) handleEnrollKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	e := m.enroll
	busy := e.phase == enrollAdding || e.phase == enrollCreatingCodes
	switch msg.String() {
	case "esc":
		if !busy {
			m.closeEnroll()
		}
	case "backspace":
		if e.phase == enrollCode && len(e.code) > 0 {
			m.enroll.code = e.code[:len(e.code)-1]
			m.enroll.err = nil
		}
	case "enter":
		switch e.phase {
		case enrollCode:
			if !proxmox.ValidTOTPCode(e.code) {
				m.enroll.err = fmt.Errorf("type the 6 digits shown by the application")
				return m, nil
			}
			m.enroll.phase = enrollAdding
			m.enroll.err = nil
			m.message = "checking the code for " + e.userID
			return m, m.addTOTP(e.userID, e.uri, e.code)
		case enrollDone, enrollRecovery:
			m.closeEnroll()
		}
	case "c":
		if e.phase == enrollDone {
			m.enroll.phase = enrollCreatingCodes
			m.enroll.err = nil
			m.message = "creating recovery codes for " + e.userID
			return m, m.addRecoveryCodes(e.userID)
		}
	default:
		if e.phase == enrollCode && len(msg.Runes) == 1 && msg.Runes[0] >= '0' && msg.Runes[0] <= '9' && len(e.code) < 6 {
			m.enroll.code = e.code + string(msg.Runes)
			m.enroll.err = nil
		}
	}
	return m, nil
}

func (m Model) addTOTP(userid string, uri string, code string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), shortActionTimeout)
		defer cancel()
		return enrollMsg{userid: userid, err: m.client.AddTOTP(ctx, userid, uri, code, enrollDescription)}
	}
}

func (m Model) addRecoveryCodes(userid string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), shortActionTimeout)
		defer cancel()
		codes, err := m.client.AddRecoveryCodes(ctx, userid, enrollDescription)
		return recoveryMsg{userid: userid, codes: codes, err: err}
	}
}

// applyEnroll applies the result of the TOTP enrollment.
func (m Model) applyEnroll(msg enrollMsg) (Model, tea.Cmd) {
	if m.modalMode != modalEnroll || m.enroll.userID != msg.userid || m.enroll.phase != enrollAdding {
		return m, nil
	}
	if msg.err != nil {
		// The secret is still good: the user can type another code.
		m.enroll.phase = enrollCode
		m.enroll.code = ""
		m.enroll.err = msg.err
		m.message = "TOTP refused for " + msg.userid
		return m.logError("enroll TOTP of "+msg.userid, msg.err), nil
	}
	m.enroll.phase = enrollDone
	m.enroll.err = nil
	m.enroll.secret, m.enroll.uri, m.enroll.qr = "", "", qrCode{}
	m.message = "TOTP enrolled for " + msg.userid
	m = m.logDone("enroll TOTP of "+msg.userid, "done")

	m = m.withTFA(msg.userid, true)
	m.tfaUserID, m.tfaEntries, m.tfaErr = msg.userid, nil, nil
	return m, m.loadUserTFA(msg.userid)
}

// applyRecovery applies the result of the creation of the recovery codes.
func (m Model) applyRecovery(msg recoveryMsg) Model {
	if m.modalMode != modalEnroll || m.enroll.userID != msg.userid || m.enroll.phase != enrollCreatingCodes {
		return m
	}
	if msg.err != nil {
		m.enroll.phase = enrollDone
		m.enroll.err = msg.err
		m.message = "recovery codes refused for " + msg.userid
		return m.logError("create recovery codes of "+msg.userid, msg.err)
	}
	m.enroll.phase = enrollRecovery
	m.enroll.recovery = msg.codes
	m.enroll.err = nil
	m.message = "recovery codes created for " + msg.userid
	// The codes themselves never go to the log.
	return m.logDone("create recovery codes of "+msg.userid, "done")
}

// enrollHints are the key hints of the enrollment modal.
func (m Model) enrollHints() []hint {
	switch m.enroll.phase {
	case enrollCode:
		return []hint{{"0-9", "code"}, {"enter", "verify"}, {"esc", "cancel"}}
	case enrollDone:
		return []hint{{"c", "recovery codes"}, {"enter", "close"}}
	case enrollRecovery:
		return []hint{{"enter", "close"}}
	}
	return []hint{{"…", "please wait"}}
}

// groupKey splits a secret in groups of 4 characters, easier to type by hand.
func groupKey(secret string) string {
	var groups []string
	for len(secret) > 4 {
		groups = append(groups, secret[:4])
		secret = secret[4:]
	}
	return strings.Join(append(groups, secret), " ")
}

// enrollModal draws the TOTP enrollment.
func (m Model) enrollModal(width int, height int) string {
	e := m.enroll
	w := modalWidth(width, 60, 46)
	inner := w - 6
	var body, footer []string

	switch e.phase {
	case enrollCode, enrollAdding:
		if e.qrErr == nil && e.qr.height() > 0 && e.qr.width() <= inner && height >= e.qr.height()+enrollChrome {
			body = append(body, e.qr.lines...)
		} else {
			body = append(body, styleWarn.Render("No room for the QR code: enlarge the window,"), styleWarn.Render("or type the key by hand."), "")
		}
		body = append(body, kv("Key", groupKey(e.secret)))
		switch {
		case e.phase == enrollAdding:
			body = append(body, "", styleMuted.Render("Checking the code…"))
		case e.err != nil:
			body = append(body, field("Code", e.code, true, 8), styleDanger.Render(ellipsis(oneLine(e.err.Error()), inner)))
		default:
			body = append(body, field("Code", e.code, true, 8), styleMuted.Render("Scan it, type the 6 digits. The key is on screen."))
		}
	case enrollDone:
		body = append(body, styleOK.Render(glyphOK+" TOTP enrolled."), "",
			styleMuted.Render("Recovery codes let the user in when the phone is lost."),
			styleMuted.Render("Proxmox keeps one set per user, shown once."))
		if e.err != nil {
			body = append(body, "", styleDanger.Render(ellipsis(oneLine(e.err.Error()), inner)))
		}
	case enrollCreatingCodes:
		body = append(body, styleMuted.Render("Creating the recovery codes…"))
	case enrollRecovery:
		body = append(body, styleWarn.Render("Shown once: copy them now."), "")
		for _, code := range e.recovery {
			body = append(body, "  "+styleKey.Render(code))
		}
	}

	for _, h := range m.enrollHints() {
		footer = append(footer, hintKey(h.key, h.label))
	}
	return modalFrame(modalKindInfo, "Enroll TOTP", e.userID, body, []string{strings.Join(footer, "   ")}, w)
}
