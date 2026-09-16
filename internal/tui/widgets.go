package tui

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
)

// box draws a rounded panel with its title sitting in the top border.
//
// The border is drawn by hand rather than with a lipgloss border, because the
// title has to interrupt the top rule and splicing a styled title into an
// already rendered border means counting escape sequences.
func box(name string, width int, content string) string {
	inner := width - 2 // the space between the two verticals
	if inner < 4 {
		inner = 4
	}

	var b strings.Builder

	// Top: rounded corner, a dash, the title, then the rule out to the corner.
	head := ""
	if name != "" {
		head = cardTitle.Render(" " + name + " ")
	}
	used := lipgloss.Width(head) + 1 // the leading dash
	fill := inner - used
	if fill < 0 {
		fill = 0
	}
	b.WriteString(edge.Render("╭─"))
	b.WriteString(head)
	b.WriteString(edge.Render(strings.Repeat("─", fill) + "╮"))
	b.WriteString("\n")

	// Body: every line padded to the inner width so the right edge lines up.
	for _, line := range strings.Split(content, "\n") {
		pad := inner - 2 - lipgloss.Width(line)
		if pad < 0 {
			pad = 0
			line = truncate(line, inner-2)
		}
		b.WriteString(edge.Render("│") + " " + line + strings.Repeat(" ", pad) + " " + edge.Render("│"))
		b.WriteString("\n")
	}

	b.WriteString(edge.Render("╰" + strings.Repeat("─", inner) + "╯"))
	return b.String()
}

// truncate cuts a styled string to a visible width, ignoring escape sequences.
func truncate(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	var out strings.Builder
	var visible int
	var inEscape bool
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEscape = true
		case inEscape:
			if r == 'm' {
				inEscape = false
			}
		default:
			if visible >= width {
				continue
			}
			visible++
		}
		out.WriteRune(r)
	}
	return out.String()
}

// bigDigits is a five row block font for the one number per panel that should
// be readable from across the room.
//
// Solid blocks rather than box drawing characters: a block still reads as a
// block when a renderer adjusts glyph spacing, whereas box drawing corners come
// apart as soon as they stop touching.
var bigDigits = map[rune][5]string{
	'0': {"███", "█ █", "█ █", "█ █", "███"},
	'1': {" ██", "  █", "  █", "  █", "  █"},
	'2': {"███", "  █", "███", "█  ", "███"},
	'3': {"███", "  █", "███", "  █", "███"},
	'4': {"█ █", "█ █", "███", "  █", "  █"},
	'5': {"███", "█  ", "███", "  █", "███"},
	'6': {"███", "█  ", "███", "█ █", "███"},
	'7': {"███", "  █", "  █", "  █", "  █"},
	'8': {"███", "█ █", "███", "█ █", "███"},
	'9': {"███", "█ █", "███", "  █", "███"},
	'.': {"   ", "   ", "   ", "   ", " █ "},
	'%': {"█ █", "  █", " █ ", "█  ", "█ █"},
	' ': {"   ", "   ", "   ", "   ", "   "},
}

// bigNumber renders text in the block font, coloured.
func bigNumber(s string, c color.Color) string {
	rows := make([]string, 5)
	for _, r := range s {
		glyph, ok := bigDigits[r]
		if !ok {
			glyph = bigDigits[' ']
		}
		for i := range rows {
			rows[i] += glyph[i] + " "
		}
	}
	style := lipgloss.NewStyle().Foreground(c).Bold(true)
	for i := range rows {
		rows[i] = style.Render(rows[i])
	}
	return strings.Join(rows, "\n")
}

// meter draws a proportional bar using eighth-width blocks, so a value lands on
// the nearest eighth of a cell rather than the nearest whole one.
func meter(value float64, width int, c color.Color) string {
	if width <= 0 {
		return ""
	}
	value = clamp(value, 0, 1)
	eighths := int(math.Round(value * float64(width) * 8))
	full, part := eighths/8, eighths%8

	partials := []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}

	var bar strings.Builder
	bar.WriteString(strings.Repeat("█", full))
	if part > 0 && full < width {
		bar.WriteString(partials[part])
	}

	drawn := full
	if part > 0 && full < width {
		drawn++
	}
	rest := width - drawn
	if rest < 0 {
		rest = 0
	}

	return lipgloss.NewStyle().Foreground(c).Render(bar.String()) +
		lipgloss.NewStyle().Foreground(surface).Render(strings.Repeat("─", rest))
}

// statRow is one labelled bar: name, bar, value.
func statRow(name string, value float64, nameWidth, barWidth int, c color.Color, note string) string {
	return fmt.Sprintf("%s %s %s %s",
		label.Width(nameWidth).Render(name),
		meter(value, barWidth, c),
		lipgloss.NewStyle().Foreground(c).Bold(true).Width(6).Render(pct(value)),
		subtle.Render(note),
	)
}

// histogram draws two opposed rows of bars over the same buckets: one for the
// samples that were actually positive, one for the negatives.
//
// This is the shape of the whole result in one picture. Good separation looks
// like two piles at opposite ends with nothing in the middle.
func histogram(positive, negative []int, height int) string {
	buckets := len(positive)
	peak := 1
	for i := range positive {
		peak = max(peak, max(positive[i], negative[i]))
	}

	blocks := []string{" ", "▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	column := func(counts []int, row int, c color.Color, flip bool) string {
		var out strings.Builder
		for i := range buckets {
			// Scale the count into eighths of the full column height.
			units := int(math.Round(float64(counts[i]) / float64(peak) * float64(height) * 8))
			var level int
			if flip {
				level = units - row*8 // filled from the top down
			} else {
				level = units - (height-1-row)*8
			}
			level = int(clamp(float64(level), 0, 8))
			out.WriteString(lipgloss.NewStyle().Foreground(c).Render(blocks[level]))
		}
		return out.String()
	}

	var rows []string
	for r := range height {
		rows = append(rows, "  "+label.Render("inj ")+column(positive, r, bad, false))
	}
	divider := "      " + lipgloss.NewStyle().Foreground(rule).Render(strings.Repeat("╌", buckets))
	rows = append(rows, divider)
	for r := range height {
		rows = append(rows, "  "+label.Render("ok  ")+column(negative, r, good, true))
	}
	rows = append(rows, "      "+axis("0.0  p(hostile)", "1.0", buckets))
	return strings.Join(rows, "\n")
}

// reliability plots claimed probability against observed rate. A perfectly
// calibrated model traces the diagonal; a point above it means the model was
// more right than it said.
func reliability(claimed, actual []float64, counts []int, width, height int) string {
	grid := make([][]string, height)
	for r := range grid {
		grid[r] = make([]string, width)
		for c := range grid[r] {
			grid[r][c] = " "
		}
	}

	// The diagonal first, so points can overwrite it.
	for c := range width {
		r := height - 1 - int(math.Round(float64(c)/float64(width-1)*float64(height-1)))
		if r >= 0 && r < height {
			grid[r][c] = lipgloss.NewStyle().Foreground(surface).Render("·")
		}
	}

	for i := range claimed {
		if counts[i] == 0 {
			continue
		}
		c := int(math.Round(clamp(claimed[i], 0, 1) * float64(width-1)))
		r := height - 1 - int(math.Round(clamp(actual[i], 0, 1)*float64(height-1)))
		if r < 0 || r >= height || c < 0 || c >= width {
			continue
		}
		// Bigger buckets get a heavier mark, so a point carrying 296 samples does
		// not look like one carrying 5.
		glyph := "•"
		switch {
		case counts[i] >= 100:
			glyph = "●"
		case counts[i] >= 25:
			glyph = "◉"
		}
		grid[r][c] = lipgloss.NewStyle().Foreground(accent2).Bold(true).Render(glyph)
	}

	var rows []string
	for r := range height {
		axisLabel := "    "
		switch r {
		case 0:
			axisLabel = subtle.Render("1.0 ")
		case height - 1:
			axisLabel = subtle.Render("0.0 ")
		}
		rows = append(rows, axisLabel+edge.Render("│")+strings.Join(grid[r], ""))
	}
	rows = append(rows, "    "+edge.Render("╰"+strings.Repeat("─", width)))
	return strings.Join(rows, "\n")
}

// confusion draws the four counts as a small matrix.
func confusion(tp, fp, fn, tn int) string {
	const col = 9
	cell := func(v int, c color.Color) string {
		return lipgloss.NewStyle().Foreground(c).Bold(true).
			Width(col).Align(lipgloss.Right).Render(fmt.Sprint(v))
	}
	head := func(s string) string {
		return label.Width(col).Align(lipgloss.Right).Render(s)
	}
	return strings.Join([]string{
		strings.Repeat(" ", 12) + head("flagged") + head("cleared"),
		label.Render("  hostile   ") + cell(tp, good) + cell(fn, bad),
		label.Render("  benign    ") + cell(fp, bad) + cell(tn, good),
	}, "\n")
}

// sparkline draws a compact trend over a series.
func sparkline(values []float64, c color.Color) string {
	if len(values) == 0 {
		return ""
	}
	blocks := []rune("▁▂▃▄▅▆▇█")
	lo, hi := values[0], values[0]
	for _, v := range values {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	span := hi - lo
	if span == 0 {
		span = 1
	}
	var out strings.Builder
	for _, v := range values {
		idx := int((v - lo) / span * float64(len(blocks)-1))
		out.WriteRune(blocks[idx])
	}
	return lipgloss.NewStyle().Foreground(c).Render(out.String())
}

func pct(v float64) string { return fmt.Sprintf("%.1f%%", 100*v) }
func clamp(v, lo, hi float64) float64 {
	return math.Min(math.Max(v, lo), hi)
}

// keyHint renders one key and what it does.
func keyHint(key, what string) string {
	return keyCap.Render(key) + " " + footer.Render(what)
}

// countLines counts the rows in a rendered block.
func countLines(s string) int { return strings.Count(s, "\n") + 1 }

// padLines grows a block to n rows with blank lines, so two panels placed side
// by side end on the same row.
func padLines(content string, n int) string {
	lines := strings.Split(content, "\n")
	for len(lines) < n {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// pairCards renders two panels side by side, matching their heights first.
func pairCards(width int, leftTitle, leftBody, rightTitle, rightBody string) string {
	rows := max(countLines(leftBody), countLines(rightBody))
	half := (width - 1) / 2
	return lipgloss.JoinHorizontal(lipgloss.Top,
		box(leftTitle, half, padLines(leftBody, rows)),
		" ",
		box(rightTitle, half, padLines(rightBody, rows)),
	)
}

// axis renders the bottom label row of a plot at an exact width.
func axis(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return subtle.Render(left) + strings.Repeat(" ", gap) + subtle.Render(right)
}
