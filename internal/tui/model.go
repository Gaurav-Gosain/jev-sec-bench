package tui

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	jev "github.com/Gaurav-Gosain/jev-go"
	"github.com/Gaurav-Gosain/jev-sec-bench/internal/audit"
	"github.com/Gaurav-Gosain/jev-sec-bench/internal/bench"
	"github.com/Gaurav-Gosain/jev-sec-bench/internal/metrics"
)

// Tab is one screen of the dashboard.
type Tab int

// The tabs, in the order they appear in the header.
const (
	TabOverview Tab = iota
	TabInjection
	TabCode
	TabLive
)

func (t Tab) String() string {
	return [...]string{"overview", "injection", "code", "live"}[t]
}

// tabs is every tab, for cycling.
var tabs = []Tab{TabOverview, TabInjection, TabCode, TabLive}

// Results holds everything loaded from disk.
type Results struct {
	Injection   bench.Result
	NoContext   bench.Result
	Code        bench.Result
	Audit       bench.AuditReport
	HasAblation bool
}

// Load reads the committed result files from dir.
func Load(dir string) (Results, error) {
	var out Results
	read := func(name string, into *bench.Result) error {
		raw, err := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- operator supplied results dir
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, into)
	}
	if err := read("injection.json", &out.Injection); err != nil {
		return out, err
	}
	if err := read("code.json", &out.Code); err != nil {
		return out, err
	}
	if err := read("injection_no_context.json", &out.NoContext); err == nil {
		out.HasAblation = true
	}
	out.Audit = bench.RunAudit(out.Code)
	return out, nil
}

// liveSample is one row in the live feed.
type liveSample struct {
	Kind        string // "injection" or "code"
	Text        string
	Language    string // set for code samples, so live matches the benchmark
	Label       int
	Probability float64
	Severity    float64
	Latency     time.Duration
	Err         error
}

// Correct reports whether a 0.50 cut put this sample on the right side.
func (s liveSample) Correct() bool {
	return (s.Probability >= 0.5) == (s.Label == 1)
}

// Model is the dashboard state.
type Model struct {
	results Results
	tab     Tab
	width   int
	height  int

	// live mode
	client  *jev.Client
	live    bool
	feed    []liveSample
	liveErr error
	pool    []liveSample // samples waiting to be sent, drawn from the results
	cursor  int
	frame   int
}

// New builds the dashboard. client may be nil, which disables live mode.
func New(results Results, client *jev.Client) Model {
	m := Model{results: results, client: client}
	m.buildPool()
	return m
}

// buildPool collects a shuffled mix of samples for the live feed to replay
// against the real API.
func (m *Model) buildPool() {
	var pool []liveSample
	for _, s := range m.results.Injection.Samples {
		pool = append(pool, liveSample{Kind: "injection", Text: s.Text, Label: s.Label})
	}
	for _, s := range m.results.Code.Samples {
		pool = append(pool, liveSample{
			Kind: "code", Text: s.Code, Language: s.Language, Label: s.Label,
		})
	}
	rng := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 7)) // #nosec G404 -- display order
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	m.pool = pool
}

// Msgs.
type (
	tickMsg    time.Time
	liveMsg    liveSample
	liveErrMsg struct{ err error }
)

func tick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return tick() }

// askNext sends the next pooled sample to Jev.
func (m Model) askNext() tea.Cmd {
	if m.client == nil || len(m.pool) == 0 {
		return nil
	}
	sample := m.pool[m.cursor%len(m.pool)]

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		var state any
		var battery jev.Questions
		var noulID string
		if sample.Kind == "injection" {
			state = map[string]string{
				"assistant":    bench.AssistantContext,
				"user_message": sample.Text,
			}
			battery, noulID = bench.InjectionBattery(), bench.QInjection
		} else {
			state = map[string]string{"language": sample.Language, "code": sample.Text}
			battery, noulID = bench.CodeBattery(), bench.QVulnerable
		}

		resp, err := m.client.Ask(ctx, state, battery)
		if err != nil {
			return liveErrMsg{err: err}
		}
		p, err := resp.Answers.Noul(noulID)
		if err != nil {
			return liveErrMsg{err: err}
		}
		sev, _ := resp.Answers.Score(bench.QSeverity)

		sample.Probability = p
		sample.Severity = sev.Value
		sample.Latency = resp.Latency
		return liveMsg(sample)
	}
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tickMsg:
		m.frame++
		return m, tick()

	case liveMsg:
		m.cursor++
		m.feed = append([]liveSample{liveSample(msg)}, m.feed...)
		if len(m.feed) > 24 {
			m.feed = m.feed[:24]
		}
		if m.live {
			return m, m.askNext()
		}
		return m, nil

	case liveErrMsg:
		m.liveErr = msg.err
		m.live = false
		return m, nil
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c", "esc":
		return m, tea.Quit

	case "tab", "right", "l":
		m.tab = tabs[(int(m.tab)+1)%len(tabs)]
		return m, nil

	case "shift+tab", "left", "h":
		m.tab = tabs[(int(m.tab)-1+len(tabs))%len(tabs)]
		return m, nil

	case "1", "2", "3", "4":
		idx := int(msg.String()[0] - '1')
		if idx < len(tabs) {
			m.tab = tabs[idx]
		}
		return m, nil

	case " ", "enter":
		// Space starts and stops the live feed.
		if m.client == nil {
			m.liveErr = errNoClient
			return m, nil
		}
		m.tab = TabLive
		m.live = !m.live
		if m.live {
			m.liveErr = nil
			return m, m.askNext()
		}
		return m, nil
	}
	return m, nil
}

// errNoClient is shown when live mode is requested without an API key.
var errNoClient = &noClientError{}

type noClientError struct{}

func (*noClientError) Error() string {
	return "live mode needs TYPESAFE_API_KEY in the environment"
}

// summary is a small helper so the view does not recompute metrics per frame.
func summarise(r bench.Result) bench.Summary { return bench.Summarise(r) }

// histogramBuckets counts probabilities into buckets, split by label.
func histogramBuckets(r bench.Result, buckets int) (positive, negative []int) {
	positive = make([]int, buckets)
	negative = make([]int, buckets)
	for _, s := range r.Samples {
		idx := int(clamp(s.Probability, 0, 0.999) * float64(buckets))
		if s.Label == 1 {
			positive[idx]++
			continue
		}
		negative[idx]++
	}
	return positive, negative
}

// calibrationSeries pulls the reliability points out of a result.
func calibrationSeries(r bench.Result) (claimed, actual []float64, counts []int) {
	for _, b := range metrics.Calibrate(r.Probabilities(), r.Labels(), 10) {
		claimed = append(claimed, b.MeanProb)
		actual = append(actual, b.ActualRate)
		counts = append(counts, b.Count)
	}
	return claimed, actual, counts
}

// mislabelled finds samples the corpus calls secure that still trip a strict
// audit rule, sorted by how confident Jev was. These are the headline examples.
func mislabelled(r bench.Result, limit int) []bench.Scored {
	var out []bench.Scored
	for _, s := range r.Samples {
		if s.Label == 0 && s.Probability >= 0.5 && audit.HasStrict(s.Code) {
			out = append(out, s)
		}
	}
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j].Probability > out[i].Probability {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	// One example per rule, so the panel shows four different ways a "fixed"
	// sample can still be broken rather than the same one four times.
	seen := map[string]bool{}
	var unique []bench.Scored
	for _, s := range out {
		key := strictRule(s.Code)
		if seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, s)
		if len(unique) == limit {
			break
		}
	}
	return unique
}

// strictRule names the first rule admitting no safe reading that fired.
func strictRule(code string) string {
	for _, f := range audit.Scan(code) {
		if f.Strict {
			return f.Rule
		}
	}
	return ""
}

// firstLine trims a snippet down to something that fits one row.
func firstLine(code string, width int) string {
	for line := range strings.SplitSeq(code, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "```") {
			continue
		}
		return truncate(trimmed, width)
	}
	return ""
}

// declaration reports whether a line only names a function rather than doing
// anything, so the panel can skip past "def safe_eval(user_input):" to the call
// that is actually dangerous.
func declaration(line string) bool {
	for _, prefix := range []string{"def ", "func ", "function ", "public ", "private ", "sub "} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// interesting picks the line of a snippet that actually trips an audit rule, so
// the panel shows the bug rather than an import statement. Declarations are a
// last resort, because the rule often matches the parameter name rather than a
// dangerous call.
func interesting(code string, width int) string {
	var fallback string
	for line := range strings.SplitSeq(code, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || !audit.Any(trimmed) {
			continue
		}
		if declaration(trimmed) {
			if fallback == "" {
				fallback = trimmed
			}
			continue
		}
		return truncate(trimmed, width)
	}
	if fallback != "" {
		return truncate(fallback, width)
	}
	// Some rules span several lines, so no single line trips them on its own.
	// Fall back to the line naming the construct the rule is about.
	if token := ruleToken(strictRule(code)); token != "" {
		for line := range strings.SplitSeq(code, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.Contains(strings.ToLower(trimmed), token) {
				return truncate(trimmed, width)
			}
		}
	}
	return firstLine(code, width)
}

// ruleToken is the most specific word in a rule name, used to find the line a
// multi line rule is talking about.
func ruleToken(rule string) string {
	var longest string
	for _, word := range strings.Fields(rule) {
		if len(word) > len(longest) {
			longest = word
		}
	}
	if len(longest) < 6 {
		return ""
	}
	return strings.ToLower(longest)
}

// ParseTab maps a tab name to its Tab, reporting whether it was recognised.
func ParseTab(name string) (Tab, bool) {
	for _, t := range tabs {
		if t.String() == name {
			return t, true
		}
	}
	return TabOverview, false
}

// WithTab returns the model opened on a given tab.
func (m Model) WithTab(t Tab) Model {
	m.tab = t
	return m
}

// Render draws one frame at a fixed size and returns it, with no terminal and
// no event loop. This is how the screenshots in the README are produced, so the
// picture and the committed numbers can never drift apart.
func (m Model) Render(width, height int, t Tab) string {
	m.width, m.height = width, height
	m.tab = t
	return m.View().Content
}

// Prefill runs n samples through the real API and fills the feed, without an
// event loop. It backs the live screenshot, and it is the only place the live
// request path runs outside the running program, so a broken battery shows up
// here rather than only on screen.
func (m Model) Prefill(ctx context.Context, n int) (Model, error) {
	if m.client == nil {
		return m, errNoClient
	}
	for range n {
		msg := m.askNext()()
		switch v := msg.(type) {
		case liveErrMsg:
			return m, v.err
		case liveMsg:
			m.cursor++
			m.feed = append([]liveSample{liveSample(v)}, m.feed...)
		}
	}
	m.live = true
	return m, nil
}
