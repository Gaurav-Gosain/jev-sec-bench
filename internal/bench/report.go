package bench

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Gaurav-Gosain/jev-sec-bench/internal/audit"
	"github.com/Gaurav-Gosain/jev-sec-bench/internal/metrics"
)

func pct(v float64) string { return fmt.Sprintf("%.1f%%", 100*v) }

// WriteSummary prints the headline numbers for a result.
func WriteSummary(w io.Writer, r Result) {
	s := Summarise(r)

	fmt.Fprintf(w, "\n%s  [%s]\n", strings.ToUpper(r.Name), r.Model)
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", 64))
	if r.Notes != "" {
		fmt.Fprintf(w, "  %s\n", r.Notes)
	}
	fmt.Fprintf(w, "  %d samples, %d positive, %d negative",
		s.Samples, s.Positives, s.Samples-s.Positives)
	if r.Failed > 0 {
		fmt.Fprintf(w, ", %d failed", r.Failed)
	}
	fmt.Fprintln(w)

	fmt.Fprintf(w, "\n  ROC-AUC            %.4f   (ordering, threshold free)\n", s.AUC)
	fmt.Fprintf(w, "  accuracy @ 0.50    %s\n", pct(s.At50.Accuracy()))
	fmt.Fprintf(w, "  precision @ 0.50   %s\n", pct(s.At50.Precision()))
	fmt.Fprintf(w, "  recall @ 0.50      %s\n", pct(s.At50.Recall()))
	fmt.Fprintf(w, "  F1 @ 0.50          %s   (tp %d, fp %d, fn %d, tn %d)\n",
		pct(s.At50.F1()), s.At50.TP, s.At50.FP, s.At50.FN, s.At50.TN)
	fmt.Fprintf(w, "  best F1            %s at threshold %.2f (fitted on this data)\n",
		pct(s.BestF1), s.BestAt)
	fmt.Fprintf(w, "  ECE                %.4f   (calibration gap, lower is better)\n", s.ECE)
	fmt.Fprintf(w, "  Brier              %.4f\n", s.Brier)

	fmt.Fprintf(w, "\n  %d requests in %.1fs, p50 %dms, p95 %dms, %d input tokens\n",
		r.Requests, r.WallSeconds, r.P50MS, r.P95MS, r.InputTokens)
}

// WriteCalibration prints a reliability table: what the model claimed against
// what actually happened.
func WriteCalibration(w io.Writer, r Result) {
	fmt.Fprintf(w, "\n  calibration (claimed probability against observed rate)\n")
	fmt.Fprintf(w, "    %-12s %6s  %8s  %8s\n", "bucket", "n", "claimed", "actual")
	for _, b := range metrics.Calibrate(r.Probabilities(), r.Labels(), 10) {
		if b.Count == 0 {
			continue
		}
		fmt.Fprintf(w, "    %.1f - %.1f    %6d  %7.2f   %7.2f\n",
			b.Low, b.High, b.Count, b.MeanProb, b.ActualRate)
	}
}

// WritePairs prints the matched pair results, overall and broken down.
func WritePairs(w io.Writer, r Result) {
	pairs := Pairs(r)
	if len(pairs) == 0 {
		return
	}

	var correct, tied int
	for _, p := range pairs {
		if p.Correct() {
			correct++
		}
		if p.Tied() {
			tied++
		}
	}

	fmt.Fprintf(w, "\n  matched pairs: the vulnerable half scored above its own secure twin\n")
	fmt.Fprintf(w, "  in %d of %d pairs = %s  (%d ties)\n",
		correct, len(pairs), pct(PairAccuracy(pairs)), tied)

	writeGrouped(w, "by vulnerability class", pairs, func(p PairOutcome) string { return p.Class })
	writeGrouped(w, "by language", pairs, func(p PairOutcome) string { return p.Language })
}

func writeGrouped(w io.Writer, title string, pairs []PairOutcome, key func(PairOutcome) string) {
	type tally struct{ correct, total int }
	groups := map[string]*tally{}
	for _, p := range pairs {
		k := key(p)
		if groups[k] == nil {
			groups[k] = &tally{}
		}
		groups[k].total++
		if p.Correct() {
			groups[k].correct++
		}
	}

	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if groups[names[i]].total != groups[names[j]].total {
			return groups[names[i]].total > groups[names[j]].total
		}
		return names[i] < names[j]
	})

	fmt.Fprintf(w, "\n    %s\n", title)
	for _, name := range names {
		g := groups[name]
		fmt.Fprintf(w, "      %-24s %3d/%-3d  %s\n",
			name, g.correct, g.total, pct(float64(g.correct)/float64(g.total)))
	}
}

// AuditReport is what the label audit found.
type AuditReport struct {
	SecureSamples int `json:"secure_samples"`
	Flagged       int `json:"flagged"`
	// FlaggedStillDangerous counts flagged samples where a rule admitting no safe
	// reading fired, so the corpus label is wrong rather than the model.
	FlaggedStillDangerous int `json:"flagged_still_dangerous"`
	// FlaggedWeakFix counts flagged samples carrying a well known weak defence.
	FlaggedWeakFix int `json:"flagged_weak_fix"`
	// ClearedButDangerous counts samples Jev cleared where a strict rule fires,
	// which are genuine misses.
	ClearedButDangerous int            `json:"cleared_but_dangerous"`
	ByRule              map[string]int `json:"by_rule"`
}

// RunAudit checks every sample the corpus labelled secure.
func RunAudit(r Result) AuditReport {
	report := AuditReport{ByRule: map[string]int{}}

	for _, s := range r.Samples {
		if s.Label != 0 {
			continue // only the halves the corpus calls secure are in question
		}
		report.SecureSamples++

		flagged := s.Probability >= 0.5
		findings := audit.Scan(s.Code)
		strict := audit.HasStrict(s.Code)

		switch {
		case flagged:
			report.Flagged++
			for _, f := range findings {
				report.ByRule[f.Rule]++
			}
			if strict {
				report.FlaggedStillDangerous++
			} else if len(findings) > 0 {
				report.FlaggedWeakFix++
			}
		case strict:
			report.ClearedButDangerous++
		}
	}
	return report
}

// WriteAudit prints the label audit.
func WriteAudit(w io.Writer, report AuditReport) {
	fmt.Fprintf(w, "\n  label audit of the halves the corpus calls secure\n")
	fmt.Fprintf(w, "    samples labelled secure                 %d\n", report.SecureSamples)
	fmt.Fprintf(w, "    of those, Jev flagged                   %d\n", report.Flagged)
	if report.Flagged > 0 {
		fmt.Fprintf(w, "      still exploitable by a strict rule    %d  (%s of the flags)\n",
			report.FlaggedStillDangerous,
			pct(float64(report.FlaggedStillDangerous)/float64(report.Flagged)))
		fmt.Fprintf(w, "      carrying a known weak defence         %d  (%s of the flags)\n",
			report.FlaggedWeakFix,
			pct(float64(report.FlaggedWeakFix)/float64(report.Flagged)))
	}
	fmt.Fprintf(w, "    Jev cleared, but a strict rule fires    %d  (genuine misses)\n",
		report.ClearedButDangerous)

	if len(report.ByRule) == 0 {
		return
	}
	names := make([]string, 0, len(report.ByRule))
	for name := range report.ByRule {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if report.ByRule[names[i]] != report.ByRule[names[j]] {
			return report.ByRule[names[i]] > report.ByRule[names[j]]
		}
		return names[i] < names[j]
	})
	fmt.Fprintf(w, "\n    what fired inside those flags\n")
	for _, name := range names {
		fmt.Fprintf(w, "      %-38s %d\n", name, report.ByRule[name])
	}
}

// WriteComparison prints two runs side by side, for the context ablation.
func WriteComparison(w io.Writer, title string, left, right Result, leftName, rightName string) {
	l, r := Summarise(left), Summarise(right)

	fmt.Fprintf(w, "\n%s\n%s\n", strings.ToUpper(title), strings.Repeat("-", 64))
	fmt.Fprintf(w, "  %-22s %14s  %14s  %s\n", "", leftName, rightName, "change")

	rows := []struct {
		name        string
		left, right float64
		asPercent   bool
	}{
		{"accuracy @ 0.50", l.At50.Accuracy(), r.At50.Accuracy(), true},
		{"precision @ 0.50", l.At50.Precision(), r.At50.Precision(), true},
		{"recall @ 0.50", l.At50.Recall(), r.At50.Recall(), true},
		{"F1 @ 0.50", l.At50.F1(), r.At50.F1(), true},
		{"ROC-AUC", l.AUC, r.AUC, false},
		{"ECE", l.ECE, r.ECE, false},
		{"Brier", l.Brier, r.Brier, false},
	}
	for _, row := range rows {
		if row.asPercent {
			fmt.Fprintf(w, "  %-22s %14s  %14s  %+.1f pp\n",
				row.name, pct(row.left), pct(row.right), 100*(row.right-row.left))
			continue
		}
		fmt.Fprintf(w, "  %-22s %14.4f  %14.4f  %+.4f\n",
			row.name, row.left, row.right, row.right-row.left)
	}
}
