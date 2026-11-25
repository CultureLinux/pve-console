package ui

import (
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// screenCols is the width of the terminal the captures show; each scenario has
// its own height (the log needs room for the end of a task log).
const screenCols = 110

// recorder collects the images of a scenario.
type recorder struct {
	t      *testing.T
	rows   int
	frames []svgFrame
}

// snap adds the current screen of m, shown for hold.
func (r *recorder) snap(m Model, hold time.Duration) {
	r.t.Helper()
	g := parseANSI(m.View(), screenCols)
	if len(g) > r.rows {
		r.t.Fatalf("the screen has %d lines, the capture holds %d", len(g), r.rows)
	}
	for len(g) < r.rows {
		g = append(g, make([]cell, screenCols))
	}
	r.frames = append(r.frames, svgFrame{grid: g, hold: hold})
}

func demoScreenModel(rows int) Model {
	m := NewWithStartup(DemoClient(), false)
	m.demo = true
	m.width, m.height = screenCols, rows
	return m
}

// screenScenarios are the captures of the README: a title and a script that
// plays the demo and snaps the screens worth showing.
var screenScenarios = []struct {
	name  string
	title string
	rows  int
	play  func(t *testing.T, r *recorder)
}{
	{"guests", "pve-console · guests, snapshots, backups", 28, playGuests},
	{"upgrade", "pve-console · guided package upgrade", 30, playUpgrade},
	{"log", "pve-console · errors and results in one log", 38, playLog},
}

func playGuests(t *testing.T, r *recorder) {
	m := demoScreenModel(r.rows)
	m, _ = execute(t, m, m.load)
	r.snap(m, 2400*time.Millisecond)

	m, _ = press(t, m, "j")
	r.snap(m, 1500*time.Millisecond)

	m, cmd := press(t, m, "s")
	m, _ = execute(t, m, cmd)
	r.snap(m, 2400*time.Millisecond)

	m, _ = press(t, m, "n")
	r.snap(m, 2400*time.Millisecond)
	m, _ = press(t, m, "esc", "esc")

	m, _ = press(t, m, "f")
	r.snap(m, 1700*time.Millisecond)
	m, _ = press(t, m, "f", "f")

	m, cmd = press(t, m, "b")
	m, _ = execute(t, m, cmd)
	r.snap(m, 2600*time.Millisecond)
}

// playUpgrade plays the upgrade message by message, so the screens in the
// middle of a step are the real ones.
func playUpgrade(t *testing.T, r *recorder) {
	previous := demoLineDelay
	demoLineDelay = 0
	t.Cleanup(func() { demoLineDelay = previous })

	m := demoScreenModel(r.rows)
	m, _ = execute(t, m, m.load)
	m, cmd := press(t, m, "p")
	m, _ = execute(t, m, cmd)
	r.snap(m, 2200*time.Millisecond)

	// pump delivers what a step sends, and snaps after the given number of lines.
	pump := func(m Model, cmd tea.Cmd, snapAt map[int]time.Duration) Model {
		lines := 0
		for cmd != nil {
			msg := cmd()
			m, cmd = deliver(t, m, msg)
			if _, ok := msg.(upgradeLineMsg); ok {
				lines++
				if hold, ok := snapAt[lines]; ok {
					r.snap(m, hold)
				}
			}
		}
		return m
	}

	m, cmd = press(t, m, "a")
	m = pump(m, cmd, map[int]time.Duration{2: 1000 * time.Millisecond, 4: 1000 * time.Millisecond})
	if m.modalMode != modalConfirm {
		t.Fatalf("the upgrade must ask first, mode=%q", m.modalMode)
	}
	r.snap(m, 3200*time.Millisecond)

	m, cmd = press(t, m, "enter")
	m = pump(m, cmd, map[int]time.Duration{3: 900 * time.Millisecond, 6: 1000 * time.Millisecond, 9: 900 * time.Millisecond})
	if m.upgradeReboot == "" {
		t.Fatal("the demo upgrade installs a new kernel: a reboot warning is expected")
	}
	r.snap(m, 4200*time.Millisecond)

	m, _ = press(t, m, "enter")
	r.snap(m, 2000*time.Millisecond)

	m, _ = press(t, m, "L")
	r.snap(m, 3000*time.Millisecond)
}

func playLog(t *testing.T, r *recorder) {
	m := demoScreenModel(r.rows)
	m, _ = execute(t, m, m.load)
	// As at start: the tasks of the cluster, as the web interface lists them.
	m, _ = execute(t, m, m.loadClusterTasks)

	m, _ = press(t, m, "j", "j", "j")
	r.snap(m, 1500*time.Millisecond)

	m, _ = press(t, m, "+")
	r.snap(m, 1700*time.Millisecond)
	m, cmd := press(t, m, "enter")
	m, _ = execute(t, m, cmd)
	r.snap(m, 2600*time.Millisecond)

	m, _ = press(t, m, "L")
	r.snap(m, 2800*time.Millisecond)

	m, _ = press(t, m, "j", "j")
	r.snap(m, 3600*time.Millisecond)

	m, _ = press(t, m, "f")
	r.snap(m, 2000*time.Millisecond)
	m, _ = press(t, m, "f")
	r.snap(m, 2000*time.Millisecond)
}

// TestGenerateScreens writes the README captures in $PVC_SCREENS_DIR (make
// screens). Without that variable it does nothing.
func TestGenerateScreens(t *testing.T) {
	dir := os.Getenv("PVC_SCREENS_DIR")
	if dir == "" {
		t.Skip("set PVC_SCREENS_DIR to write the README captures (make screens)")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("unable to create %s: %v", dir, err)
	}

	// Colors come out whatever the output is: the captures are always in 24 bits, dark.
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	for _, scenario := range screenScenarios {
		t.Run(scenario.name, func(t *testing.T) {
			r := &recorder{t: t, rows: scenario.rows}
			scenario.play(t, r)
			if len(r.frames) < 3 {
				t.Fatalf("a capture needs several images, got %d", len(r.frames))
			}

			path := filepath.Join(dir, scenario.name+".svg")
			file, err := os.Create(path)
			if err != nil {
				t.Fatalf("unable to create %s: %v", path, err)
			}
			defer file.Close()
			if err := writeSVG(file, scenario.title, screenCols, scenario.rows, r.frames); err != nil {
				t.Fatalf("unable to write %s: %v", path, err)
			}
			info, _ := file.Stat()
			t.Logf("%s: %d images, %d KiB", path, len(r.frames), info.Size()/1024)
		})
	}
}

func TestParseANSI(t *testing.T) {
	g := parseANSI("\x1b[1;38;2;255;0;0mAB\x1b[0m C\x1b[48;5;196mD\x1b[0m\x1b]11;?\x07\x1b[2Kz\nsecond", 12)
	if len(g) != 2 || len(g[0]) != 12 {
		t.Fatalf("two padded lines expected, got %d lines of %d", len(g), len(g[0]))
	}
	a, b := g[0][0], g[0][1]
	if a.r != 'A' || b.r != 'B' || a.st.fg != "#ff0000" || !a.st.bold || a != (cell{r: 'A', st: a.st}) {
		t.Fatalf("styled cells: %+v %+v", a, b)
	}
	if g[0][2].st != (cellStyle{}) || g[0][3].r != 'C' || g[0][3].st.bold {
		t.Fatalf("reset ends the style: %+v %+v", g[0][2], g[0][3])
	}
	if d := g[0][4]; d.r != 'D' || d.st.bg != color256(196) || d.st.bg != "#ff0000" {
		t.Fatalf("256 colors: %+v", d)
	}
	if z := g[0][5]; z.r != 'z' {
		t.Fatalf("other escape sequences are skipped: %+v", z)
	}
	if g[1][0].r != 's' || g[1][0].st != (cellStyle{}) {
		t.Fatalf("the style does not leak to the next line: %+v", g[1][0])
	}
}

func TestParseANSIWideAndOverlong(t *testing.T) {
	g := parseANSI("日x", 3)
	if g[0][0].r != '日' || g[0][1].r != -1 || g[0][2].r != 'x' {
		t.Fatalf("a wide character takes two cells: %+v", g[0])
	}
	g = parseANSI("abcdef", 4)
	if len(g[0]) != 4 || g[0][3].r != 'd' {
		t.Fatalf("a line longer than the grid is cut: %+v", g[0])
	}
}

func TestColor256(t *testing.T) {
	for n, want := range map[int]string{1: ansiPalette[1], 16: "#000000", 231: "#ffffff", 232: "#080808", 255: "#eeeeee", 196: "#ff0000"} {
		if got := color256(n); got != want {
			t.Errorf("color256(%d) = %q, want %q", n, got, want)
		}
	}
	if color256(300) != "" || color256(-1) != "" {
		t.Fatal("an index out of range has no color")
	}
}

// svgOf renders frames to a string.
func svgOf(t *testing.T, frames []svgFrame, cols int, rows int) string {
	t.Helper()
	var b strings.Builder
	if err := writeSVG(&b, `a <b> & "c"`, cols, rows, frames); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return b.String()
}

func TestWriteSVGIsWellFormedAndEscaped(t *testing.T) {
	g := parseANSI("\x1b[38;2;10;20;30m<&>\x1b[0m \x1b[7mrev\x1b[0m\n╭──╮\n│ab│\n╰──╯ ▌", 8)
	out := svgOf(t, []svgFrame{{grid: g, hold: time.Second}}, 8, 4)

	decoder := xml.NewDecoder(strings.NewReader(out))
	for {
		if _, err := decoder.Token(); err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatalf("the SVG is not well-formed: %v\n%s", err, out)
			}
			break
		}
	}
	if !strings.Contains(out, "&lt;&amp;&gt;") || strings.Contains(out, ">&<") || !strings.Contains(out, "a &lt;b&gt; &amp; &quot;c&quot;") {
		t.Fatalf("text and title are escaped:\n%s", out)
	}
	if strings.Contains(out, "<animate") {
		t.Fatal("a single image has no animation")
	}
	if !strings.Contains(out, `visibility="visible"`) {
		t.Fatal("the first image is visible without animation")
	}
}

func TestWriteSVGDrawsFramesAsShapes(t *testing.T) {
	g := parseANSI("╭──╮\n│ab│\n╰──╯", 5)
	out := svgOf(t, []svgFrame{{grid: g, hold: time.Second}}, 5, 3)

	decoder := xml.NewDecoder(strings.NewReader(out))
	inText := false
	var content strings.Builder
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch tok := token.(type) {
		case xml.StartElement:
			inText = tok.Name.Local == "text"
		case xml.EndElement:
			inText = false
		case xml.CharData:
			if inText {
				content.Write(tok)
			}
		}
	}
	if !strings.Contains(content.String(), "ab") {
		t.Fatalf("the letters are text: %q", content.String())
	}
	for _, r := range content.String() {
		if drawn(r) {
			t.Fatalf("a frame character is text: %q", content.String())
		}
	}
	// 4 corners, the top and bottom runs (one line each), the 2 sides.
	if strings.Count(out, "<line") != 4 || strings.Count(out, "stroke-width") != 8 {
		t.Fatalf("frame characters are drawn as shapes, runs of ─ joined:\n%s", out)
	}
}

func TestWriteSVGAnimationTiming(t *testing.T) {
	one := parseANSI("a", 2)
	out := svgOf(t, []svgFrame{
		{grid: one, hold: 1 * time.Second},
		{grid: one, hold: 2 * time.Second},
		{grid: one, hold: 1 * time.Second},
	}, 2, 1)

	if strings.Count(out, "<animate") != 3 || !strings.Contains(out, `dur="4.00s"`) || !strings.Contains(out, `repeatCount="indefinite"`) {
		t.Fatalf("one animation per image over the whole loop:\n%s", out)
	}
	for _, want := range []string{
		`values="visible;hidden" keyTimes="0;0.2500"`,
		`values="hidden;visible;hidden" keyTimes="0;0.2500;0.7500"`,
		`values="hidden;visible" keyTimes="0;0.7500"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Count(out, `visibility="visible"`) < 1 || strings.Count(out, `<g visibility="hidden">`) != 2 {
		t.Fatalf("only the first image starts visible:\n%s", out)
	}
}

func TestWriteSVGTextSitsOnTheGrid(t *testing.T) {
	g := parseANSI("  ab  \x1b[1mcd\x1b[0m", 10)
	out := svgOf(t, []svgFrame{{grid: g, hold: time.Second}}, 10, 1)

	// "ab" starts at the third cell, without its edge spaces; its length is 2 cells.
	if !strings.Contains(out, `x="`+f1(svgPadX+2*svgCellW)+`"`) || !strings.Contains(out, `textLength="`+f1(2*svgCellW)+`"`) {
		t.Fatalf("segments are placed on the grid:\n%s", out)
	}
	if !strings.Contains(out, `font-weight="700"`) {
		t.Fatalf("bold is kept:\n%s", out)
	}
}
