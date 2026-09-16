// Package audit checks the code corpus's own labels.
//
// The corpus is synthetic: an LLM was asked to write a vulnerable solution and a
// fixed one. Some of those fixes do not fix anything, so a sample labelled secure
// can still be exploitable. That matters, because every such sample is counted as
// a false positive against any model that notices.
//
// The rules here fire only on constructs that stay dangerous whatever surrounds
// them. A hit on a sample labelled secure means the label is wrong, not that the
// model was.
package audit

import (
	"regexp"
	"sort"
)

// Rule is one dangerous construct worth looking for.
type Rule struct {
	// Name is what the construct is called.
	Name string
	// Why explains why it is dangerous regardless of context.
	Why string
	// Pattern matches the construct in source text.
	Pattern *regexp.Regexp
	// Strict marks rules that admit essentially no safe reading. Non strict rules
	// are well known weaknesses that a determined reader could argue about, so
	// they are reported separately.
	Strict bool
}

// Rules is the full set, strictest first.
var Rules = []Rule{
	{
		Name:    "eval of request data",
		Why:     "evaluates caller-supplied text as code, which is remote code execution",
		Pattern: regexp.MustCompile(`(?i)eval\s*\(\s*(?:@?request|params|req\.|input|user|payload|data\[|body)`),
		Strict:  true,
	},
	{
		Name:    "eval of stdin",
		Why:     "evaluates caller-supplied text as code",
		Pattern: regexp.MustCompile(`(?i)eval\s*\(\s*(?:gets|input\s*\(|readline|STDIN|stdin)`),
		Strict:  true,
	},
	{
		Name:    "new Function on input",
		Why:     "new Function compiles a string into code, exactly like eval",
		Pattern: regexp.MustCompile(`new\s+Function\s*\(`),
		Strict:  true,
	},
	{
		Name:    "BinaryFormatter",
		Why:     "BinaryFormatter is unsafe by design and deprecated by Microsoft",
		Pattern: regexp.MustCompile(`(?s)BinaryFormatter\s*\(\s*\).{0,400}?\.Deserialize`),
		Strict:  true,
	},
	{
		Name:    "pickle load",
		Why:     "pickle runs arbitrary code while loading",
		Pattern: regexp.MustCompile(`pickle\.loads?\s*\(`),
		Strict:  true,
	},
	{
		Name:    "yaml.load without SafeLoader",
		Why:     "the default loader constructs arbitrary Python objects",
		Pattern: regexp.MustCompile(`yaml\.load\s*\((?:[^)]*)\)`),
		Strict:  false,
	},
	{
		Name:    "shell command built from input",
		Why:     "a command line assembled from input is command injection",
		Pattern: regexp.MustCompile(`(?is)(?:cmd\.exe|/bin/sh|bash).{0,120}?(?:"\s*\+|\+\s*"|\$\{|#\{|%s)`),
		Strict:  true,
	},
	{
		Name:    "exec with interpolation",
		Why:     "a command assembled from input is command injection",
		Pattern: regexp.MustCompile(`(?i)(?:os\.system|Runtime\.getRuntime\(\)\.exec|subprocess\.\w+)\s*\([^)]*(?:\+|\$\{|#\{|f"|%s|%\()`),
		Strict:  true,
	},
	{
		Name:    "unbounded C string copy",
		Why:     "strcpy, strcat, gets and sprintf do no bounds checking",
		Pattern: regexp.MustCompile(`\b(?:strcpy|strcat|gets|sprintf)\s*\(`),
		Strict:  true,
	},
	{
		Name:    "scanf %s without a width",
		Why:     "%s with no width writes past the end of the buffer",
		Pattern: regexp.MustCompile(`scanf\s*\(\s*"[^"]*%s`),
		Strict:  true,
	},
	{
		Name: "SQL built by concatenation",
		Why:  "query text assembled from input instead of bound parameters",
		// Stops at a statement end or a newline so it cannot reach across into
		// unrelated code. The alternation covers the concatenation spellings that
		// actually appear: "+ in Java and C#, . in PHP, ${} and #{} interpolation,
		// and Python %s and f-strings.
		Pattern: regexp.MustCompile(`(?i)(?:SELECT|INSERT|UPDATE|DELETE)\b[^;\n]{0,160}?` +
			`(?:"\s*\+|\+\s*"|"\s*\.\s*\$|'\s*\.\s*\$|\$\{|#\{|%s"\s*%|f"[^"]*\{)`),
		Strict: true,
	},
	{
		Name:    "innerHTML from input",
		Why:     "writes unescaped input into the DOM",
		Pattern: regexp.MustCompile(`(?i)innerHTML\s*=[^;]*(?:input|param|query|user|data|value|req)`),
		Strict:  true,
	},
	{
		Name:    "document.write from input",
		Why:     "writes unescaped input into the page",
		Pattern: regexp.MustCompile(`(?i)document\.write\s*\([^)]*(?:input|param|query|user|data|req)`),
		Strict:  true,
	},
	{
		Name:    "PHP echo of a superglobal",
		Why:     "reflects request data into HTML with no escaping",
		Pattern: regexp.MustCompile(`(?i)echo\s+[^;]{0,60}\$_(?:GET|POST|REQUEST|COOKIE)`),
		Strict:  true,
	},
	{
		Name:    "regex HTML sanitisation",
		Why:     "stripping tags with a regex is defeated by nesting, such as <<script>script>",
		Pattern: regexp.MustCompile(`(?i)replace\s*\(\s*/<[^/]*/[gim]*\s*,|strip_tags\s*\(`),
		Strict:  false,
	},
	{
		Name:    "escape then interpolate SQL",
		Why:     "real_escape_string interpolated into a query is bypassable on a multibyte charset",
		Pattern: regexp.MustCompile(`(?i)real_escape_string|addslashes|mysql_escape`),
		Strict:  false,
	},
	{
		Name:    "deprecated PHP sanitiser",
		Why:     "FILTER_SANITIZE_STRING was deprecated in PHP 8.1 and never stopped injection",
		Pattern: regexp.MustCompile(`FILTER_SANITIZE_STRING`),
		Strict:  false,
	},
}

// Finding is one rule firing on one sample.
type Finding struct {
	Rule string `json:"rule"`
	Why  string `json:"why"`
	// Strict is whether the rule admits essentially no safe reading.
	Strict bool `json:"strict"`
}

// Scan reports every rule that fires on a snippet, strict findings first.
func Scan(code string) []Finding {
	var found []Finding
	for _, rule := range Rules {
		if rule.Pattern.MatchString(code) {
			found = append(found, Finding{Rule: rule.Name, Why: rule.Why, Strict: rule.Strict})
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Strict && !found[j].Strict })
	return found
}

// HasStrict reports whether any rule that admits no safe reading fired.
func HasStrict(code string) bool {
	for _, rule := range Rules {
		if rule.Strict && rule.Pattern.MatchString(code) {
			return true
		}
	}
	return false
}

// Any reports whether any rule fired.
func Any(code string) bool { return len(Scan(code)) > 0 }
