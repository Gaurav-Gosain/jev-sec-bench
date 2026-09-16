package audit_test

import (
	"testing"

	"github.com/Gaurav-Gosain/jev-sec-bench/internal/audit"
)

// These snippets are shortened from samples the corpus labels "secure". Each one
// still carries the weakness the fix was supposed to remove, which is why the
// audit exists.
func TestStrictRulesFireOnMislabelledSamples(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		code string
	}{
		{
			name: "ruby sinatra still evals request body",
			code: "get '/' do\n  result = eval(@request_payload['code'])\n  return result.to_s\nend",
		},
		{
			name: "javascript wraps eval in new Function",
			code: "function processUserInput(userInput) {\n  new Function(userInput)();\n}",
		},
		{
			name: "csharp still uses BinaryFormatter",
			code: "var formatter = new BinaryFormatter();\nvar obj = formatter.Deserialize(stream);",
		},
		{
			name: "python unpickles input",
			code: "data = pickle.loads(request.data)",
		},
		{
			name: "php reflects a superglobal",
			code: "<?php echo \"Hello \" . $_GET['name']; ?>",
		},
		{
			name: "c copies without bounds",
			code: "char buf[16];\nstrcpy(buf, argv[1]);",
		},
		{
			name: "sql built by concatenation",
			code: "String q = \"SELECT * FROM users WHERE id = \" + userId;",
		},
		{
			name: "shell command built from input",
			code: "process.StartInfo.FileName = \"cmd.exe\";\nprocess.StartInfo.Arguments = \"/C \" + command;",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !audit.HasStrict(tc.code) {
				t.Errorf("no strict rule fired on:\n%s", tc.code)
			}
		})
	}
}

func TestStrictRulesStaySilentOnDefendedCode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		code string
	}{
		{
			name: "parameterised query",
			code: "stmt, _ := db.Prepare(\"SELECT * FROM users WHERE id = ?\")\nstmt.Query(userID)",
		},
		{
			name: "bounded copy",
			code: "char buf[16];\nstrncpy(buf, src, sizeof(buf)-1);\nbuf[sizeof(buf)-1] = '\\0';",
		},
		{
			name: "command run without a shell",
			code: "exec.Command(\"ls\", \"-l\", dir).Output()",
		},
		{
			name: "text node instead of innerHTML",
			code: "el.textContent = req.query.name;",
		},
		{
			name: "safe yaml loader",
			code: "config = yaml.safe_load(stream)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if audit.HasStrict(tc.code) {
				t.Errorf("a strict rule fired on defended code:\n%s\nfindings: %+v",
					tc.code, audit.Scan(tc.code))
			}
		})
	}
}

func TestWeakDefencesAreReportedButNotStrict(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		code string
	}{
		{
			name: "regex tag stripping",
			code: "const clean = name.replace(/<[^>]*>?/gm, '');",
		},
		{
			name: "escape then interpolate",
			code: "$u = $conn->real_escape_string($_GET['u']);",
		},
		{
			name: "deprecated php sanitiser",
			code: "$in = filter_var($in, FILTER_SANITIZE_STRING);",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			findings := audit.Scan(tc.code)
			if len(findings) == 0 {
				t.Fatalf("nothing fired on a known weak defence:\n%s", tc.code)
			}
			for _, f := range findings {
				if f.Strict {
					t.Errorf("%q should be reported as a weak defence, not a strict finding", f.Rule)
				}
			}
		})
	}
}

func TestScanPutsStrictFindingsFirst(t *testing.T) {
	t.Parallel()
	// Both a weak defence and an outright hole in one snippet.
	code := "$u = $conn->real_escape_string($_GET['u']);\n" +
		"$sql = \"SELECT * FROM users WHERE name = '\" . $u . \"'\";"

	findings := audit.Scan(code)
	if len(findings) < 2 {
		t.Fatalf("findings = %+v; want both a strict and a weak one", findings)
	}
	if !findings[0].Strict {
		t.Errorf("findings[0] = %+v; want the strict finding first", findings[0])
	}
}

func TestCleanCodeProducesNothing(t *testing.T) {
	t.Parallel()
	code := "func add(a, b int) int {\n\treturn a + b\n}"
	if audit.Any(code) {
		t.Errorf("findings on harmless code: %+v", audit.Scan(code))
	}
}
