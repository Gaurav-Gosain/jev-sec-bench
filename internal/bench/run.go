package bench

import (
	"context"
	"fmt"
	"sort"
	"time"

	jev "github.com/Gaurav-Gosain/jev-go"
	"github.com/Gaurav-Gosain/jev-sec-bench/internal/dataset"
	"github.com/Gaurav-Gosain/jev-sec-bench/internal/metrics"
)

// Scored is one sample after Jev has judged it.
type Scored struct {
	// Label is the ground truth: 1 for a positive (an injection, or vulnerable
	// code), 0 for a negative.
	Label int `json:"label"`
	// Probability is the Noul answer: how likely Jev thinks the positive is.
	Probability float64 `json:"probability"`
	// Severity is the Score answer, on the battery's own 0 to 3 scale.
	Severity float64 `json:"severity"`
	// SeverityConfidence is how concentrated the severity distribution was.
	SeverityConfidence float64 `json:"severity_confidence"`
	// LatencyMS is the round trip time for this sample's request.
	LatencyMS   int64 `json:"latency_ms"`
	InputTokens int   `json:"input_tokens"`

	// Text carries the message for the injection benchmark.
	Text string `json:"text,omitempty"`
	// Code, Language, Class and PairID carry the code benchmark's sample.
	Code     string `json:"code,omitempty"`
	Language string `json:"language,omitempty"`
	Class    string `json:"class,omitempty"`
	PairID   int    `json:"pair_id,omitempty"`
}

// Result is a finished benchmark: every scored sample plus the run's totals.
type Result struct {
	Name    string    `json:"name"`
	Model   string    `json:"model"`
	RunAt   time.Time `json:"run_at"`
	Samples []Scored  `json:"samples"`

	WallSeconds  float64 `json:"wall_seconds"`
	Requests     int     `json:"requests"`
	Failed       int     `json:"failed"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	P50MS        int64   `json:"p50_ms"`
	P95MS        int64   `json:"p95_ms"`
	// Notes records anything about how the run was configured that changes how
	// the numbers should be read.
	Notes string `json:"notes,omitempty"`
}

// Probabilities and Labels return the columns the metrics package works on.
func (r Result) Probabilities() []float64 {
	out := make([]float64, len(r.Samples))
	for i, s := range r.Samples {
		out[i] = s.Probability
	}
	return out
}

// Labels returns the ground truth column.
func (r Result) Labels() []int {
	out := make([]int, len(r.Samples))
	for i, s := range r.Samples {
		out[i] = s.Label
	}
	return out
}

// Positives counts the ground truth positives.
func (r Result) Positives() int {
	var n int
	for _, s := range r.Samples {
		n += s.Label
	}
	return n
}

// Options configures a benchmark run.
type Options struct {
	Concurrency int
	Model       string
	// WithContext controls whether the injection benchmark passes the deployment
	// description as state. Turning it off is the ablation.
	WithContext bool
	Progress    func(done, total int)
}

// RunInjection judges every message in the corpus.
//
// With opts.WithContext the state is a named object holding the assistant's
// description alongside the message. Without it, the state is the bare message
// and the questions are unchanged, which isolates what the context is worth.
func RunInjection(ctx context.Context, client *jev.Client, samples []dataset.InjectionSample, opts Options) (Result, error) {
	battery := InjectionBattery()

	results, stats := jev.Batch(ctx, client, samples,
		func(s dataset.InjectionSample) (any, jev.Questions) {
			if !opts.WithContext {
				return s.Text, battery
			}
			return map[string]string{
				"assistant":    AssistantContext,
				"user_message": s.Text,
			}, battery
		},
		jev.BatchOptions{Concurrency: opts.Concurrency, OnProgress: opts.Progress},
	)

	notes := "deployment context supplied as state"
	if !opts.WithContext {
		notes = "ablation: no deployment context, bare message as state"
	}

	out := newResult("prompt-injection", notes, stats)
	for _, r := range results {
		if !r.OK() {
			continue
		}
		scored, err := score(r.Response, QInjection, r.Item.IsInjection)
		if err != nil {
			return Result{}, err
		}
		scored.Text = r.Item.Text
		out.Model = r.Response.Model
		out.Samples = append(out.Samples, scored)
	}
	return out, nil
}

// RunCode judges every blind code sample.
func RunCode(ctx context.Context, client *jev.Client, samples []dataset.CodeSample, opts Options) (Result, error) {
	battery := CodeBattery()

	results, stats := jev.Batch(ctx, client, samples,
		func(s dataset.CodeSample) (any, jev.Questions) {
			return map[string]string{"language": s.Language, "code": s.Code}, battery
		},
		jev.BatchOptions{Concurrency: opts.Concurrency, OnProgress: opts.Progress},
	)

	out := newResult("vulnerable-code", "blind: no vulnerability class given to the model", stats)
	for _, r := range results {
		if !r.OK() {
			continue
		}
		scored, err := score(r.Response, QVulnerable, r.Item.IsVulnerable)
		if err != nil {
			return Result{}, err
		}
		scored.Code = r.Item.Code
		scored.Language = r.Item.Language
		scored.Class = r.Item.Class
		scored.PairID = r.Item.PairID
		out.Model = r.Response.Model
		out.Samples = append(out.Samples, scored)
	}
	return out, nil
}

func newResult(name, notes string, stats jev.BatchStats) Result {
	return Result{
		Name:         name,
		RunAt:        time.Now().UTC(),
		Notes:        notes,
		WallSeconds:  stats.Wall.Seconds(),
		Requests:     stats.Succeeded,
		Failed:       stats.Failed,
		InputTokens:  stats.InputTokens,
		OutputTokens: stats.OutputTokens,
		P50MS:        stats.Percentile(0.5).Milliseconds(),
		P95MS:        stats.Percentile(0.95).Milliseconds(),
	}
}

// score narrows one response into a Scored row.
func score(resp *jev.Response, noulID string, positive bool) (Scored, error) {
	probability, err := resp.Answers.Noul(noulID)
	if err != nil {
		return Scored{}, fmt.Errorf("reading %q: %w", noulID, err)
	}
	severity, err := resp.Answers.Score(QSeverity)
	if err != nil {
		return Scored{}, fmt.Errorf("reading %q: %w", QSeverity, err)
	}
	label := 0
	if positive {
		label = 1
	}
	return Scored{
		Label:              label,
		Probability:        probability,
		Severity:           severity.Value,
		SeverityConfidence: severity.Confidence,
		LatencyMS:          resp.Latency.Milliseconds(),
		InputTokens:        resp.Usage.InputTokens,
	}, nil
}

// PairOutcome is how one matched pair was ordered.
type PairOutcome struct {
	Class      string
	Language   string
	Vulnerable float64
	Secure     float64
}

// Correct reports whether the vulnerable half outscored its own secure twin.
func (p PairOutcome) Correct() bool { return p.Vulnerable > p.Secure }

// Tied reports whether both halves got the same probability.
func (p PairOutcome) Tied() bool { return p.Vulnerable == p.Secure }

// Pairs rebuilds the matched pairs from a code result.
//
// This is the measure that surface features cannot game: both halves solve the
// same task in the same language and style, so ranking them is a judgment about
// the security difference and nothing else.
func Pairs(r Result) []PairOutcome {
	type halves struct {
		vulnerable, secure float64
		seenV, seenS       bool
		class, language    string
	}
	byPair := map[int]*halves{}
	for _, s := range r.Samples {
		h, ok := byPair[s.PairID]
		if !ok {
			h = &halves{class: s.Class, language: s.Language}
			byPair[s.PairID] = h
		}
		if s.Label == 1 {
			h.vulnerable, h.seenV = s.Probability, true
			continue
		}
		h.secure, h.seenS = s.Probability, true
	}

	ids := make([]int, 0, len(byPair))
	for id := range byPair {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	out := make([]PairOutcome, 0, len(ids))
	for _, id := range ids {
		h := byPair[id]
		if !h.seenV || !h.seenS {
			continue // a half failed, so the pair cannot be ranked
		}
		out = append(out, PairOutcome{
			Class: h.class, Language: h.language,
			Vulnerable: h.vulnerable, Secure: h.secure,
		})
	}
	return out
}

// PairAccuracy is the share of pairs ranked the right way round.
func PairAccuracy(pairs []PairOutcome) float64 {
	if len(pairs) == 0 {
		return 0
	}
	var correct int
	for _, p := range pairs {
		if p.Correct() {
			correct++
		}
	}
	return float64(correct) / float64(len(pairs))
}

// Subset selects the samples matching keep, for a per class or per language view.
func Subset(r Result, keep func(Scored) bool) ([]float64, []int) {
	var probs []float64
	var labels []int
	for _, s := range r.Samples {
		if !keep(s) {
			continue
		}
		probs = append(probs, s.Probability)
		labels = append(labels, s.Label)
	}
	return probs, labels
}

// Summary collects the headline numbers for one result.
type Summary struct {
	Samples   int
	Positives int
	AUC       float64
	At50      metrics.Confusion
	BestF1    float64
	BestAt    float64
	ECE       float64
	Brier     float64
}

// Summarise computes the headline numbers.
func Summarise(r Result) Summary {
	probs, labels := r.Probabilities(), r.Labels()
	threshold, f1 := metrics.BestF1(probs, labels)
	return Summary{
		Samples:   len(probs),
		Positives: r.Positives(),
		AUC:       metrics.ROCAUC(probs, labels),
		At50:      metrics.Confuse(probs, labels, 0.5),
		BestF1:    f1,
		BestAt:    threshold,
		ECE:       metrics.ECE(probs, labels, 10),
		Brier:     metrics.Brier(probs, labels),
	}
}
