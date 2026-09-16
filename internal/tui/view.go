package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Gaurav-Gosain/jev-sec-bench/internal/bench"
)

// View implements tea.Model.
func (m Model) View() tea.View {
	if m.width < 80 || m.height < 24 {
		return m.shell(base.Render(
			fmt.Sprintf("jev-sec-bench needs at least 80x24. This terminal is %dx%d.", m.width, m.height)))
	}

	var body string
	switch m.tab {
	case TabOverview:
		body = m.viewOverview()
	case TabInjection:
		body = m.viewInjection()
	case TabCode:
		body = m.viewCode()
	case TabLive:
		body = m.viewLive()
	}

	return m.shell(lipgloss.JoinVertical(lipgloss.Left,
		m.header(),
		"",
		body,
		"",
		m.footer(),
	))
}

// shell wraps rendered content in the view shell: alt screen, window title and
// the ground the panels are drawn against.
func (m Model) shell(content string) tea.View {
	v := tea.NewView(content)
	v.AltScreen = true
	v.WindowTitle = "jev-sec-bench"
	v.BackgroundColor = canvas
	return v
}

func (m Model) header() string {
	mark := brand.Render("JEV SEC BENCH")

	model := m.results.Injection.Model
	if model == "" {
		model = "no results"
	}
	chip := modelChip.Render(model)

	var tabBar strings.Builder
	for _, t := range tabs {
		style := tabOff
		if t == m.tab {
			style = tabOn
		}
		tabBar.WriteString(style.Render(t.String()))
	}

	left := mark + " " + chip
	right := tabBar.String()
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) footer() string {
	hints := []string{
		keyHint("tab", "switch"),
		keyHint("1-4", "jump"),
	}
	if m.client != nil {
		state := "start live"
		if m.live {
			state = "stop live"
		}
		hints = append(hints, keyHint("space", state))
	}
	hints = append(hints, keyHint("q", "quit"))

	line := strings.Join(hints, footer.Render("  ·  "))
	if m.liveErr != nil {
		line += footer.Render("   ") + lipgloss.NewStyle().Foreground(bad).Render("! "+m.liveErr.Error())
	}
	return line
}

// viewOverview is the screenshot: both headline numbers, the ablation, and the
// calibration curve.
func (m Model) viewOverview() string {
	half := (m.width - 1) / 2
	inner := half - 4 // what fits between the borders and padding

	inj := summarise(m.results.Injection)
	code := summarise(m.results.Code)
	pairs := bench.Pairs(m.results.Code)
	pairAcc := bench.PairAccuracy(pairs)

	injBody := strings.Join([]string{
		bigNumber(pct(inj.At50.Accuracy()), scale(inj.At50.Accuracy())),
		"",
		strong.Render("accuracy") + subtle.Render("  plain 0.50 cut, no tuning"),
		meter(inj.At50.Accuracy(), inner, scale(inj.At50.Accuracy())),
		"",
		label.Render("auc ") + strong.Render(fmt.Sprintf("%.4f", inj.AUC)) +
			label.Render("  ece ") + strong.Render(fmt.Sprintf("%.4f", inj.ECE)) +
			label.Render("  p50 ") + strong.Render(fmt.Sprintf("%dms", m.results.Injection.P50MS)),
		subtle.Render(fmt.Sprintf("%d messages, %d hostile, %d fp, %d fn",
			inj.Samples, inj.Positives, inj.At50.FP, inj.At50.FN)),
	}, "\n")

	codeBody := strings.Join([]string{
		bigNumber(pct(pairAcc), scale(pairAcc)),
		"",
		strong.Render("pairwise") + subtle.Render("  bug ranked over its twin"),
		meter(pairAcc, inner, scale(pairAcc)),
		"",
		label.Render("auc ") + strong.Render(fmt.Sprintf("%.4f", code.AUC)) +
			label.Render("  pairs ") + strong.Render(fmt.Sprint(len(pairs))) +
			label.Render("  p50 ") + strong.Render(fmt.Sprintf("%dms", m.results.Code.P50MS)),
		subtle.Render("same task, same language, blind"),
	}, "\n")

	top := pairCards(m.width, "PROMPT INJECTION", injBody, "VULNERABLE CODE", codeBody)

	// The ablation: the same questions, with and without the deployment context.
	ablation := subtle.Render("run with -ablation to fill this in")
	if m.results.HasAblation {
		bare := summarise(m.results.NoContext)
		barW := inner - 30
		ablation = strings.Join([]string{
			subtle.Render("same model, same questions. the only"),
			subtle.Render("change is whether the request says"),
			subtle.Render("what the assistant is for."),
			"",
			statRow("recall", bare.At50.Recall(), 9, barW, bad, "bare"),
			statRow("", inj.At50.Recall(), 9, barW, good, "+context"),
			"",
			statRow("accuracy", bare.At50.Accuracy(), 9, barW, bad, "bare"),
			statRow("", inj.At50.Accuracy(), 9, barW, good, "+context"),
			"",
			label.Render("auc  ") + strong.Render(fmt.Sprintf("%.4f", bare.AUC)) +
				subtle.Render(" -> ") + strong.Render(fmt.Sprintf("%.4f", inj.AUC)),
			label.Render("ece  ") + strong.Render(fmt.Sprintf("%.4f", bare.ECE)) +
				subtle.Render(" -> ") + strong.Render(fmt.Sprintf("%.4f", inj.ECE)),
			"",
			subtle.Render("auc barely moves, so the ordering was"),
			subtle.Render("already right. context is what puts the"),
			subtle.Render("probabilities where a fixed cut works."),
		}, "\n")
	}

	claimed, actual, counts := calibrationSeries(m.results.Injection)
	plotW := inner - 5
	calibration := strings.Join([]string{
		subtle.Render("claimed probability vs what happened"),
		"",
		reliability(claimed, actual, counts, plotW, 8),
		axis("     0.0", "claimed 1.0", inner),
		"",
		subtle.Render("above the line means it was more"),
		subtle.Render("right than it claimed to be"),
	}, "\n")

	bottom := pairCards(m.width, "CONTEXT AS STATE", ablation, "CALIBRATION", calibration)

	return lipgloss.JoinVertical(lipgloss.Left, top, "", bottom)
}

func (m Model) viewInjection() string {
	r := m.results.Injection
	s := summarise(r)
	half := (m.width - 1) / 2

	pos, neg := histogramBuckets(r, m.width-14)
	distCard := box("SCORE DISTRIBUTION", m.width, strings.Join([]string{
		subtle.Render("where every message landed. two piles at opposite ends means a fixed threshold works."),
		"",
		histogram(pos, neg, 5),
	}, "\n"))

	statsBody := strings.Join([]string{
		confusion(s.At50.TP, s.At50.FP, s.At50.FN, s.At50.TN),
		"",
		statRow("accuracy", s.At50.Accuracy(), 10, half-24, scale(s.At50.Accuracy()), ""),
		"",
		statRow("precision", s.At50.Precision(), 10, half-24, scale(s.At50.Precision()), ""),
		"",
		statRow("recall", s.At50.Recall(), 10, half-24, scale(s.At50.Recall()), ""),
		"",
		statRow("f1", s.At50.F1(), 10, half-24, scale(s.At50.F1()), ""),
	}, "\n")

	// The messages it got most wrong, which is the honest part of any benchmark.
	var misses []string
	misses = append(misses, subtle.Render("the ones it got most wrong"))
	misses = append(misses, "")
	for _, s := range worstMisses(r, 6) {
		tag := lipgloss.NewStyle().Foreground(bad).Render("miss")
		if s.Label == 0 {
			tag = lipgloss.NewStyle().Foreground(warn).Render("fp  ")
		}
		misses = append(misses, fmt.Sprintf("%s %s %s",
			tag,
			lipgloss.NewStyle().Foreground(verdict(s.Probability)).Bold(true).Render(fmt.Sprintf("%.2f", s.Probability)),
			subtle.Render(truncate(collapse(s.Text), half-14)),
		))
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		distCard, "",
		pairCards(m.width, "AT A 0.50 CUT", statsBody, "WORST CALLS", strings.Join(misses, "\n")),
	)
}

func (m Model) viewCode() string {
	r := m.results.Code
	half := (m.width - 1) / 2
	pairs := bench.Pairs(r)

	// Per class pairwise accuracy.
	type row struct {
		name    string
		correct int
		total   int
	}
	byClass := map[string]*row{}
	for _, p := range pairs {
		if byClass[p.Class] == nil {
			byClass[p.Class] = &row{name: p.Class}
		}
		byClass[p.Class].total++
		if p.Correct() {
			byClass[p.Class].correct++
		}
	}
	var rows []*row
	for _, r := range byClass {
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool {
		a := float64(rows[i].correct) / float64(rows[i].total)
		b := float64(rows[j].correct) / float64(rows[j].total)
		if a != b {
			return a > b
		}
		return rows[i].total > rows[j].total
	})

	var classLines []string
	classLines = append(classLines, subtle.Render("pairs ranked the right way round"), "")
	for _, r := range rows {
		v := float64(r.correct) / float64(r.total)
		classLines = append(classLines, statRow(r.name, v, 18, half-36, scale(v),
			fmt.Sprintf("%d/%d", r.correct, r.total)), "")
	}
	classBody := strings.Join(classLines, "\n")

	// The audit, which is the most interesting panel here.
	a := m.results.Audit
	flagged := float64(a.FlaggedStillDangerous+a.FlaggedWeakFix) / float64(max(1, a.Flagged))
	auditLines := []string{
		subtle.Render("checking the halves the corpus calls secure"),
		"",
		label.Render("labelled secure      ") + strong.Render(fmt.Sprint(a.SecureSamples)),
		label.Render("jev flagged          ") + lipgloss.NewStyle().Foreground(warn).Bold(true).Render(fmt.Sprint(a.Flagged)),
		label.Render("  still exploitable  ") + lipgloss.NewStyle().Foreground(bad).Bold(true).Render(fmt.Sprint(a.FlaggedStillDangerous)),
		label.Render("  weak defence       ") + lipgloss.NewStyle().Foreground(warn).Bold(true).Render(fmt.Sprint(a.FlaggedWeakFix)),
		label.Render("genuine misses       ") + strong.Render(fmt.Sprint(a.ClearedButDangerous)),
		"",
		meter(flagged, half-6, bad),
		lipgloss.NewStyle().Foreground(bad).Bold(true).Render(pct(flagged)) +
			subtle.Render(" of the flags are label errors,"),
		subtle.Render("proven by rule. the real number is higher."),
	}
	auditBody := strings.Join(auditLines, "\n")

	// Real examples of code the corpus calls secure that is not.
	var examples []string
	examples = append(examples, subtle.Render("labelled secure by the corpus. jev disagreed, and jev is right."), "")
	for _, s := range mislabelled(r, 4) {
		examples = append(examples,
			lipgloss.NewStyle().Foreground(bad).Bold(true).Render(fmt.Sprintf("%.2f", s.Probability))+
				" "+lipgloss.NewStyle().Foreground(info).Render(fmt.Sprintf("%-11s", s.Language))+
				subtle.Render(strictRule(s.Code)),
			"     "+lipgloss.NewStyle().Foreground(dim).Render(interesting(s.Code, m.width-12)),
		)
	}
	exampleCard := box("WHAT IT CAUGHT THAT THE BENCHMARK MISSED", m.width, strings.Join(examples, "\n"))

	return lipgloss.JoinVertical(lipgloss.Left,
		pairCards(m.width, "BY VULNERABILITY CLASS", classBody, "LABEL AUDIT", auditBody),
		"",
		exampleCard,
	)
}

func (m Model) viewLive() string {
	status := lipgloss.NewStyle().Foreground(muted).Render("paused")
	if m.live {
		spinner := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		status = lipgloss.NewStyle().Foreground(good).Bold(true).
			Render(spinner[m.frame%len(spinner)] + " streaming")
	}

	head := status + subtle.Render("   press space to ") +
		strong.Render(map[bool]string{true: "stop", false: "start"}[m.live]) +
		subtle.Render("   each row is one real request to jev")

	var rows []string
	rows = append(rows, head, "")

	if len(m.feed) == 0 {
		rows = append(rows, subtle.Render("no samples yet"))
	}

	barW := max(10, m.width/4)
	for _, s := range m.feed {
		mark := lipgloss.NewStyle().Foreground(good).Render("✓")
		if !s.Correct() {
			mark = lipgloss.NewStyle().Foreground(bad).Render("✗")
		}
		kind := lipgloss.NewStyle().Foreground(info).Render(fmt.Sprintf("%-9s", s.Kind))
		truth := subtle.Render("benign")
		if s.Label == 1 {
			truth = lipgloss.NewStyle().Foreground(bad).Render("hostile")
		}
		rows = append(rows, fmt.Sprintf("%s %s %s %s %s  %s  %s",
			mark,
			kind,
			meter(s.Probability, barW, verdict(s.Probability)),
			lipgloss.NewStyle().Foreground(verdict(s.Probability)).Bold(true).Width(5).Render(fmt.Sprintf("%.2f", s.Probability)),
			lipgloss.NewStyle().Width(7).Render(truth),
			subtle.Render(fmt.Sprintf("%4dms", s.Latency.Milliseconds())),
			subtle.Render(truncate(collapse(unfence(s.Text)), max(10, m.width-barW-46))),
		))
	}

	// A running tally, so the panel says something even mid stream.
	var correct int
	var latencies []float64
	for _, s := range m.feed {
		if s.Correct() {
			correct++
		}
		latencies = append(latencies, float64(s.Latency.Milliseconds()))
	}
	tally := subtle.Render("no samples yet")
	if len(m.feed) > 0 {
		tally = label.Render("this session  ") +
			strong.Render(fmt.Sprintf("%d/%d", correct, len(m.feed))) +
			subtle.Render(" correct at a 0.50 cut   ") +
			label.Render("latency ") + sparkline(latencies, accent2)
	}

	return box("LIVE FEED", m.width, strings.Join(rows, "\n")) + "\n\n" +
		box("SESSION", m.width, tally)
}

// worstMisses returns the samples furthest from their own label.
func worstMisses(r bench.Result, limit int) []bench.Scored {
	var out []bench.Scored
	for _, s := range r.Samples {
		if (s.Probability >= 0.5) != (s.Label == 1) {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		di := absDiff(out[i].Probability, float64(out[i].Label))
		dj := absDiff(out[j].Probability, float64(out[j].Label))
		return di > dj
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func absDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}

// collapse flattens whitespace so a multi line sample fits one row.
func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// unfence drops the markdown code fence the corpus wraps every snippet in, so
// the feed shows code rather than backticks.
func unfence(s string) string {
	var kept []string
	for line := range strings.SplitSeq(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
