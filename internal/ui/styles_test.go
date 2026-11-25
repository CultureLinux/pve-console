package ui

import (
	"math"
	"strconv"
	"testing"
)

// luminance is the WCAG relative luminance of a "#RRGGBB" color.
func luminance(t *testing.T, hex string) float64 {
	t.Helper()
	if len(hex) != 7 || hex[0] != '#' {
		t.Fatalf("bad color %q", hex)
	}
	channel := func(s string) float64 {
		v, err := strconv.ParseUint(s, 16, 8)
		if err != nil {
			t.Fatalf("bad color %q: %v", hex, err)
		}
		c := float64(v) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(hex[1:3]) + 0.7152*channel(hex[3:5]) + 0.0722*channel(hex[5:7])
}

func contrast(t *testing.T, a string, b string) float64 {
	la, lb := luminance(t, a), luminance(t, b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// The brand color is only used as a badge background (see TestBadgesAreReadable).
// The terminal keeps its own background, so text colors are checked against the
// usual dark and light backgrounds and against the selected row.
func TestTextColorsAreReadable(t *testing.T) {
	darkBackgrounds := []string{"#000000", "#1A1B26", "#1E1E1E", "#282A36"}
	lightBackgrounds := []string{"#FFFFFF", "#F5F5F5", "#FDF6E3"}
	darkSelected, lightSelected := colSelected.Dark, colSelected.Light

	text := map[string]struct{ light, dark string }{
		"accent": {colAccent.Light, colAccent.Dark},
		"ok":     {colOK.Light, colOK.Dark},
		"warn":   {colWarn.Light, colWarn.Dark},
		"danger": {colDanger.Light, colDanger.Dark},
		"muted":  {colMuted.Light, colMuted.Dark},
	}
	const minimum = 4.5
	for name, c := range text {
		for _, bg := range darkBackgrounds {
			if got := contrast(t, c.dark, bg); got < minimum {
				t.Errorf("%s (dark %s) on %s: contrast %.2f < %.1f", name, c.dark, bg, got, minimum)
			}
		}
		for _, bg := range lightBackgrounds {
			if got := contrast(t, c.light, bg); got < minimum {
				t.Errorf("%s (light %s) on %s: contrast %.2f < %.1f", name, c.light, bg, got, minimum)
			}
		}
		if got := contrast(t, c.dark, darkSelected); got < minimum {
			t.Errorf("%s (dark) on the selected row: contrast %.2f", name, got)
		}
		if got := contrast(t, c.light, lightSelected); got < minimum {
			t.Errorf("%s (light) on the selected row: contrast %.2f", name, got)
		}
	}
}

func TestBadgesAreReadable(t *testing.T) {
	pairs := map[string][4]string{
		"tab":     {colOnAccent.Light, colAccent.Light, colOnAccent.Dark, colAccent.Dark},
		"brand":   {colOnBrand.Light, colBrand.Light, colOnBrand.Dark, colBrand.Dark},
		"warn":    {onColor(colWarn).Light, colWarn.Light, onColor(colWarn).Dark, colWarn.Dark},
		"danger":  {onColor(colDanger).Light, colDanger.Light, onColor(colDanger).Dark, colDanger.Dark},
		"primary": {onColor(colAccent).Light, colAccent.Light, onColor(colAccent).Dark, colAccent.Dark},
	}
	for name, p := range pairs {
		if got := contrast(t, p[0], p[1]); got < 4.5 {
			t.Errorf("%s badge (light): text %s on %s has contrast %.2f", name, p[0], p[1], got)
		}
		if got := contrast(t, p[2], p[3]); got < 4.5 {
			t.Errorf("%s badge (dark): text %s on %s has contrast %.2f", name, p[2], p[3], got)
		}
	}
}

func TestEveryStatusHasItsOwnGlyph(t *testing.T) {
	seen := map[string]string{}
	for _, status := range []string{"running", "stopped", "paused"} {
		glyph, _ := statusDot(status)
		if other, dup := seen[glyph]; dup {
			t.Errorf("%q and %q share the glyph %q: color alone must not carry the meaning", status, other, glyph)
		}
		seen[glyph] = status
	}
}
