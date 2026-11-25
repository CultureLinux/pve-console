package ui

import (
	"fmt"
	"strings"
)

func (m Model) userPanel(width int, height int) string {
	users := m.filteredUsers()
	inner := panelInner(width)

	meta := fmt.Sprintf("%d", len(users))
	if len(users) != len(m.data.users) {
		meta = fmt.Sprintf("%d of %d", len(users), len(m.data.users))
	}

	const tfaW = 4
	emailW := max(0, min(28, inner-1-2-tfaW-1-20))
	userW := max(8, inner-1-2-tfaW-1-emailW-1)
	head := "  " + col("USER", userW) + " " + col("2FA", tfaW)
	if emailW > 0 {
		head += " " + col("EMAIL", emailW)
	}
	lines := tableHeader(head, inner)

	switch {
	case m.loading && len(m.data.users) == 0:
		lines = append(lines, loading("reading users"))
	case len(m.data.users) == 0:
		lines = append(lines, styleMuted.Render("No users loaded. Press r to refresh."))
	case len(users) == 0:
		lines = append(lines, styleMuted.Render("No user matches the search."), styleMuted.Render("Press c to clear it."))
	default:
		start, end := window(m.selectedUser, len(users), panelRows(height)-len(lines))
		for i := start; i < end; i++ {
			user := users[i]
			dot, dotStyle := glyphDotOn, styleOK
			if !bool(user.Enable) {
				dot, dotStyle = glyphDotOff, styleMuted
			}
			tfa, tfaStyle := "-", styleMuted
			if user.TFAEnabled {
				tfa, tfaStyle = glyphOK, styleOK
			}
			segs := []seg{
				{dot + " ", dotStyle},
				{col(user.UserID, userW) + " ", plain},
				{col(tfa, tfaW), tfaStyle},
			}
			if emailW > 0 {
				segs = append(segs, seg{" " + col(user.Email, emailW), styleMuted})
			}
			lines = append(lines, row(segs, inner, i == m.selectedUser))
		}
	}

	return panel("Users", styleMuted.Render(meta), lines, width, height, true)
}

func (m Model) userDetailPanel(width int, height int) string {
	inner := panelInner(width)
	title := "Details"
	var lines []string

	if user, ok := m.selectedUserModel(); ok {
		title = user.UserID
		state, stateStyle := "enabled", styleOK
		if !bool(user.Enable) {
			state, stateStyle = "disabled", styleMuted
		}
		tfa := styleMuted.Render("no 2FA")
		if user.TFAEnabled {
			tfa = styleOK.Render(glyphOK + " 2FA")
		}
		lines = append(lines, stateStyle.Render(state)+styleMuted.Render("  "+glyphSep+"  ")+tfa, "")
		if user.Email != "" {
			lines = append(lines, kv("Email", ellipsis(user.Email, inner-9)))
		}
		if name := strings.TrimSpace(user.FirstName + " " + user.LastName); name != "" {
			lines = append(lines, kv("Name", ellipsis(name, inner-9)))
		}
		if user.Comment != "" {
			lines = append(lines, kv("Comment", ellipsis(user.Comment, inner-9)))
		}
	} else {
		lines = append(lines, styleMuted.Render("Select a user to see its details."))
	}

	lines = append(lines, "", styleSection.Render("TWO-FACTOR ENTRIES"))
	switch {
	case m.tfaErr != nil:
		lines = append(lines, styleDanger.Render(m.tfaErr.Error()))
	case m.tfaUserID == "":
		lines = append(lines, styleMuted.Render("Press s to load them."))
	case len(m.tfaEntries) == 0:
		lines = append(lines, styleMuted.Render("None for "+m.tfaUserID+"."))
	default:
		for _, entry := range m.tfaEntries {
			lines = append(lines, ellipsis(strings.TrimSpace(fmt.Sprintf("%s  %s", entry.Type, entry.Description)), inner))
		}
	}

	return panel(title, "", lines, width, height, false)
}
