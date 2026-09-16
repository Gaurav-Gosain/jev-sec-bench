// Package ansisvg turns a rendered terminal frame into an SVG.
//
// The dashboard already knows how to draw itself into a string of text and ANSI
// colour codes. This converts that string into a picture, so the screenshots in
// the README are produced by the same code that produced the numbers and cannot
// drift away from them.
//
// Only the escape sequences lipgloss emits are handled: reset, bold, faint, and
// 24 bit foreground and background. Anything else is skipped rather than drawn.
package ansisvg

import (
	"fmt"
	"html"
	"image/color"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"
)

// Options controls the rendered picture.
type Options struct {
	// FontSize in pixels.
	FontSize float64
	// FontFamily is a CSS font stack. The first font present on the rasterising
	// machine wins, so list a few and end with monospace.
	FontFamily string
	// Padding around the frame, in pixels.
	Padding float64
	// Background is the page colour behind the frame.
	Background color.Color
	// Foreground is the colour for text that never set one.
	Foreground color.Color
	// Radius rounds the window corners.
	Radius float64
}

// Default returns sensible options for a dark dashboard.
func Default() Options {
	return Options{
		FontSize:   15,
		FontFamily: "JetBrains Mono,Fira Code,SFMono-Regular,Menlo,DejaVu Sans Mono,monospace",
		Padding:    28,
		Background: color.RGBA{R: 0x20, G: 0x1F, B: 0x26, A: 0xFF},
		Foreground: color.RGBA{R: 0xF7, G: 0xF6, B: 0xFB, A: 0xFF},
		Radius:     10,
	}
}

// cellWidth is the advance of one character as a fraction of the font size.
// Every run is drawn with an explicit textLength, so this only has to be close
// enough for the glyphs to look right, not exact for the layout to line up.
const cellWidth = 0.6

// lineHeight is the baseline to baseline distance as a fraction of the font size.
const lineHeight = 1.32

// blockRect describes the part of a cell a block drawing character fills, as
// fractions of the cell. Drawing these as rectangles rather than glyphs is what
// makes bars and the big digits tile exactly: text lines are spaced by the line
// height, so a glyph that fills its cell still leaves a gap above and below.
type blockRect struct{ x, y, w, h float64 }

var blocks = map[rune]blockRect{
	'\u2588': {0, 0, 1, 1},       // full block
	'\u2589': {0, 0, 7.0 / 8, 1}, // left seven eighths
	'\u258a': {0, 0, 6.0 / 8, 1},
	'\u258b': {0, 0, 5.0 / 8, 1},
	'\u258c': {0, 0, 4.0 / 8, 1}, // left half
	'\u258d': {0, 0, 3.0 / 8, 1},
	'\u258e': {0, 0, 2.0 / 8, 1},
	'\u258f': {0, 0, 1.0 / 8, 1},
	'\u2580': {0, 0, 1, 0.5},           // upper half
	'\u2581': {0, 7.0 / 8, 1, 1.0 / 8}, // lower one eighth
	'\u2582': {0, 6.0 / 8, 1, 2.0 / 8},
	'\u2583': {0, 5.0 / 8, 1, 3.0 / 8},
	'\u2584': {0, 4.0 / 8, 1, 4.0 / 8}, // lower half
	'\u2585': {0, 3.0 / 8, 1, 5.0 / 8},
	'\u2586': {0, 2.0 / 8, 1, 6.0 / 8},
	'\u2587': {0, 1.0 / 8, 1, 7.0 / 8},
}

// span is a run of characters sharing one style.
type span struct {
	col   int
	text  string
	width int
	fg    color.Color
	bg    color.Color
	bold  bool
	faint bool
}

// Render converts a frame of ANSI text into an SVG document.
func Render(frame string, opts Options) string {
	lines := strings.Split(strings.TrimRight(frame, "\n"), "\n")

	rows := make([][]span, 0, len(lines))
	widest := 0
	for _, line := range lines {
		spans := parseLine(line)
		rows = append(rows, spans)
		if w := lineEnd(spans); w > widest {
			widest = w
		}
	}

	cw := opts.FontSize * cellWidth
	ch := opts.FontSize * lineHeight
	width := float64(widest)*cw + 2*opts.Padding
	height := float64(len(rows))*ch + 2*opts.Padding

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%.0f" `+
		`viewBox="0 0 %.0f %.0f" font-family="%s" font-size="%.2f">`,
		width, height, width, height, html.EscapeString(opts.FontFamily), opts.FontSize)

	fmt.Fprintf(&b, `<rect width="%.0f" height="%.0f" rx="%.1f" fill="%s"/>`,
		width, height, opts.Radius, hex(opts.Background))

	// Backgrounds first, as one pass, so text never lands under a later fill.
	for r, spans := range rows {
		y := opts.Padding + float64(r)*ch
		for _, s := range spans {
			if s.bg == nil {
				continue
			}
			fmt.Fprintf(&b, `<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" fill="%s"/>`,
				opts.Padding+float64(s.col)*cw, y, float64(s.width)*cw, ch, hex(s.bg))
		}
	}

	// Then the content. Block characters become rectangles so they tile exactly;
	// everything else is text pinned to its column with an explicit advance, so a
	// missing font cannot shift the columns out of alignment.
	for r, spans := range rows {
		top := opts.Padding + float64(r)*ch
		baseline := top + opts.FontSize*0.98
		for _, s := range spans {
			fg := s.fg
			if fg == nil {
				fg = opts.Foreground
			}
			for _, run := range splitBlocks(s) {
				x := opts.Padding + float64(run.col)*cw
				if run.block {
					for i, glyph := range []rune(run.text) {
						rect := blocks[glyph]
						fmt.Fprintf(&b,
							`<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" fill="%s"/>`,
							x+float64(i)*cw+rect.x*cw, top+rect.y*ch,
							rect.w*cw, rect.h*ch, hex(fg))
					}
					continue
				}
				if strings.TrimSpace(run.text) == "" {
					continue
				}
				fmt.Fprintf(&b, `<text x="%.2f" y="%.2f" fill="%s"`, x, baseline, hex(fg))
				if s.bold {
					b.WriteString(` font-weight="bold"`)
				}
				if s.faint {
					b.WriteString(` opacity="0.6"`)
				}
				fmt.Fprintf(&b, ` textLength="%.2f" lengthAdjust="spacingAndGlyphs"`+
					` xml:space="preserve">%s</text>`,
					float64(run.width)*cw, html.EscapeString(run.text))
			}
		}
	}

	b.WriteString(`</svg>`)
	return b.String()
}

// run is a piece of a span that is either all block characters or none.
type run struct {
	col   int
	text  string
	width int
	block bool
}

// splitBlocks divides a span so block characters can be drawn as rectangles.
func splitBlocks(s span) []run {
	var runs []run
	var current run
	current.col = s.col
	col := s.col

	for _, r := range s.text {
		_, isBlock := blocks[r]
		if current.text != "" && isBlock != current.block {
			runs = append(runs, current)
			current = run{col: col, block: isBlock}
		}
		if current.text == "" {
			current.col, current.block = col, isBlock
		}
		w := runewidth.RuneWidth(r)
		if w == 0 {
			w = 1
		}
		current.text += string(r)
		current.width += w
		col += w
	}
	if current.text != "" {
		runs = append(runs, current)
	}
	return runs
}

// lineEnd is the column just past the last span on a line.
func lineEnd(spans []span) int {
	if len(spans) == 0 {
		return 0
	}
	last := spans[len(spans)-1]
	return last.col + last.width
}

// parseLine splits one line into styled runs.
func parseLine(line string) []span {
	var (
		spans   []span
		current span
		col     int
		fg, bg  color.Color
		bold    bool
		faint   bool
	)

	flush := func() {
		if current.text != "" {
			spans = append(spans, current)
		}
		current = span{}
	}

	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\x1b' && i+1 < len(runes) && runes[i+1] == '[' {
			end := i + 2
			for end < len(runes) && runes[end] != 'm' {
				end++
			}
			if end >= len(runes) {
				break // an unterminated sequence; nothing sensible left to draw
			}
			flush()
			fg, bg, bold, faint = applySGR(string(runes[i+2:end]), fg, bg, bold, faint)
			i = end
			continue
		}

		w := runewidth.RuneWidth(runes[i])
		if w == 0 {
			w = 1
		}
		if current.text == "" {
			current = span{col: col, fg: fg, bg: bg, bold: bold, faint: faint}
		}
		current.text += string(runes[i])
		current.width += w
		col += w
	}
	flush()
	return spans
}

// applySGR folds one escape sequence's parameters into the current style.
func applySGR(params string, fg, bg color.Color, bold, faint bool) (color.Color, color.Color, bool, bool) {
	if params == "" {
		return nil, nil, false, false
	}
	parts := strings.Split(params, ";")
	for i := 0; i < len(parts); i++ {
		switch parts[i] {
		case "0", "":
			fg, bg, bold, faint = nil, nil, false, false
		case "1":
			bold = true
		case "2":
			faint = true
		case "22":
			bold, faint = false, false
		case "39":
			fg = nil
		case "49":
			bg = nil
		case "38", "48":
			// Truecolor: 38;2;r;g;b or 48;2;r;g;b.
			if i+4 >= len(parts) || parts[i+1] != "2" {
				continue
			}
			c := color.RGBA{
				R: byteOf(parts[i+2]),
				G: byteOf(parts[i+3]),
				B: byteOf(parts[i+4]),
				A: 0xFF,
			}
			if parts[i] == "38" {
				fg = c
			} else {
				bg = c
			}
			i += 4
		}
	}
	return fg, bg, bold, faint
}

func byteOf(s string) uint8 {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 255 {
		return 0
	}
	return uint8(n)
}

func hex(c color.Color) string {
	if c == nil {
		return "none"
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}
