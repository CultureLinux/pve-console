package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"pvc/internal/proxmox"
)

// tokenExpiryLabel is the expiry column: a date, "never", or the state when the
// token is expired or about to be.
func tokenExpiryLabel(token proxmox.Token, now time.Time) (string, bool) {
	switch {
	case token.Expire <= 0:
		return "never", false
	case token.Expired(now):
		return "expired", true
	}
	return time.Unix(token.Expire, 0).Format("2006-01-02"), token.ExpiresSoon(now)
}

func (m Model) tokenPanel(width int, height int) string {
	tokens := m.filteredTokens()
	inner := panelInner(width)
	now := time.Now()

	meta := fmt.Sprintf("%d", len(tokens))
	if len(tokens) != len(m.data.tokens) {
		meta = fmt.Sprintf("%d of %d", len(tokens), len(m.data.tokens))
	}

	const expiryW, privW = 10, 4
	commentW := max(0, min(24, inner-1-2-expiryW-1-privW-1-24))
	tokenW := max(10, inner-1-2-expiryW-1-privW-commentW-1)
	if commentW > 0 {
		tokenW = max(10, tokenW-1)
	}
	head := "  " + col("TOKEN", tokenW) + " " + col("EXPIRES", expiryW) + " " + col("PRIV", privW)
	if commentW > 0 {
		head += " " + col("COMMENT", commentW)
	}
	lines := tableHeader(head, inner)

	switch {
	case m.loading && len(m.data.tokens) == 0:
		lines = append(lines, loading("reading tokens"))
	case m.data.tokenErr != nil && len(m.data.tokens) == 0:
		lines = append(lines, styleDanger.Render(ellipsis(firstLine(m.data.tokenErr.Error()), inner)))
	case len(m.data.tokens) == 0:
		lines = append(lines, styleMuted.Render("No API token. Press r to refresh."))
	case len(tokens) == 0:
		lines = append(lines, styleMuted.Render("No token matches the search."), styleMuted.Render("Press c to clear it."))
	default:
		start, end := window(m.selectedToken, len(tokens), panelRows(height)-len(lines))
		for i := start; i < end; i++ {
			token := tokens[i]
			expiry, warn := tokenExpiryLabel(token, now)
			expiryStyle := styleMuted
			switch {
			case token.Expired(now):
				expiryStyle = styleDanger
			case warn:
				expiryStyle = styleWarn
			}
			priv, privStyle := glyphOK, styleOK
			if !bool(token.PrivSep) {
				// The token has every right of its user: worth noticing.
				priv, privStyle = "no", styleWarn
			}
			segs := []seg{
				{glyphDotOn + " ", tokenDotStyle(token, now)},
				{col(token.FullID(), tokenW) + " ", plain},
				{col(expiry, expiryW) + " ", expiryStyle},
				{col(priv, privW), privStyle},
			}
			if commentW > 0 {
				segs = append(segs, seg{" " + col(token.Comment, commentW), styleMuted})
			}
			lines = append(lines, row(segs, inner, i == m.selectedToken))
		}
	}

	return panel("Tokens", styleMuted.Render(meta), lines, width, height, !m.detailFocus)
}

// tokenDotStyle colors the dot of a token: red when expired, amber when it
// expires soon, green otherwise.
func tokenDotStyle(token proxmox.Token, now time.Time) lipgloss.Style {
	switch {
	case token.Expired(now):
		return styleDanger
	case token.ExpiresSoon(now):
		return styleWarn
	}
	return styleOK
}

func (m Model) tokenDetailPanel(width int, height int) string {
	title, lines := m.tokenDetailContent(panelInner(width))
	lines, _, mark := scrollWindow(lines, panelRows(height), m.detailScroll)

	return panel(title, mark, lines, width, height, m.detailFocus)
}

// tokenDetailContent is the whole detail of the selected token, for a panel
// that is inner columns wide. The panel scrolls it when it is too long.
func (m Model) tokenDetailContent(inner int) (string, []string) {
	title := "Details"
	var lines []string
	now := time.Now()

	token, ok := m.selectedTokenModel()
	if !ok {
		lines = append(lines, styleMuted.Render("Select a token to see its details."))
	} else {
		title = token.FullID()
		expiry, _ := tokenExpiryLabel(token, now)
		lines = append(lines, kv("User", token.UserID), kv("Token", token.ID), kv("Expires", expiry))
		if token.Comment != "" {
			lines = append(lines, kv("Comment", ellipsis(token.Comment, inner-9)))
		}
		lines = append(lines, "")
		lines = append(lines, m.tokenRoleLines(token, inner)...)
	}
	if m.data.tokenErr != nil {
		lines = append(lines, "")
		for _, line := range wrapText(strings.TrimSpace(m.data.tokenErr.Error()), inner) {
			lines = append(lines, styleDanger.Render(line))
		}
	}
	if ok {
		lines = append(lines, m.tokenPrivilegeLines(token, inner)...)
	}

	return title, lines
}

// tokenRoleLines lists the roles of a token: its own when it has privilege
// separation, those of its user otherwise (the token inherits them).
func (m Model) tokenRoleLines(token proxmox.Token, inner int) []string {
	var lines []string
	var entries []proxmox.ACLEntry
	if bool(token.PrivSep) {
		lines = append(lines, styleSection.Render("ROLES"))
		entries = proxmox.TokenACLs(m.data.acls, token)
		if len(entries) == 0 {
			return append(lines, styleWarn.Render("None: this token can do nothing."))
		}
	} else {
		lines = append(lines,
			styleWarn.Render("No privilege separation: this token"),
			styleWarn.Render("has every permission of "+token.UserID+"."),
			"",
			styleSection.Render("ROLES OF "+strings.ToUpper(token.UserID)))
		for _, entry := range m.data.acls {
			if entry.Type == "user" && strings.EqualFold(entry.UGID, token.UserID) {
				entries = append(entries, entry)
			}
		}
		if len(entries) == 0 {
			return append(lines, styleMuted.Render("None of its own (maybe through a group)."))
		}
	}
	for _, entry := range entries {
		propagate := ""
		if !bool(entry.Propagate) {
			propagate = styleMuted.Render("  not inherited")
		}
		lines = append(lines, ellipsis(entry.Path+"  "+entry.RoleID, inner)+propagate)
	}

	return lines
}

// tokenPrivilegeLines lists the privileges the token really has, path by path.
func (m Model) tokenPrivilegeLines(token proxmox.Token, inner int) []string {
	permissions, known := m.data.tokenPerms[token.FullID()]
	if !known {
		return nil
	}
	lines := []string{"", styleSection.Render("EFFECTIVE PRIVILEGES")}
	if len(permissions) == 0 {
		return append(lines, styleMuted.Render("None."))
	}
	paths := make([]string, 0, len(permissions))
	for path := range permissions {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		privileges := permissions[path]
		lines = append(lines, styleAccent.Render(ellipsis(path, inner))+styleMuted.Render(fmt.Sprintf("  %d", len(privileges))))
		for _, line := range wrapText(strings.Join(privileges, " "), inner-2) {
			lines = append(lines, styleMuted.Render("  "+line))
		}
	}

	return lines
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return line
}
