// Package judge decides by meaning whether a product's claims prove an intent. A model is the
// judge, and the launcher starts it, never the verilex core. Only a confident "full" lets an
// intent stand for a green: "partial", "none", an unsure "full" and a failed judge never do. A
// judgment is stamped with the intent, the claim texts it relied on and the judge, and the store
// reuses it until one of them drifts.
package judge

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// System is the judge's system prompt. It tells the judge that the request is data, never
// instructions: without that, one injected sentence turned a judged "none" into "full".
//
//go:embed system.txt
var System string

// Schema is the JSON schema of an answer.
//
//go:embed schema.json
var Schema string

// DefaultModel is the model the judge asks when the run spec names none. It judged 108 of 108
// normal cases right, with no false green and no false red, in the experiment that chose it.
const DefaultModel = "claude-opus-5-5"

// MinConfidence is the lowest confidence at which a "full" stands. In the experiment every
// right "full" had 0.55 or more, and the two wrong ones, both from an injected request, had 0.5
// and 0.3. A "full" below it is a guess, and a guess never goes green.
const MinConfidence = 0.55

// format changes whenever the meaning of a judgment or the layout of a request changes, so a
// judgment made before stops being reused.
const format = "verilex-judgment-2"

// The coverage a judge answers.
const (
	// Full: a green on the judged claims proves everything the intent asks.
	Full = "full"
	// Partial: the judged claims prove part of the intent, and no claim proves the rest.
	Partial = "partial"
	// None: no claim proves anything the intent asks.
	None = "none"
)

// Claim is one claim of the catalog as `verilex claims --json` prints it: the text the judge
// reads, and the fingerprint that changes whenever that text does.
type Claim struct {
	Claim       string `json:"claim"`
	Fingerprint string `json:"fingerprint"`
	Sentence    string `json:"sentence"`
	Evidence    struct {
		Action      string `json:"action"`
		Observation string `json:"observation"`
	} `json:"evidence"`
}

// Catalog reads `verilex claims --json`. A claim without a name or a fingerprint is refused: a
// judgment could not name it, or tell when it drifted.
func Catalog(raw []byte) ([]Claim, error) {
	var claims []Claim
	if err := json.Unmarshal(raw, &claims); err != nil {
		return nil, fmt.Errorf("verilex claims JSON: %v", err)
	}
	for _, c := range claims {
		if c.Claim == "" || c.Fingerprint == "" {
			return nil, fmt.Errorf("verilex claims JSON: a claim without a name or a fingerprint")
		}
	}
	return claims, nil
}

// Judge is the command that judges, and what names it in a stamp.
type Judge struct {
	Argv  []string
	Model string
	// driver names the command: the built-in template, or the content of a named executable.
	driver string
}

// builtin is the Claude Code command the experiment measured. It loads no tools, settings, MCP
// servers or session, so nothing but the prompt reaches the model. {system} is the system prompt
// file and {schema} the answer schema; the request comes on stdin.
var builtin = []string{"claude", "-p", "--model", "{model}", "--system-prompt-file", "{system}",
	"--tools", "", "--setting-sources", "", "--strict-mcp-config", "--no-session-persistence",
	"--output-format", "json", "--json-schema", "{schema}"}

// Resolve returns the judge: the executable at path when one is given, else the built-in Claude
// Code judge, which asks a model and so runs only with allowHarness. An empty model is DefaultModel.
func Resolve(path, model string, allowHarness bool) (Judge, error) {
	if model == "" {
		model = DefaultModel
	}
	if path != "" {
		// The judge runs from an empty directory, so the file checked, stamped and run is one
		// absolute path, never a PATH lookup.
		full, err := filepath.Abs(path)
		if err != nil {
			return Judge{}, fmt.Errorf("--judge %s: %v", path, err)
		}
		path = full
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			return Judge{}, fmt.Errorf("--judge %s is not an executable file", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return Judge{}, fmt.Errorf("--judge %s: %v", path, err)
		}
		return Judge{Argv: []string{path}, Model: model, driver: "executable " + digest(string(data))}, nil
	}
	if !allowHarness {
		return Judge{}, errors.New("an intent needs a judge, and the built-in claude judge is not started unless --allow-harness is set; pass --judge to run a named executable instead")
	}
	return Judge{Argv: builtin, Model: model, driver: "builtin " + strings.Join(builtin, "\x00")}, nil
}

// Command fills placeholders only in the built-in template. An explicit judge's argv is literal.
func (j Judge) Command(system string) []string {
	if !strings.HasPrefix(j.driver, "builtin ") {
		return slices.Clone(j.Argv)
	}
	out := make([]string, len(j.Argv))
	for i, arg := range j.Argv {
		out[i] = strings.NewReplacer("{model}", j.Model, "{system}", system, "{schema}", strings.TrimSpace(Schema)).Replace(arg)
	}
	return out
}

// Identity names the judge in a stamp: its command, prompt, schema and model. A change to any of
// them is a new judge, whose judgments are made again.
func (j Judge) Identity() string {
	return digest(format, j.driver, System, Schema, j.Model)
}

// Request is the prompt the judge answers: the catalog, then the intent between request tags.
// The intent is someone else's data, so its angle brackets are escaped and it cannot close the tag.
func Request(intent string, catalog []Claim) string {
	var b strings.Builder
	b.WriteString("Claim catalog:\n")
	for _, c := range catalog {
		fmt.Fprintf(&b, "- id: %s\n  sentence: %s\n  check action: %s\n  check observation: %s\n", c.Claim, c.Sentence, c.Evidence.Action, c.Evidence.Observation)
	}
	escaped := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(Normalize(intent))
	fmt.Fprintf(&b, "\nRequest: <request>%s</request>\n\nJudge the request.\n", escaped)
	return b.String()
}

// Normalize preserves the intent exactly: even whitespace can change the meaning of a literal.
// The judge reads and stamps the same text the caller supplied.
func Normalize(intent string) string { return intent }

// Answer is what the judge decided.
type Answer struct {
	Coverage   string   `json:"coverage"`
	Claims     []string `json:"claims"`
	Confidence float64  `json:"confidence"`
	Reason     string   `json:"reason"`
}

// Parse reads the judge's answer from its stdout: the answer object itself, or the Claude Code
// result that carries it as structured_output. An answer that breaks the schema, names a claim
// outside the catalog or contradicts itself is refused, so the run is inconclusive.
func Parse(stdout []byte, catalog []Claim) (Answer, error) {
	var doc struct {
		Structured json.RawMessage `json:"structured_output"`
		IsError    bool            `json:"is_error"`
		Result     string          `json:"result"`
	}
	if err := json.Unmarshal(stdout, &doc); err != nil {
		return Answer{}, fmt.Errorf("the judge printed no JSON answer: %v", err)
	}
	if doc.IsError {
		return Answer{}, fmt.Errorf("the judge reported an error: %s", oneLine(doc.Result))
	}
	raw := stdout
	if doc.Structured != nil {
		raw = doc.Structured
	}
	if err := validateAnswer(raw); err != nil {
		return Answer{}, err
	}
	var answer Answer
	if err := json.Unmarshal(raw, &answer); err != nil {
		return Answer{}, fmt.Errorf("the judge printed no JSON answer: %v", err)
	}
	return answer, answer.check(func(name string) bool {
		return slices.ContainsFunc(catalog, func(c Claim) bool { return c.Claim == name })
	})
}

func (a Answer) check(known func(string) bool) error {
	switch a.Coverage {
	case Full, Partial:
		if len(a.Claims) == 0 {
			return fmt.Errorf("the judge answered %q and named no claim", a.Coverage)
		}
	case None:
		if len(a.Claims) > 0 {
			return fmt.Errorf("the judge answered %q and named claims %s", None, strings.Join(a.Claims, ", "))
		}
	default:
		return fmt.Errorf("the judge answered coverage %q, not full, partial or none", a.Coverage)
	}
	if a.Confidence < 0 || a.Confidence > 1 {
		return fmt.Errorf("the judge answered confidence %v, outside 0 to 1", a.Confidence)
	}
	for i, name := range a.Claims {
		if !known(name) {
			return fmt.Errorf("the judge named claim %q, which is not in the catalog", name)
		}
		if slices.Contains(a.Claims[:i], name) {
			return fmt.Errorf("the judge named claim %q twice", name)
		}
	}
	return nil
}

// Judgment is one stamped judgment: the answer, the intent and judge it answered for, and the
// fingerprint of each claim whose text it relied on.
type Judgment struct {
	Format string `json:"format"`
	Intent string `json:"intent"`
	Judge  string `json:"judge"`
	Model  string `json:"model"`
	Answer
	// Relied maps each claim the answer relied on to its fingerprint. A "full" relies only on the
	// claims it names: no other claim can make them prove less. "partial" and "none" rely on the
	// whole catalog, because a claim added or changed since may prove what was missing.
	Relied map[string]string `json:"relied"`
	Judged string            `json:"judged"`
	// Reused says the judgment came from the store; it is not stored.
	Reused bool `json:"-"`
}

// New stamps an answer the judge just gave.
func New(intent string, j Judge, a Answer, catalog []Claim, now time.Time) Judgment {
	relied := map[string]string{}
	for _, c := range catalog {
		if a.Coverage != Full || slices.Contains(a.Claims, c.Claim) {
			relied[c.Claim] = c.Fingerprint
		}
	}
	return Judgment{Format: format, Intent: Normalize(intent), Judge: j.Identity(), Model: j.Model, Answer: a, Relied: relied, Judged: now.UTC().Format(time.RFC3339)}
}

// Stands says whether the intent may stand on the judged claims: only a "full" the judge is sure of.
func (j Judgment) Stands() bool { return j.Coverage == Full && j.Confidence >= MinConfidence }

// holds says whether every claim text the judgment relied on is unchanged in catalog.
func (j Judgment) holds(catalog []Claim) bool {
	current := map[string]string{}
	for _, c := range catalog {
		current[c.Claim] = c.Fingerprint
	}
	if j.Coverage != Full && len(j.Relied) != len(current) {
		return false
	}
	for name, fingerprint := range j.Relied {
		if current[name] != fingerprint {
			return false
		}
	}
	return true
}

// Cautious orders judgments from the most cautious: none, then partial, then full, then the
// lowest confidence, then the earliest made. Of several judgments that hold, the first decides.
func Cautious(a, b Judgment) int {
	if a.rank() != b.rank() {
		return a.rank() - b.rank()
	}
	switch {
	case a.Confidence < b.Confidence:
		return -1
	case a.Confidence > b.Confidence:
		return 1
	}
	return strings.Compare(a.Judged, b.Judged)
}

func (j Judgment) rank() int { return slices.Index([]string{None, Partial, Full}, j.Coverage) }

func digest(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		fmt.Fprintf(h, "%d:%s\n", len(part), part)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
