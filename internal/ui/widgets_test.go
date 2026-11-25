package ui

import (
	"strings"
	"testing"
)

const (
	red   = "\x1b[31m"
	bold  = "\x1b[1m"
	reset = "\x1b[0m"
)

func TestStripANSI(t *testing.T) {
	if got := stripANSI(red + "abc" + reset + bold + "é" + reset); got != "abcé" {
		t.Fatalf("stripANSI = %q", got)
	}
	if got := stripANSI("plain ✓"); got != "plain ✓" {
		t.Fatalf("plain text must be untouched, got %q", got)
	}
}

func TestCutColumns(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		from, to int
		want     string
	}{
		{"plain", "abcdef", 1, 4, "bcd"},
		{"whole", "abc", 0, 10, "abc"},
		{"empty range", "abc", 2, 2, ""},
		{"reversed range", "abc", 3, 1, ""},
		{"past the end", "abc", 5, 9, ""},
		{"wide runes", "日本語", 0, 4, "日本"},
		{"wide rune cut at the end", "日本語", 0, 3, "日"},
		{"wide rune straddling the start", "日本語", 1, 4, " 本"},
		{"unicode glyphs", "●○✓", 1, 3, "○✓"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cutColumns(tt.in, tt.from, tt.to); got != tt.want {
				t.Fatalf("cutColumns(%q, %d, %d) = %q, want %q", tt.in, tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestCutColumnsKeepsStyles(t *testing.T) {
	in := red + "abc" + reset + "def"

	got := cutColumns(in, 1, 5)
	if stripANSI(got) != "bcde" {
		t.Fatalf("visible text = %q", stripANSI(got))
	}
	if !strings.HasPrefix(got, red) {
		t.Fatalf("the color active at the cut must be restored, got %q", got)
	}
	if !strings.Contains(got, reset) {
		t.Fatalf("the color must be closed, got %q", got)
	}

	// A cut inside a styled run is closed so the style cannot leak.
	got = cutColumns(red+"abcdef", 0, 3)
	if stripANSI(got) != "abc" || !strings.HasSuffix(got, reset) {
		t.Fatalf("got %q", got)
	}
	if width := textWidth(got); width != 3 {
		t.Fatalf("escape sequences must not count, width = %d", width)
	}
}

func TestFitAndEllipsis(t *testing.T) {
	tests := []struct {
		name string
		in   string
		w    int
		want string
	}{
		{"pad", "ab", 5, "ab   "},
		{"exact", "abcde", 5, "abcde"},
		{"truncate", "abcdefgh", 5, "abcd…"},
		{"one column", "abc", 1, "…"},
		{"zero", "abc", 0, ""},
		{"wide", "日本語日本語", 7, "日本語…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fit(tt.in, tt.w)
			if got != tt.want {
				t.Fatalf("fit(%q, %d) = %q, want %q", tt.in, tt.w, got, tt.want)
			}
			if textWidth(got) > tt.w {
				t.Fatalf("width %d exceeds %d", textWidth(got), tt.w)
			}
		})
	}
	if got := ellipsis("short", 10); got != "short" {
		t.Fatalf("ellipsis must not pad, got %q", got)
	}
	if got := truncate("proxmox-backup", 8); textWidth(got) != 8 || !strings.HasSuffix(got, "…") {
		t.Fatalf("truncate = %q", got)
	}
}

func TestRightAlign(t *testing.T) {
	got := rightAlign("left", "right", 20)
	if textWidth(got) != 20 || !strings.HasPrefix(got, "left") || !strings.HasSuffix(got, "right") {
		t.Fatalf("rightAlign = %q", got)
	}
	// When both do not fit, the right part is dropped rather than overflowing.
	got = rightAlign("left side", "right side", 12)
	if textWidth(got) != 12 || strings.Contains(got, "right") {
		t.Fatalf("rightAlign must drop the right part, got %q", got)
	}
}

func TestPanelHasExactDimensions(t *testing.T) {
	for _, size := range [][2]int{{20, 5}, {40, 10}, {80, 24}} {
		out := panel("Guests", "4", []string{"one", "two"}, size[0], size[1], true)
		lines := strings.Split(out, "\n")
		if len(lines) != size[1] {
			t.Fatalf("%v: %d lines", size, len(lines))
		}
		for i, line := range lines {
			if textWidth(line) != size[0] {
				t.Fatalf("%v: line %d has width %d: %q", size, i, textWidth(line), line)
			}
		}
	}
}

func TestPanelPutsTheTitleInTheBorder(t *testing.T) {
	out := stripANSI(panel("Guests", "4", nil, 40, 5, false))
	first := strings.Split(out, "\n")[0]
	if !strings.HasPrefix(first, "╭─ Guests 4") || !strings.HasSuffix(first, "╮") {
		t.Fatalf("top border = %q", first)
	}
	last := strings.Split(out, "\n")[4]
	if !strings.HasPrefix(last, "╰") || !strings.HasSuffix(last, "╯") {
		t.Fatalf("bottom border = %q", last)
	}
}

func TestPanelCutsLongContentAndTitles(t *testing.T) {
	long := strings.Repeat("x", 200)
	out := panel(strings.Repeat("T", 100), "", []string{long, long, long, long, long, long}, 30, 5, false)
	lines := strings.Split(out, "\n")
	if len(lines) != 5 {
		t.Fatalf("%d lines", len(lines))
	}
	for _, line := range lines {
		if textWidth(line) != 30 {
			t.Fatalf("width %d: %q", textWidth(line), line)
		}
	}
}

func TestPanelTooSmallIsEmpty(t *testing.T) {
	if panel("x", "", nil, 3, 2, false) != "" {
		t.Fatal("a panel that cannot hold a border must render nothing")
	}
}

func TestBar(t *testing.T) {
	tests := []struct {
		ratio  float64
		filled int
	}{
		{0, 0}, {0.5, 5}, {1, 10}, {-1, 0}, {2, 10}, {0.04, 0}, {0.96, 10},
	}
	for _, tt := range tests {
		got := stripANSI(bar(tt.ratio, 10))
		if textWidth(got) != 10 {
			t.Fatalf("bar(%v) width = %d", tt.ratio, textWidth(got))
		}
		if full := strings.Count(got, glyphBarFull); full != tt.filled {
			t.Fatalf("bar(%v) filled %d, want %d: %q", tt.ratio, full, tt.filled, got)
		}
	}
	if bar(0.5, 0) != "" {
		t.Fatal("an empty bar must render nothing")
	}
}

func TestWindowKeepsTheSelectionVisible(t *testing.T) {
	tests := []struct {
		selected, count, rows int
		start, end            int
	}{
		{0, 5, 10, 0, 5},
		{0, 50, 10, 0, 10},
		{9, 50, 10, 0, 10},
		{10, 50, 10, 1, 11},
		{49, 50, 10, 40, 50},
		{3, 0, 10, 0, 0},
		{3, 5, 0, 3, 4},
	}
	for _, tt := range tests {
		start, end := window(tt.selected, tt.count, tt.rows)
		if start != tt.start || end != tt.end {
			t.Errorf("window(%d, %d, %d) = %d,%d want %d,%d", tt.selected, tt.count, tt.rows, start, end, tt.start, tt.end)
		}
	}
}

func TestRowFillsTheWidthAndMarksTheSelection(t *testing.T) {
	segs := []seg{{"● ", styleOK}, {"name", plain}}
	plainRow := stripANSI(row(segs, 20, false))
	selected := stripANSI(row(segs, 20, true))
	if textWidth(plainRow) != 20 || textWidth(selected) != 20 {
		t.Fatalf("widths %d and %d", textWidth(plainRow), textWidth(selected))
	}
	if !strings.HasPrefix(selected, glyphSelection) || strings.HasPrefix(plainRow, glyphSelection) {
		t.Fatalf("only the selected row has the bar: %q / %q", plainRow, selected)
	}
	if got := stripANSI(row([]seg{{strings.Repeat("x", 50), plain}}, 10, false)); textWidth(got) != 10 {
		t.Fatalf("a long cell must be cut, width %d", textWidth(got))
	}
}

func TestOverlayKeepsTheBackgroundAroundTheModal(t *testing.T) {
	var base []string
	for i := 0; i < 10; i++ {
		base = append(base, strings.Repeat(string(rune('a'+i)), 40))
	}
	modal := "┌──────┐\n│ hi   │\n└──────┘"
	out := overlay(strings.Join(base, "\n"), modal, 40, 10)

	lines := strings.Split(stripANSI(out), "\n")
	if len(lines) != 10 {
		t.Fatalf("%d lines", len(lines))
	}
	for i, line := range lines {
		if textWidth(line) != 40 {
			t.Fatalf("line %d has width %d: %q", i, textWidth(line), line)
		}
	}
	// The modal is centered: 3 lines starting at row (10-3)/2 = 3, column (40-8)/2 = 16.
	if !strings.Contains(lines[4], "│ hi   │") || strings.Index(lines[4], "│") != 16 {
		t.Fatalf("modal not centered: %q", lines[4])
	}
	// On the modal's rows the background is still there on both sides.
	if !strings.HasPrefix(lines[4], strings.Repeat("e", 16)) || !strings.HasSuffix(lines[4], strings.Repeat("e", 16)) {
		t.Fatalf("the background must stay around the modal: %q", lines[4])
	}
	// The other rows are untouched.
	if lines[0] != base[0] || lines[9] != base[9] {
		t.Fatalf("background rows changed: %q %q", lines[0], lines[9])
	}
}

func TestOverlayCropsAModalTallerThanTheScreen(t *testing.T) {
	modal := strings.TrimRight(strings.Repeat("│ line │\n", 30), "\n")
	out := overlay(strings.Repeat(strings.Repeat(".", 30)+"\n", 9)+strings.Repeat(".", 30), modal, 30, 10)
	lines := strings.Split(out, "\n")
	if len(lines) != 10 {
		t.Fatalf("the screen must keep its height, got %d lines", len(lines))
	}
}

func TestOverlayWithoutModalPadsToTheScreenHeight(t *testing.T) {
	out := overlay("a\nb", "", 10, 5)
	if lines := strings.Split(out, "\n"); len(lines) != 5 {
		t.Fatalf("%d lines", len(lines))
	}
}

func TestWrapText(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		width int
		want  []string
	}{
		{"fits", "short text", 20, []string{"short text"}},
		{"breaks at spaces", "one two three four", 9, []string{"one two", "three", "four"}},
		{"exact width", "abcd efgh", 9, []string{"abcd efgh"}},
		{"keeps line breaks", "first\nsecond", 20, []string{"first", "second"}},
		{"keeps blank lines", "a\n\nb", 20, []string{"a", "", "b"}},
		{"cuts a word that cannot fit", "abcdefghij", 4, []string{"abcd", "efgh", "ij"}},
		{"keeps the indentation", "  indented words that wrap", 14, []string{"  indented", "  words that", "  wrap"}},
		{"wide runes", "日本語日本語", 6, []string{"日本語", "日本語"}},
		{"empty", "", 10, []string{""}},
		{"no room", "abc", 0, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapText(tt.in, tt.width)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") || len(got) != len(tt.want) {
				t.Fatalf("wrapText(%q, %d) = %q, want %q", tt.in, tt.width, got, tt.want)
			}
			for _, line := range got {
				if textWidth(line) > tt.width {
					t.Fatalf("line %q is wider than %d", line, tt.width)
				}
			}
		})
	}
}
