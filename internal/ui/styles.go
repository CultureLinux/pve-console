package ui

import (
	"github.com/charmbracelet/lipgloss"
)

// Palette. Every color is adaptive (light or dark terminal background) and the
// regular text keeps the terminal's own foreground and background: backgrounds
// are only used for badges, buttons and the selected row, so the TUI follows
// the user's terminal theme. Contrast ratios are checked in styles_test.go.
var (
	colAccent   = lipgloss.AdaptiveColor{Light: "#2F5BEA", Dark: "#7AA2F7"}
	colOnAccent = lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#0B1020"}
	colBrand    = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#F08A24"}
	colOnBrand  = lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#1A0E00"}
	colOK       = lipgloss.AdaptiveColor{Light: "#176A31", Dark: "#9ECE6A"}
	colWarn     = lipgloss.AdaptiveColor{Light: "#8F5400", Dark: "#E0AF68"}
	colDanger   = lipgloss.AdaptiveColor{Light: "#C0262D", Dark: "#F7768E"}
	colMuted    = lipgloss.AdaptiveColor{Light: "#5B6478", Dark: "#959DB3"}
	colBorder   = lipgloss.AdaptiveColor{Light: "#C4C9D4", Dark: "#3B4261"}
	colSelected = lipgloss.AdaptiveColor{Light: "#E6EBFF", Dark: "#2A3150"}
)

// Glyphs. All of them are common Unicode (no icon font needed).
const (
	glyphDotOn     = "●"
	glyphDotOff    = "○"
	glyphDotOther  = "◐"
	glyphBarFull   = "━"
	glyphBarEmpty  = "─"
	glyphSelection = "▌"
	glyphCursor    = "▏"
	glyphOK        = "✓"
	glyphFail      = "✗"
	glyphWarn      = "⚠"
	glyphInfo      = "•"
	glyphSep       = "·"
	glyphEnter     = "⏎"
)

var (
	styleMuted  = lipgloss.NewStyle().Foreground(colMuted)
	styleAccent = lipgloss.NewStyle().Foreground(colAccent)
	styleBold   = lipgloss.NewStyle().Bold(true)
	styleOK     = lipgloss.NewStyle().Foreground(colOK)
	styleWarn   = lipgloss.NewStyle().Foreground(colWarn)
	styleDanger = lipgloss.NewStyle().Foreground(colDanger)
	styleBorder = lipgloss.NewStyle().Foreground(colBorder)
	styleFocus  = lipgloss.NewStyle().Foreground(colAccent)
	styleDim    = lipgloss.NewStyle().Foreground(colMuted).Faint(true)

	// Keys in hints and in the help are bold in the accent color.
	styleKey = lipgloss.NewStyle().Bold(true).Foreground(colAccent)

	// Section titles inside panels.
	styleSection = lipgloss.NewStyle().Bold(true).Foreground(colMuted)
	// Column headers of the tables.
	styleTableHead = lipgloss.NewStyle().Foreground(colMuted)

	styleBrandBadge = lipgloss.NewStyle().Bold(true).Foreground(colOnBrand).Background(colBrand).Padding(0, 1)
	styleTabActive  = lipgloss.NewStyle().Bold(true).Foreground(colOnAccent).Background(colAccent).Padding(0, 1)
	styleTabIdle    = lipgloss.NewStyle().Padding(0, 1)

	styleSelected = lipgloss.NewStyle().Background(colSelected)
)

// buttonKind picks the color of a modal button.
type buttonKind int

const (
	buttonNeutral buttonKind = iota
	buttonPrimary
	buttonWarn
	buttonDanger
)

func (k buttonKind) color() lipgloss.AdaptiveColor {
	switch k {
	case buttonWarn:
		return colWarn
	case buttonDanger:
		return colDanger
	}
	return colAccent
}

// onColor is the text color to use on a filled badge of the given color.
func onColor(c lipgloss.AdaptiveColor) lipgloss.AdaptiveColor {
	switch c {
	case colWarn, colDanger, colOK:
		return lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#0B1020"}
	}
	return colOnAccent
}
