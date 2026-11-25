package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"rsc.io/qr"
)

// qrMargin is the white border around the code, in modules. The standard asks
// for 4; 2 is read by every phone and saves lines in the terminal.
const qrMargin = 2

// qrCode is a QR code ready to be drawn: each line is one terminal row that
// shows two rows of modules.
type qrCode struct {
	lines []string
}

// height is the number of terminal rows of the code.
func (q qrCode) height() int { return len(q.lines) }

// width is the number of terminal columns of the code.
func (q qrCode) width() int {
	if len(q.lines) == 0 {
		return 0
	}
	return textWidth(q.lines[0])
}

// newQR encodes text as a QR code. Modules are black on white whatever the
// theme: a code drawn light on dark is not read by most applications.
func newQR(text string) (qrCode, error) {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		return qrCode{}, fmt.Errorf("unable to encode the QR code: %w", err)
	}
	size := code.Size + 2*qrMargin
	dark := func(x, y int) bool {
		x, y = x-qrMargin, y-qrMargin
		return x >= 0 && y >= 0 && x < code.Size && y < code.Size && code.Black(x, y)
	}
	black, white := lipgloss.Color("#000000"), lipgloss.Color("#ffffff")
	colorOf := func(isDark bool) lipgloss.Color {
		if isDark {
			return black
		}
		return white
	}

	var lines []string
	for y := 0; y < size; y += 2 {
		var row strings.Builder
		for x := 0; x < size; x++ {
			// The upper half block takes the color of the upper module as its
			// foreground and the lower module as its background.
			style := lipgloss.NewStyle().Foreground(colorOf(dark(x, y))).Background(colorOf(dark(x, y+1)))
			row.WriteString(style.Render("▀"))
		}
		lines = append(lines, row.String())
	}

	return qrCode{lines: lines}, nil
}
