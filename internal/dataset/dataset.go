// Package dataset loads the two public corpora the benchmark runs on.
//
// Both come from the Hugging Face datasets server, so a run needs no local
// corpus and anyone can reproduce it from a clean checkout. Raw pulls are cached
// on disk, which also pins the exact rows a published result was measured on.
package dataset

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"
)

const (
	rowsAPI  = "https://datasets-server.huggingface.co/rows"
	pageSize = 100

	// InjectionDataset holds user messages sent to a news publisher's assistant,
	// labelled by whether they try to subvert it.
	InjectionDataset = "deepset/prompt-injections"
	// CodeDataset holds pairs of solutions to the same task, one secure and one
	// vulnerable.
	CodeDataset = "CyberNative/Code_Vulnerability_Security_DPO"
)

// InjectionSample is one user message with the corpus label.
type InjectionSample struct {
	Text string `json:"text"`
	// IsInjection is the dataset's own judgment, used as ground truth.
	IsInjection bool   `json:"is_injection"`
	Split       string `json:"split"`
}

// CodePair is two solutions to the same task in the same language, differing in
// whether they carry a vulnerability.
type CodePair struct {
	Language   string `json:"language"`
	Class      string `json:"class"`
	Secure     string `json:"secure"`
	Vulnerable string `json:"vulnerable"`
}

// CodeSample is one half of a pair, carrying nothing that says which half it is.
type CodeSample struct {
	PairID       int    `json:"pair_id"`
	Language     string `json:"language"`
	Class        string `json:"class"`
	IsVulnerable bool   `json:"is_vulnerable"`
	Code         string `json:"code"`
}

// Fetcher pulls dataset rows, caching raw responses under Dir.
type Fetcher struct {
	Dir    string
	Client *http.Client
}

// NewFetcher returns a fetcher caching into dir.
func NewFetcher(dir string) *Fetcher {
	return &Fetcher{Dir: dir, Client: &http.Client{Timeout: 60 * time.Second}}
}

func (f *Fetcher) rows(dataset, split string, offset, length int) ([]map[string]any, error) {
	query := url.Values{
		"dataset": {dataset},
		"config":  {"default"},
		"split":   {split},
		"offset":  {strconv.Itoa(offset)},
		"length":  {strconv.Itoa(length)},
	}
	resp, err := f.Client.Get(rowsAPI + "?" + query.Encode())
	if err != nil {
		return nil, fmt.Errorf("fetching %s rows: %w", dataset, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("fetching %s rows: status %d: %s", dataset, resp.StatusCode, body)
	}

	var payload struct {
		Rows []struct {
			Row map[string]any `json:"row"`
		} `json:"rows"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decoding %s rows: %w", dataset, err)
	}

	out := make([]map[string]any, len(payload.Rows))
	for i, r := range payload.Rows {
		out[i] = r.Row
	}
	return out, nil
}

// cached reads name from the cache directory, or calls produce and stores it.
func (f *Fetcher) cached(name string, produce func() ([]map[string]any, error)) ([]map[string]any, error) {
	path := filepath.Join(f.Dir, name)
	if raw, err := os.ReadFile(path); err == nil { // #nosec G304 -- path built from a constant name
		var rows []map[string]any
		if err := json.Unmarshal(raw, &rows); err == nil {
			return rows, nil
		}
	}

	rows, err := produce()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(f.Dir, 0o750); err != nil {
		return nil, fmt.Errorf("creating cache dir: %w", err)
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return nil, fmt.Errorf("encoding cache: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return nil, fmt.Errorf("writing cache: %w", err)
	}
	return rows, nil
}

// Injections loads the whole prompt injection corpus, both splits, 662 messages.
func (f *Fetcher) Injections() ([]InjectionSample, error) {
	rows, err := f.cached("prompt_injections.json", func() ([]map[string]any, error) {
		var all []map[string]any
		for _, split := range []struct {
			name  string
			total int
		}{{"train", 546}, {"test", 116}} {
			for offset := 0; offset < split.total; offset += pageSize {
				page, err := f.rows(InjectionDataset, split.name, offset, min(pageSize, split.total-offset))
				if err != nil {
					return nil, err
				}
				for _, row := range page {
					row["split"] = split.name
					all = append(all, row)
				}
			}
		}
		return all, nil
	})
	if err != nil {
		return nil, err
	}

	samples := make([]InjectionSample, 0, len(rows))
	for _, row := range rows {
		text, _ := row["text"].(string)
		label, _ := row["label"].(float64)
		split, _ := row["split"].(string)
		samples = append(samples, InjectionSample{
			Text:        text,
			IsInjection: label == 1,
			Split:       split,
		})
	}
	return samples, nil
}

// vulnerabilityClasses maps a class name to a pattern matching the corpus's free
// text description of it.
//
// Only weaknesses with a clear, agreed meaning are listed. The corpus also
// contains vague entries such as "improper null safety", where the two halves
// differ in style rather than in security. Those are dropped, so the benchmark
// measures vulnerability detection instead of tolerance for label noise.
var vulnerabilityClasses = map[string]*regexp.Regexp{
	"SQL injection":         regexp.MustCompile(`(?i)sql inject`),
	"Command injection":     regexp.MustCompile(`(?i)command inject|shell inject|os command|arbitrary command`),
	"XSS":                   regexp.MustCompile(`(?i)cross-site scripting|xss`),
	"Deserialization":       regexp.MustCompile(`(?i)deserializ|pickle|unmarshal`),
	"Code injection":        regexp.MustCompile(`(?i)\beval\(|code injection|arbitrary code execution|remote code execution`),
	"Buffer overflow":       regexp.MustCompile(`(?i)buffer overflow`),
	"Path traversal":        regexp.MustCompile(`(?i)path traversal|directory traversal`),
	"Hardcoded credentials": regexp.MustCompile(`(?i)hardcoded|hard-coded`),
	"SSRF":                  regexp.MustCompile(`(?i)ssrf|server-side request`),
	"XXE":                   regexp.MustCompile(`(?i)xxe|xml external entity`),
}

// ClassifyVulnerability maps a description onto one class name, or "" when it is
// ambiguous. A description matching two classes is dropped rather than guessed at.
func ClassifyVulnerability(description string) string {
	var hits []string
	for name, rx := range vulnerabilityClasses {
		if rx.MatchString(description) {
			hits = append(hits, name)
		}
	}
	if len(hits) != 1 {
		return ""
	}
	return hits[0]
}

// CodePairs loads matched solutions, keeping only recognised vulnerability classes.
func (f *Fetcher) CodePairs(sampleRows, seed int) ([]CodePair, error) {
	rows, err := f.cached("code_vulnerability.json", func() ([]map[string]any, error) {
		// Spread the sample across the corpus rather than taking a prefix, so one
		// contiguous run of similar rows cannot dominate.
		rng := rand.New(rand.NewPCG(uint64(seed), 0)) // #nosec G404 -- sampling, not a secret
		offsets := rng.Perm(46)[:sampleRows/pageSize]
		sort.Ints(offsets)

		var all []map[string]any
		for _, o := range offsets {
			page, err := f.rows(CodeDataset, "train", o*pageSize, pageSize)
			if err != nil {
				return nil, err
			}
			all = append(all, page...)
		}
		return all, nil
	})
	if err != nil {
		return nil, err
	}

	var pairs []CodePair
	for _, row := range rows {
		description, _ := row["vulnerability"].(string)
		class := ClassifyVulnerability(description)
		if class == "" {
			continue
		}
		language, _ := row["lang"].(string)
		secure, _ := row["chosen"].(string)
		vulnerable, _ := row["rejected"].(string)
		pairs = append(pairs, CodePair{
			Language:   language,
			Class:      class,
			Secure:     secure,
			Vulnerable: vulnerable,
		})
	}
	return pairs, nil
}

// Stratify caps each vulnerability class so one common class cannot dominate the
// score, then trims to limit pairs.
func Stratify(pairs []CodePair, limit, seed int) []CodePair {
	rng := rand.New(rand.NewPCG(uint64(seed), 1)) // #nosec G404 -- sampling, not a secret

	shuffled := append([]CodePair(nil), pairs...)
	rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

	byClass := map[string][]CodePair{}
	for _, p := range shuffled {
		byClass[p.Class] = append(byClass[p.Class], p)
	}

	cap := max(8, limit/max(1, len(byClass))*2)

	// Iterate class names in sorted order so the selection is reproducible.
	names := make([]string, 0, len(byClass))
	for name := range byClass {
		names = append(names, name)
	}
	sort.Strings(names)

	var selected []CodePair
	for _, name := range names {
		group := byClass[name]
		selected = append(selected, group[:min(cap, len(group))]...)
	}
	rng.Shuffle(len(selected), func(i, j int) { selected[i], selected[j] = selected[j], selected[i] })
	return selected[:min(limit, len(selected))]
}

// Explode splits every pair into two blind samples and shuffles them together,
// so the model cannot tell which half of a pair it is looking at, or that a pair
// exists at all.
func Explode(pairs []CodePair, seed int) []CodeSample {
	samples := make([]CodeSample, 0, len(pairs)*2)
	for i, pair := range pairs {
		samples = append(samples,
			CodeSample{PairID: i, Language: pair.Language, Class: pair.Class,
				IsVulnerable: true, Code: pair.Vulnerable},
			CodeSample{PairID: i, Language: pair.Language, Class: pair.Class,
				IsVulnerable: false, Code: pair.Secure},
		)
	}
	rng := rand.New(rand.NewPCG(uint64(seed), 2)) // #nosec G404 -- shuffling, not a secret
	rng.Shuffle(len(samples), func(i, j int) { samples[i], samples[j] = samples[j], samples[i] })
	return samples
}
