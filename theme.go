package main

import (
	"image/color"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// theme is a warm, amber "beer & foam" palette that adapts to light and dark
// terminals.
type theme struct {
	dark        bool
	accent      color.Color // amber
	onAccent    color.Color // text drawn on top of accent
	foam        color.Color
	fg          color.Color
	muted       color.Color
	faint       color.Color
	border      color.Color
	borderFocus color.Color
	selBg       color.Color
	green       color.Color
	red         color.Color
	yellow      color.Color
	teal        color.Color
	purple      color.Color
}

func newTheme(dark bool) theme {
	ld := lipgloss.LightDark(dark)
	c := lipgloss.Color
	return theme{
		dark:        dark,
		accent:      ld(c("#B45A00"), c("#FFB347")),
		onAccent:    ld(c("#FFFFFF"), c("#1F1408")),
		foam:        ld(c("#7A4A12"), c("#FFE3AE")),
		fg:          ld(c("#2A2118"), c("#ECE3D5")),
		muted:       ld(c("#776957"), c("#9E9182")),
		faint:       ld(c("#B5A893"), c("#5C534A")),
		border:      ld(c("#DCD0BE"), c("#3A332C")),
		borderFocus: ld(c("#D49248"), c("#8C6433")),
		selBg:       ld(c("#F9E8CC"), c("#33281C")),
		green:       ld(c("#2E7D32"), c("#8FD481")),
		red:         ld(c("#C62828"), c("#FF7B72")),
		yellow:      ld(c("#9A6400"), c("#F2CC60")),
		teal:        ld(c("#00796B"), c("#6ED3C5")),
		purple:      ld(c("#6B4FBB"), c("#BBA6FF")),
	}
}

func (t theme) s(fg color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(fg) }

func (t theme) pill(bg color.Color, text string) string {
	return lipgloss.NewStyle().
		Background(bg).
		Foreground(t.onAccent).
		Bold(true).
		Padding(0, 1).
		Render(text)
}

func (t theme) kindColor(k Kind) color.Color {
	if k == KindCask {
		return t.teal
	}
	return t.accent
}

func kindGlyph(k Kind) string {
	if k == KindCask {
		return "◆"
	}
	return "●"
}

// --- layout helpers ---------------------------------------------------------

// fit truncates or pads s (which may contain ANSI) to exactly w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	if pad := w - lipgloss.Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

func trunc(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) > w {
		return ansi.Truncate(s, w, "…")
	}
	return s
}

// wrap word-wraps plain text to width w.
func wrap(s string, w int) []string {
	if w <= 0 {
		return nil
	}
	return strings.Split(lipgloss.NewStyle().Width(w).Render(s), "\n")
}

// panel draws a rounded box with a title in the top border and an optional
// note in the bottom border.
func (t theme) panel(title, note string, lines []string, w, h int, focused bool) string {
	if w < 4 || h < 2 {
		return ""
	}
	bc := t.border
	if focused {
		bc = t.borderFocus
	}
	b := t.s(bc)
	inner := w - 4

	var top string
	if title != "" {
		title = " " + title + " "
		fill := w - 3 - lipgloss.Width(title)
		top = b.Render("╭─") + title + b.Render(strings.Repeat("─", max(0, fill))+"╮")
	} else {
		top = b.Render("╭" + strings.Repeat("─", w-2) + "╮")
	}

	var bottom string
	if note != "" {
		note = " " + note + " "
		fill := w - 3 - lipgloss.Width(note)
		bottom = b.Render("╰"+strings.Repeat("─", max(0, fill))) + note + b.Render("─╯")
	} else {
		bottom = b.Render("╰" + strings.Repeat("─", w-2) + "╯")
	}

	out := make([]string, 0, h)
	out = append(out, top)
	side := b.Render("│")
	for i := 0; i < h-2; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		out = append(out, side+" "+fit(line, inner)+" "+side)
	}
	out = append(out, bottom)
	return strings.Join(out, "\n")
}

func relTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	plural := func(n int, unit string) string {
		if n == 1 {
			return "1 " + unit + " ago"
		}
		return itoa(n) + " " + unit + "s ago"
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute")
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour")
	case d < 7*24*time.Hour:
		return plural(int(d.Hours()/24), "day")
	case d < 30*24*time.Hour:
		return plural(int(d.Hours()/24/7), "week")
	case d < 365*24*time.Hour:
		return plural(int(d.Hours()/24/30), "month")
	default:
		return plural(int(d.Hours()/24/365), "year")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func fmtDuration(d time.Duration) string {
	if d < time.Minute {
		return d.Round(100 * time.Millisecond).String()
	}
	return d.Round(time.Second).String()
}
