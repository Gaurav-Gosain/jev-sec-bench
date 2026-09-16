package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/Gaurav-Gosain/jev-sec-bench/internal/bench"
)

func TestBoxIsRectangular(t *testing.T) {
	t.Parallel()
	// Lines of different lengths, one of them styled, must all come out the same
	// visible width or the right border goes ragged.
	content := strings.Join([]string{
		"short",
		strong.Render("a styled line that is longer"),
		"",
	}, "\n")

	out := box("TITLE", 40, content)
	for i, line := range strings.Split(out, "\n") {
		if got := lipgloss.Width(line); got != 40 {
			t.Errorf("line %d is %d wide, want 40: %q", i, got, line)
		}
	}
}

func TestBoxTruncatesRatherThanOverflowing(t *testing.T) {
	t.Parallel()
	out := box("T", 20, strings.Repeat("x", 200))
	for _, line := range strings.Split(out, "\n") {
		if got := lipgloss.Width(line); got != 20 {
			t.Errorf("line is %d wide, want 20", got)
		}
	}
}

func TestMeterFillsProportionally(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		value float64
		full  int
	}{
		{0, 0},
		{0.5, 10},
		{1, 20},
	} {
		got := strings.Count(stripStyle(meter(tc.value, 20, good)), "█")
		if got != tc.full {
			t.Errorf("meter(%.2f) drew %d full blocks, want %d", tc.value, got, tc.full)
		}
	}
}

func TestMeterIsAlwaysItsFullWidth(t *testing.T) {
	t.Parallel()
	// Partial blocks must not change the cell count, or bars in a column stop
	// lining up with each other.
	for _, v := range []float64{0, 0.013, 0.37, 0.499, 0.5, 0.878, 0.999, 1} {
		if got := lipgloss.Width(meter(v, 24, good)); got != 24 {
			t.Errorf("meter(%.3f) is %d cells wide, want 24", v, got)
		}
	}
}

func TestMeterClampsOutOfRangeValues(t *testing.T) {
	t.Parallel()
	if got := lipgloss.Width(meter(-3, 10, good)); got != 10 {
		t.Errorf("a negative value gave width %d, want 10", got)
	}
	if got := strings.Count(stripStyle(meter(9, 10, good)), "█"); got != 10 {
		t.Errorf("a value above 1 drew %d blocks, want 10", got)
	}
}

func TestTruncateCountsVisibleCellsNotBytes(t *testing.T) {
	t.Parallel()
	styled := strong.Render("abcdefghij")
	got := truncate(styled, 4)
	if w := lipgloss.Width(got); w != 4 {
		t.Errorf("truncated to %d cells, want 4", w)
	}
	if !strings.Contains(got, "abcd") {
		t.Errorf("lost the visible text: %q", got)
	}
}

func TestTruncateLeavesShortStringsAlone(t *testing.T) {
	t.Parallel()
	if got := truncate("abc", 10); got != "abc" {
		t.Errorf("truncate padded or cut a short string: %q", got)
	}
}

func TestBigNumberIsFiveRows(t *testing.T) {
	t.Parallel()
	out := bigNumber("96.5%", good)
	if got := len(strings.Split(out, "\n")); got != 5 {
		t.Errorf("bigNumber drew %d rows, want 5", got)
	}
}

func TestBigNumberHandlesUnknownRunes(t *testing.T) {
	t.Parallel()
	// An unmapped rune must fall back to a blank cell rather than panicking.
	out := bigNumber("9z", good)
	if got := len(strings.Split(out, "\n")); got != 5 {
		t.Errorf("bigNumber drew %d rows, want 5", got)
	}
}

func TestPairCardsMatchHeights(t *testing.T) {
	t.Parallel()
	left := box("L", 30, padLines("one line", 5))
	right := box("R", 30, padLines(strings.Repeat("row\n", 4)+"row", 5))
	if lipgloss.Height(left) != lipgloss.Height(right) {
		t.Errorf("padded panels differ in height: %d vs %d",
			lipgloss.Height(left), lipgloss.Height(right))
	}

	joined := pairCards(61, "L", "one line", "R", strings.Repeat("row\n", 6)+"row")
	widths := map[int]bool{}
	for _, line := range strings.Split(joined, "\n") {
		widths[lipgloss.Width(line)] = true
	}
	if len(widths) != 1 {
		t.Errorf("pairCards produced ragged lines, widths seen: %v", widths)
	}
}

func TestAxisFillsExactly(t *testing.T) {
	t.Parallel()
	if got := lipgloss.Width(axis("0.0", "1.0", 40)); got != 40 {
		t.Errorf("axis is %d wide, want 40", got)
	}
}

func TestHistogramBucketsSplitByLabel(t *testing.T) {
	t.Parallel()
	r := bench.Result{Samples: []bench.Scored{
		{Label: 1, Probability: 0.95},
		{Label: 1, Probability: 0.91},
		{Label: 0, Probability: 0.02},
	}}
	pos, neg := histogramBuckets(r, 10)
	if pos[9] != 2 {
		t.Errorf("top bucket holds %d positives, want 2", pos[9])
	}
	if neg[0] != 1 {
		t.Errorf("bottom bucket holds %d negatives, want 1", neg[0])
	}
}

func TestHistogramBucketsPutOneInTheTopBucket(t *testing.T) {
	t.Parallel()
	// A probability of exactly 1.0 must not index past the end of the slice.
	r := bench.Result{Samples: []bench.Scored{{Label: 1, Probability: 1.0}}}
	pos, _ := histogramBuckets(r, 10)
	if pos[9] != 1 {
		t.Errorf("a probability of 1.0 landed outside the top bucket: %v", pos)
	}
}

func TestInterestingPrefersTheDangerousLine(t *testing.T) {
	t.Parallel()
	code := strings.Join([]string{
		"```python",
		"import os",
		"def safe_eval(user_input):",
		"    result = eval(user_input)",
		"    return result",
		"```",
	}, "\n")

	got := interesting(code, 80)
	if !strings.HasPrefix(got, "result = eval") {
		t.Errorf("interesting picked %q, want the eval call rather than the def", got)
	}
}

func TestInterestingFallsBackToTheNamedConstruct(t *testing.T) {
	t.Parallel()
	// The BinaryFormatter rule spans several lines, so no single line trips it.
	code := strings.Join([]string{
		"using System;",
		"var formatter = new BinaryFormatter();",
		"var obj = formatter.Deserialize(stream);",
	}, "\n")

	got := interesting(code, 80)
	if !strings.Contains(got, "BinaryFormatter") {
		t.Errorf("interesting picked %q, want the BinaryFormatter line", got)
	}
}

func TestUnfenceDropsMarkdownFences(t *testing.T) {
	t.Parallel()
	got := unfence("```go\nfmt.Println()\n```")
	if strings.Contains(got, "```") {
		t.Errorf("fence survived: %q", got)
	}
	if !strings.Contains(got, "fmt.Println()") {
		t.Errorf("lost the code: %q", got)
	}
}

func TestParseTab(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]Tab{
		"overview":  TabOverview,
		"injection": TabInjection,
		"code":      TabCode,
		"live":      TabLive,
	} {
		got, ok := ParseTab(name)
		if !ok || got != want {
			t.Errorf("ParseTab(%q) = %v, %v", name, got, ok)
		}
	}
	if _, ok := ParseTab("nope"); ok {
		t.Error("ParseTab accepted an unknown tab")
	}
}

func TestVerdictBandsMatchTheRoutingPolicy(t *testing.T) {
	t.Parallel()
	if verdict(0.9) != bad {
		t.Error("0.90 should read as a block")
	}
	if verdict(0.5) != warn {
		t.Error("0.50 should read as a review")
	}
	if verdict(0.1) != good {
		t.Error("0.10 should read as a pass")
	}
}

// TestRenderEveryTab is the guard that matters for a screenshot: each tab has to
// draw at a realistic size without panicking and without spilling past the width.
func TestRenderEveryTab(t *testing.T) {
	t.Parallel()
	m := New(sampleResults(), nil)

	for _, tab := range tabs {
		t.Run(tab.String(), func(t *testing.T) {
			t.Parallel()
			out := m.Render(118, 44, tab)
			if out == "" {
				t.Fatal("rendered nothing")
			}
			for i, line := range strings.Split(out, "\n") {
				if got := lipgloss.Width(line); got > 118 {
					t.Errorf("line %d is %d cells wide, past the 118 terminal: %q",
						i, got, stripStyle(line))
				}
			}
		})
	}
}

func TestRenderRefusesATinyTerminal(t *testing.T) {
	t.Parallel()
	m := New(sampleResults(), nil)
	if !strings.Contains(stripStyle(m.Render(40, 10, TabOverview)), "80x24") {
		t.Error("a small terminal should say what size it needs")
	}
}

// sampleResults is a small stand-in so the render tests do not need the
// committed JSON or a network call.
func sampleResults() Results {
	inj := bench.Result{Name: "prompt-injection", Model: "jev-test", P50MS: 300}
	code := bench.Result{Name: "vulnerable-code", Model: "jev-test", P50MS: 300}
	for i := range 40 {
		hostile := i%2 == 0
		p := 0.05
		label := 0
		if hostile {
			p, label = 0.95, 1
		}
		inj.Samples = append(inj.Samples, bench.Scored{
			Label: label, Probability: p, Severity: 1, Text: "a message",
		})
		code.Samples = append(code.Samples, bench.Scored{
			Label: label, Probability: p, Severity: 1, PairID: i / 2,
			Language: "go", Class: "SQL injection",
			Code: "```go\nq := \"SELECT * FROM t WHERE a = \" + x\n```",
		})
	}
	return Results{
		Injection:   inj,
		NoContext:   inj,
		Code:        code,
		Audit:       bench.RunAudit(code),
		HasAblation: true,
	}
}

// stripStyle removes ANSI sequences so a test can assert on visible text.
func stripStyle(s string) string {
	var out strings.Builder
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
			out.WriteRune(r)
		}
	}
	return out.String()
}
