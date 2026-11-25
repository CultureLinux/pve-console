package ui

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

// sgrPattern matches the SGR escape sequences (colors, bold...) lipgloss emits.
var sgrPattern = regexp.MustCompile(`^\x1b\[[0-9;:]*m`)

// stripANSI removes the SGR escape sequences of s.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			if loc := sgrPattern.FindStringIndex(s[i:]); loc != nil {
				i += loc[1]
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		b.WriteRune(r)
		i += size
	}
	return b.String()
}

// textWidth is the number of terminal columns of s, escape sequences excluded.
func textWidth(s string) int {
	return lipgloss.Width(s)
}

// cutColumns returns the columns [from, to) of s. Escape sequences are kept, so
// the colors that were active at `from` still apply, and the result is closed
// with a reset when it ends inside a style.
func cutColumns(s string, from int, to int) string {
	if to <= from {
		return ""
	}
	var out strings.Builder
	var active []string
	col := 0
	started := false

	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			if loc := sgrPattern.FindStringIndex(s[i:]); loc != nil {
				seq := s[i : i+loc[1]]
				i += loc[1]
				if seq == "\x1b[0m" || seq == "\x1b[m" {
					active = active[:0]
				} else {
					active = append(active, seq)
				}
				if started {
					out.WriteString(seq)
				}
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		w := lipgloss.Width(string(r))
		if col >= to {
			break
		}
		if col+w > to {
			break
		}
		if col < from {
			col += w
			if col > from {
				// A wide rune straddles the left edge: fill the gap.
				if !started {
					started = true
					out.WriteString(strings.Join(active, ""))
				}
				out.WriteString(strings.Repeat(" ", col-from))
			}
			continue
		}
		if !started {
			started = true
			out.WriteString(strings.Join(active, ""))
		}
		out.WriteRune(r)
		col += w
	}
	if len(active) > 0 {
		out.WriteString("\x1b[0m")
	}
	return out.String()
}

// fit truncates s to width columns (with an ellipsis) or pads it with spaces.
func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := textWidth(s)
	switch {
	case w == width:
		return s
	case w < width:
		return s + strings.Repeat(" ", width-w)
	case width == 1:
		return "…"
	}
	return cutColumns(s, 0, width-1) + "…"
}

// ellipsis shortens plain text to at most width columns.
func ellipsis(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if textWidth(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return cutColumns(s, 0, width-1) + "…"
}

// rightAlign puts right at the right edge of a width-column line after left.
// When both do not fit, right is dropped.
func rightAlign(left string, right string, width int) string {
	gap := width - textWidth(left) - textWidth(right)
	if gap < 1 {
		return fit(left, width)
	}
	return left + strings.Repeat(" ", gap) + right
}

// panel draws a rounded box of exactly width x height columns/rows with the
// title in the top border. meta, already styled, follows the title. lines are the body; extra lines are cut and missing
// ones are blank. The focused panel gets the accent color.
func panel(title string, meta string, lines []string, width int, height int, focused bool) string {
	if width < 6 || height < 3 {
		return ""
	}
	border := styleBorder
	if focused {
		border = styleFocus
	}
	inner := width - 4

	head := ""
	if title != "" {
		head = " " + styleBold.Render(ellipsis(title, max(1, width-8))) + " "
		if meta != "" {
			room := width - 6 - textWidth(head)
			if room > 2 {
				head += ellipsis(meta, room-1) + " "
			}
		}
	}
	top := border.Render("╭─") + head + border.Render(strings.Repeat("─", max(0, width-3-textWidth(head)))+"╮")

	rows := make([]string, 0, height)
	rows = append(rows, top)
	for i := 0; i < height-2; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		rows = append(rows, border.Render("│")+" "+fit(line, inner)+" "+border.Render("│"))
	}
	rows = append(rows, border.Render("╰"+strings.Repeat("─", width-2)+"╯"))
	return strings.Join(rows, "\n")
}

// panelRows is the number of body rows of a panel of the given height.
func panelRows(height int) int { return max(0, height-2) }

// panelInner is the width available to the body lines of a panel.
func panelInner(width int) int { return max(0, width-4) }

// window returns the slice [start, end) of count items to show in rows rows so
// that selected stays visible.
func window(selected int, count int, rows int) (int, int) {
	if rows < 1 {
		rows = 1
	}
	start := 0
	if selected >= rows {
		start = selected - rows + 1
	}
	return start, min(count, start+rows)
}

// usageColor picks the color of a usage ratio.
func usageStyle(ratio float64) lipgloss.Style {
	switch {
	case ratio >= 0.90:
		return styleDanger
	case ratio >= 0.80:
		return styleWarn
	}
	return styleAccent
}

// bar draws a usage bar of width columns for a ratio between 0 and 1.
func bar(ratio float64, width int) string {
	if width < 1 {
		return ""
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio*float64(width) + 0.5)
	return usageStyle(ratio).Render(strings.Repeat(glyphBarFull, filled)) + styleBorder.Render(strings.Repeat(glyphBarEmpty, width-filled))
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinner returns the current frame of a braille spinner.
func spinner() string {
	frame := int(time.Now().UnixMilli()/90) % len(spinnerFrames)
	return styleAccent.Render(spinnerFrames[frame])
}

var petFaces = []string{"(o_o)", "(O_o)", "(o_O)", "(^_^)"}

// petFace is the little face that keeps the project's loader signature.
func petFace(frame int) string {
	if frame < 0 {
		frame = 0
	}
	return petFaces[frame%len(petFaces)]
}

// loading renders an animated "working" line.
func loading(label string) string {
	frame := int(time.Now().UnixMilli() / 350)
	return spinner() + " " + styleMuted.Render(petFace(frame)+" "+label+"…")
}

// hintKey renders a key and what it does, for the footer and the modals.
func hintKey(key string, label string) string {
	return styleKey.Render(key) + " " + styleMuted.Render(label)
}

// button renders a modal button. A focused button is filled, the others are plain.
func button(label string, kind buttonKind, focused bool) string {
	label = " " + label + " "
	switch {
	case focused && kind == buttonNeutral:
		return styleBold.Background(colSelected).Render(label)
	case focused:
		c := kind.color()
		return lipgloss.NewStyle().Bold(true).Foreground(onColor(c)).Background(c).Render(label)
	case kind == buttonNeutral:
		return styleMuted.Render(label)
	}
	return lipgloss.NewStyle().Foreground(kind.color()).Render(label)
}

// seg is a styled piece of a table row.
type seg struct {
	text  string
	style lipgloss.Style
}

// row renders table cells on one line of width columns. The selected row gets
// the selection bar and a subtle background.
func row(segs []seg, width int, selected bool) string {
	if width < 2 {
		return ""
	}
	var b strings.Builder
	used := 0
	prefix := " "
	if selected {
		prefix = styleAccent.Background(colSelected).Render(glyphSelection)
	}
	b.WriteString(prefix)
	used++
	for _, s := range segs {
		if used >= width {
			break
		}
		text := s.text
		if room := width - used; textWidth(text) > room {
			text = ellipsis(text, room)
		}
		style := s.style
		if selected {
			style = style.Background(colSelected)
		}
		b.WriteString(style.Render(text))
		used += textWidth(text)
	}
	if used < width {
		pad := strings.Repeat(" ", width-used)
		if selected {
			pad = styleSelected.Render(pad)
		}
		b.WriteString(pad)
	}
	return b.String()
}

// col pads or cuts a cell to width columns.
func col(text string, width int) string {
	return fit(text, width)
}

// tableHeader renders the muted header line of a table and its rule.
func tableHeader(cells string, width int) []string {
	return []string{styleTableHead.Render(" " + cells), styleBorder.Render(strings.Repeat("─", max(0, width)))}
}

// field renders a labelled text field of a modal. The focused field is
// highlighted and shows a cursor.
func field(label string, value string, focused bool, width int) string {
	text := value
	if focused {
		text += styleKey.Render(glyphCursor)
	}
	box := fit(text, width)
	if focused {
		box = styleSelected.Render(" ") + box
		return styleMuted.Render(fmt.Sprintf("%-9s", label)) + box
	}
	return styleMuted.Render(fmt.Sprintf("%-9s", label)) + " " + box
}

// overlay draws modal centered over base. The background is kept, dimmed, around
// the modal, and every line keeps exactly width columns.
func overlay(base string, modal string, width int, height int) string {
	lines := strings.Split(base, "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	if modal == "" {
		return strings.Join(lines, "\n")
	}

	modalLines := strings.Split(modal, "\n")
	modalWidth := 0
	for _, line := range modalLines {
		modalWidth = max(modalWidth, textWidth(line))
	}
	if modalWidth > width {
		modalWidth = width
	}
	startRow := max(0, (height-len(modalLines))/2)
	startCol := max(0, (width-modalWidth)/2)

	out := make([]string, len(lines))
	for i, line := range lines {
		plain := fit(stripANSI(line), width)
		j := i - startRow
		if j < 0 || j >= len(modalLines) {
			out[i] = styleDim.Render(plain)
			continue
		}
		ml := fit(modalLines[j], modalWidth)
		left := styleDim.Render(cutColumns(plain, 0, startCol))
		right := styleDim.Render(cutColumns(plain, startCol+modalWidth, width))
		out[i] = left + ml + right
	}
	return strings.Join(out, "\n")
}

// wrapText breaks text into lines of at most width columns, at spaces when it
// can. Leading spaces are kept (they carry the indentation of log lines) and a
// word longer than a line is cut.
func wrapText(text string, width int) []string {
	if width < 1 {
		return nil
	}
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		indent := paragraph[:len(paragraph)-len(strings.TrimLeft(paragraph, " "))]
		if textWidth(indent) >= width {
			indent = ""
		}
		current := indent
		flush := func() {
			if strings.TrimSpace(current) != "" {
				lines = append(lines, strings.TrimRight(current, " "))
			}
			current = indent
		}
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		for _, word := range words {
			for textWidth(word) > width-textWidth(indent) {
				room := width - textWidth(indent)
				if strings.TrimSpace(current) != "" {
					flush()
				}
				lines = append(lines, indent+cutColumns(word, 0, room))
				word = cutColumns(word, room, textWidth(word))
				if word == "" {
					break
				}
			}
			if word == "" {
				continue
			}
			switch {
			case strings.TrimSpace(current) == "":
				current = indent + word
			case textWidth(current)+1+textWidth(word) <= width:
				current += " " + word
			default:
				flush()
				current = indent + word
			}
		}
		flush()
	}
	return lines
}
