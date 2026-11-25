package ui

// Capture of the interface as an animated SVG, for the README. This is test
// code on purpose: nothing of it is part of the binary. TestGenerateScreens
// (screens_test.go) uses it; run it with `make screens`.

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// cellStyle is what a terminal remembers between two characters.
type cellStyle struct {
	fg, bg                         string // "#rrggbb", "" for the default color
	bold, faint, italic, underline bool
	reverse                        bool
}

// cell is one character position. r == 0 is an empty cell, r == -1 the right half
// of a wide character.
type cell struct {
	r  rune
	st cellStyle
}

type grid [][]cell

// Theme of the capture: a dark terminal, the colors the interface is tuned for.
const (
	svgBackground = "#1a1b26"
	svgForeground = "#c0caf5"
	svgChrome     = "#16161e"
	svgFontStack  = `ui-monospace,SFMono-Regular,"SF Mono",Menlo,Consolas,"DejaVu Sans Mono","Liberation Mono",monospace`

	svgCellW    = 7.8
	svgCellH    = 17.0
	svgPadX     = 18.0
	svgPadTop   = 12.0
	svgPadBot   = 16.0
	svgBarH     = 34.0
	svgFontPx   = 13.0
	svgBaseline = 0.76
)

// ansiPalette are the 16 colors of the basic SGR codes.
var ansiPalette = [16]string{
	"#15161e", "#f7768e", "#9ece6a", "#e0af68", "#7aa2f7", "#bb9af7", "#7dcfff", "#a9b1d6",
	"#414868", "#f7768e", "#9ece6a", "#e0af68", "#7aa2f7", "#bb9af7", "#7dcfff", "#c0caf5",
}

// color256 returns the color of an index of the 256 colors palette.
func color256(n int) string {
	switch {
	case n < 0 || n > 255:
		return ""
	case n < 16:
		return ansiPalette[n]
	case n < 232:
		n -= 16
		level := func(v int) int {
			if v == 0 {
				return 0
			}
			return 55 + 40*v
		}
		return fmt.Sprintf("#%02x%02x%02x", level(n/36), level(n/6%6), level(n%6))
	}
	v := 8 + 10*(n-232)
	return fmt.Sprintf("#%02x%02x%02x", v, v, v)
}

// applySGR applies the parameters of an SGR sequence ("1;38;2;255;0;0").
func applySGR(st cellStyle, params string) cellStyle {
	if params == "" {
		return cellStyle{}
	}
	parts := strings.Split(params, ";")
	num := func(i int) int {
		if i >= len(parts) {
			return -1
		}
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return -1
		}
		return n
	}
	for i := 0; i < len(parts); i++ {
		n := num(i)
		switch {
		case n == 0:
			st = cellStyle{}
		case n == 1:
			st.bold = true
		case n == 2:
			st.faint = true
		case n == 3:
			st.italic = true
		case n == 4:
			st.underline = true
		case n == 7:
			st.reverse = true
		case n == 22:
			st.bold, st.faint = false, false
		case n == 23:
			st.italic = false
		case n == 24:
			st.underline = false
		case n == 27:
			st.reverse = false
		case n >= 30 && n <= 37:
			st.fg = ansiPalette[n-30]
		case n >= 90 && n <= 97:
			st.fg = ansiPalette[n-90+8]
		case n >= 40 && n <= 47:
			st.bg = ansiPalette[n-40]
		case n >= 100 && n <= 107:
			st.bg = ansiPalette[n-100+8]
		case n == 39:
			st.fg = ""
		case n == 49:
			st.bg = ""
		case n == 38 || n == 48:
			color := ""
			switch num(i + 1) {
			case 2:
				if num(i+2) >= 0 && num(i+3) >= 0 && num(i+4) >= 0 {
					color = fmt.Sprintf("#%02x%02x%02x", num(i+2)&255, num(i+3)&255, num(i+4)&255)
				}
				i += 4
			case 5:
				color = color256(num(i + 2))
				i += 2
			}
			if n == 38 {
				st.fg = color
			} else {
				st.bg = color
			}
		}
	}
	return st
}

// parseANSI turns styled text (what View returns) into a grid of cols columns.
// Only colors and text attributes are understood; any other escape sequence is
// skipped.
func parseANSI(text string, cols int) grid {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	out := make(grid, len(lines))
	for y, line := range lines {
		row := make([]cell, cols)
		var st cellStyle
		col := 0
		runes := []rune(line)
		for i := 0; i < len(runes); i++ {
			r := runes[i]
			if r == 0x1b && i+1 < len(runes) {
				switch runes[i+1] {
				case '[':
					j := i + 2
					for j < len(runes) && !(runes[j] >= 0x40 && runes[j] <= 0x7e) {
						j++
					}
					if j < len(runes) && runes[j] == 'm' {
						st = applySGR(st, string(runes[i+2:j]))
					}
					i = j
				case ']':
					j := i + 2
					for j < len(runes) && runes[j] != 0x07 && !(runes[j] == 0x1b && j+1 < len(runes) && runes[j+1] == '\\') {
						j++
					}
					if j < len(runes) && runes[j] == 0x1b {
						j++
					}
					i = j
				default:
					i++
				}
				continue
			}
			width := lipgloss.Width(string(r))
			if width == 0 || col >= cols {
				continue
			}
			row[col] = cell{r: r, st: st}
			if width == 2 && col+1 < cols {
				row[col+1] = cell{r: -1, st: st}
			}
			col += width
		}
		out[y] = row
	}
	return out
}

// effective returns the colors a cell is drawn with (reverse video applied).
func (c cell) effective() (fg string, bg string) {
	fg, bg = c.st.fg, c.st.bg
	if c.st.reverse {
		if fg == "" {
			fg = svgBackground
		}
		if bg == "" {
			bg = svgForeground
		}
		fg, bg = bg, fg
	}
	return fg, bg
}

// textStyle is what makes two cells part of the same text segment.
type textStyle struct {
	fg                             string
	bold, faint, italic, underline bool
}

func (c cell) textStyle() textStyle {
	fg, _ := c.effective()
	if fg == "" {
		fg = svgForeground
	}
	return textStyle{fg: fg, bold: c.st.bold, faint: c.st.faint, italic: c.st.italic, underline: c.st.underline}
}

// drawn reports whether a character is drawn as a vector shape: the frames of
// the panels must join without gaps, whatever font the viewer falls back to.
func drawn(r rune) bool {
	switch r {
	case '─', '━', '│', '╭', '╮', '╰', '╯', '▌':
		return true
	}
	return false
}

// svgEscape escapes text for an XML text node or attribute.
var svgEscape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

func f1(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }

// writeFrame draws one grid: backgrounds, text, then the vector shapes.
func writeFrame(b *strings.Builder, g grid, originX float64, originY float64) {
	for y, row := range g {
		top := originY + float64(y)*svgCellH

		// Backgrounds: runs of the same color.
		for x := 0; x < len(row); {
			_, bg := row[x].effective()
			if bg == "" {
				x++
				continue
			}
			end := x
			for end < len(row) {
				if _, next := row[end].effective(); next != bg {
					break
				}
				end++
			}
			fmt.Fprintf(b, `<rect x="%s" y="%s" width="%s" height="%s" fill="%s"/>`,
				f1(originX+float64(x)*svgCellW), f1(top), f1(float64(end-x)*svgCellW+0.4), f1(svgCellH+0.4), bg)
			x = end
		}

		// Text: runs of the same style, without the characters that are drawn.
		for x := 0; x < len(row); {
			c := row[x]
			if c.r == -1 || drawn(c.r) {
				x++
				continue
			}
			style := c.textStyle()
			end := x
			var text strings.Builder
			for end < len(row) && row[end].r != -1 && !drawn(row[end].r) && row[end].textStyle() == style {
				if row[end].r == 0 {
					text.WriteByte(' ')
				} else {
					text.WriteRune(row[end].r)
				}
				end++
				if end < len(row) && row[end].r == -1 { // a wide character takes two cells
					end++
				}
			}
			s := text.String()
			// Spaces at the edges say nothing: only underlined text shows them.
			start, cells := x, end-x
			if !style.underline {
				trimmed := strings.TrimLeft(s, " ")
				start += len(s) - len(trimmed)
				cells -= len(s) - len(trimmed)
				s = strings.TrimRight(trimmed, " ")
				cells -= len(trimmed) - len(s)
			}
			if s != "" && (strings.TrimSpace(s) != "" || style.underline) {
				attrs := ""
				if style.bold {
					attrs += ` font-weight="700"`
				}
				if style.italic {
					attrs += ` font-style="italic"`
				}
				if style.underline {
					attrs += ` text-decoration="underline"`
				}
				if style.faint {
					attrs += ` fill-opacity=".6"`
				}
				fmt.Fprintf(b, `<text class="t" x="%s" y="%s" fill="%s" textLength="%s" lengthAdjust="spacing"%s>%s</text>`,
					f1(originX+float64(start)*svgCellW), f1(top+svgCellH*svgBaseline), style.fg,
					f1(float64(cells)*svgCellW), attrs, svgEscape.Replace(s))
			}
			x = end
		}

		// Shapes: the frame characters.
		for x := 0; x < len(row); x++ {
			c := row[x]
			if !drawn(c.r) {
				continue
			}
			fg, _ := c.effective()
			if fg == "" {
				fg = svgForeground
			}
			left := originX + float64(x)*svgCellW
			right := left + svgCellW
			mid := left + svgCellW/2
			bottom := top + svgCellH
			centre := top + svgCellH/2
			radius := svgCellW / 2
			stroke := func(width float64) string {
				return fmt.Sprintf(`fill="none" stroke="%s" stroke-width="%s" stroke-linecap="butt"`, fg, f1(width))
			}
			switch c.r {
			case '─', '━':
				end := x
				for end+1 < len(row) && row[end+1].r == c.r && row[end+1].st == c.st {
					end++
				}
				width := 1.2
				if c.r == '━' {
					width = 2.4
				}
				fmt.Fprintf(b, `<line x1="%s" y1="%s" x2="%s" y2="%s" %s/>`, f1(left), f1(centre), f1(originX+float64(end+1)*svgCellW), f1(centre), stroke(width))
				x = end
			case '│':
				fmt.Fprintf(b, `<line x1="%s" y1="%s" x2="%s" y2="%s" %s/>`, f1(mid), f1(top-0.2), f1(mid), f1(bottom+0.2), stroke(1.2))
			case '╭':
				fmt.Fprintf(b, `<path d="M%s %s L%s %s Q%s %s %s %s L%s %s" %s/>`, f1(mid), f1(bottom), f1(mid), f1(centre+radius), f1(mid), f1(centre), f1(mid+radius), f1(centre), f1(right), f1(centre), stroke(1.2))
			case '╮':
				fmt.Fprintf(b, `<path d="M%s %s L%s %s Q%s %s %s %s L%s %s" %s/>`, f1(left), f1(centre), f1(mid-radius), f1(centre), f1(mid), f1(centre), f1(mid), f1(centre+radius), f1(mid), f1(bottom), stroke(1.2))
			case '╰':
				fmt.Fprintf(b, `<path d="M%s %s L%s %s Q%s %s %s %s L%s %s" %s/>`, f1(mid), f1(top), f1(mid), f1(centre-radius), f1(mid), f1(centre), f1(mid+radius), f1(centre), f1(right), f1(centre), stroke(1.2))
			case '╯':
				fmt.Fprintf(b, `<path d="M%s %s L%s %s Q%s %s %s %s L%s %s" %s/>`, f1(left), f1(centre), f1(mid-radius), f1(centre), f1(mid), f1(centre), f1(mid), f1(centre-radius), f1(mid), f1(top), stroke(1.2))
			case '▌':
				fmt.Fprintf(b, `<rect x="%s" y="%s" width="%s" height="%s" fill="%s"/>`, f1(left), f1(top), f1(svgCellW/2), f1(svgCellH+0.2), fg)
			}
		}
	}
}

// svgFrame is one image of an animation and how long it stays.
type svgFrame struct {
	grid grid
	hold time.Duration
}

// writeSVG writes an animated SVG: a terminal window, and one group per frame
// shown in turn, in a loop. The first frame is visible without animation, so a
// viewer that does not animate still shows something useful.
func writeSVG(w io.Writer, title string, cols int, rows int, frames []svgFrame) error {
	width := float64(cols)*svgCellW + 2*svgPadX
	height := svgBarH + svgPadTop + float64(rows)*svgCellH + svgPadBot

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" xml:space="preserve" width="%s" height="%s" viewBox="0 0 %s %s" role="img" aria-label="%s">`,
		f1(width), f1(height), f1(width), f1(height), svgEscape.Replace(title))
	fmt.Fprintf(&b, `<title>%s</title>`, svgEscape.Replace(title))
	fmt.Fprintf(&b, `<style>.t{font-family:%s;font-size:%spx;white-space:pre}.c{font-family:%s;font-size:12px}</style>`, svgFontStack, f1(svgFontPx), svgFontStack)
	fmt.Fprintf(&b, `<rect width="%s" height="%s" rx="10" fill="%s"/>`, f1(width), f1(height), svgBackground)
	fmt.Fprintf(&b, `<path d="M0 %s V10 a10 10 0 0 1 10 -10 H%s a10 10 0 0 1 10 10 V%s Z" fill="%s"/>`, f1(svgBarH), f1(width-10), f1(svgBarH), svgChrome)
	for i, color := range []string{"#f7768e", "#e0af68", "#9ece6a"} {
		fmt.Fprintf(&b, `<circle cx="%s" cy="%s" r="5.5" fill="%s"/>`, f1(20+float64(i)*18), f1(svgBarH/2), color)
	}
	fmt.Fprintf(&b, `<text class="c" x="%s" y="%s" fill="#787c99" text-anchor="middle">%s</text>`, f1(width/2), f1(svgBarH/2+4), svgEscape.Replace(title))

	var total time.Duration
	for _, frame := range frames {
		total += frame.hold
	}
	var start time.Duration
	for i, frame := range frames {
		visibility := "hidden"
		if i == 0 {
			visibility = "visible"
		}
		fmt.Fprintf(&b, `<g visibility="%s">`, visibility)
		writeFrame(&b, frame.grid, svgPadX, svgBarH+svgPadTop)
		if len(frames) > 1 {
			// Shown from its start to the start of the next frame, hidden otherwise.
			at := func(d time.Duration) string { return strconv.FormatFloat(float64(d)/float64(total), 'f', 4, 64) }
			end := start + frame.hold
			var values, keys string
			switch {
			case i == 0:
				values, keys = "visible;hidden", "0;"+at(end)
			case i == len(frames)-1:
				values, keys = "hidden;visible", "0;"+at(start)
			default:
				values, keys = "hidden;visible;hidden", "0;"+at(start)+";"+at(end)
			}
			fmt.Fprintf(&b, `<animate attributeName="visibility" calcMode="discrete" dur="%ss" repeatCount="indefinite" values="%s" keyTimes="%s"/>`,
				strconv.FormatFloat(total.Seconds(), 'f', 2, 64), values, keys)
		}
		b.WriteString(`</g>`)
		start += frame.hold
	}
	b.WriteString(`</svg>`)
	b.WriteByte('\n')

	_, err := io.WriteString(w, b.String())
	return err
}
