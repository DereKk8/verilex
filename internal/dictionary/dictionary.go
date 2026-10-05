// Package dictionary owns project discovery, word and claim contracts, and chain planning.
package dictionary

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/dlclark/regexp2"
	"go.yaml.in/yaml/v3"
)

var frameSteps = []string{"launch", "doctor", "cleanup"}

// DefaultSkillDirs are where verify skills live when config.yaml names no 'skills'.
var DefaultSkillDirs = []string{".cursor/skills", ".claude/skills", ".agents/skills"}

// ErrNoProject means no .verilex/config.yaml exists in the start directory or its parents.
var ErrNoProject = errors.New("no .verilex/config.yaml")

// WordName is the shape of a word: letters or digits in any script, then '.', '_' or '-'.
var WordName = regexp.MustCompile(`^[\pL\pN][\pL\pN._-]*$`)

type Project struct {
	Root, Name     string
	SecretPatterns []*regexp2.Regexp
	SkillDirs      []string
}

func (p Project) Dir() string              { return filepath.Join(p.Root, ".verilex") }
func (p Project) Frame(step string) string { return filepath.Join(p.Dir(), "frame", step) }

type Word struct {
	Name, Path, Promise                  string
	Args, Requires, Provides, Implements []string
	Timeout                              int
	// Inputs are the product paths the word reads, relative to the project root. A word whose
	// contract omits 'inputs', or lists none, has an unknown footprint, so its results are never reused.
	Inputs         []string
	InputsDeclared bool
	// Env names the environment variables whose values the word's result depends on.
	Env []string
	// ReadOnly declares that the word only observes the instance and never changes it, so it
	// may run on a kept instance whatever ran there before. A read-only word provides no states.
	ReadOnly bool
	// Claim is the claim the word's contract pins ('claim: <claim>@<version>'), nil for a word
	// that only names feature-map sections in 'implements'. Requires and Provides include the
	// claim's states, Pinned lists the requires the claim pins, and Implements its sources.
	Claim  *Claim
	Pinned []string
	// Entry is the user entry point the word exercises, one of its claim's.
	Entry string
	// Stale says why the pin names an older version of the claim; a chain holding the word is refused.
	Stale string
	pin   string
}

func (w Word) Run() string { return filepath.Join(w.Path, "run") }

// Proves is the claim version the word's contract pins, <claim>@<version>; empty without a
// claim. It differs from the claim's current version only while the word is Stale.
func (w Word) Proves() string { return w.pin }

type Step struct {
	Word                     Word
	Argv, Requires, Provides []string
	// Pinned are the requires the step's claim pins, with arguments bound.
	Pinned []string
}

func (s Step) Label() string { return strings.Join(append([]string{s.Word.Name}, s.Argv...), " ") }

func Executable(path string) bool {
	return syscall.Access(path, 1) == nil
}

func FindProject(start string) (Project, error) {
	for root := start; ; root = filepath.Dir(root) {
		config := filepath.Join(root, ".verilex", "config.yaml")
		if info, err := os.Stat(config); err == nil && !info.IsDir() {
			data, err := readYAML(config)
			if err != nil {
				return Project{}, err
			}
			name, ok := data["project"].(string)
			if !ok || !regexp.MustCompile(`^[A-Za-z0-9._-]+$`).MatchString(name) {
				return Project{}, fmt.Errorf("%s: 'project' must be a plain name", config)
			}
			p := Project{Root: root, Name: name, SkillDirs: DefaultSkillDirs}
			if data["skills"] != nil {
				p.SkillDirs, err = stringsList(data["skills"])
				if err != nil || len(p.SkillDirs) == 0 {
					return Project{}, fmt.Errorf("%s: 'skills' must be a list of directories holding verify skills", config)
				}
			}
			if data["secret_patterns"] != nil {
				patterns, err := stringsList(data["secret_patterns"])
				if err != nil {
					return Project{}, fmt.Errorf("%s: 'secret_patterns' must be a list of regex strings", config)
				}
				for _, pattern := range patterns {
					r, err := CompilePattern(pattern)
					if err != nil {
						return Project{}, fmt.Errorf("%s: invalid regex in 'secret_patterns' %s: %v", config, quote(pattern), err)
					}
					p.SecretPatterns = append(p.SecretPatterns, r)
				}
			}
			for _, step := range frameSteps {
				if !Executable(p.Frame(step)) {
					return Project{}, fmt.Errorf("missing executable frame step %s", p.Frame(step))
				}
			}
			return p, nil
		}
		if filepath.Dir(root) == root {
			break
		}
	}
	return Project{}, fmt.Errorf("%w in %s or its parents", ErrNoProject, start)
}

// CompilePattern retains lookaround and backreferences accepted by the word contract.
func CompilePattern(pattern string) (*regexp2.Regexp, error) {
	pattern = strings.ReplaceAll(pattern, "(?P<", "(?<")
	pattern = regexp.MustCompile(`\(\?P=([A-Za-z_][A-Za-z_0-9]*)\)`).ReplaceAllString(pattern, `\k<$1>`)
	return regexp2.Compile(pattern, 0)
}

func LoadWords(p Project) ([]Word, error) {
	_, words, err := Load(p)
	return words, err
}

// Load reads a project's claims and its words, binds each word to the claim it pins, and fills
// each claim source's Covered from the claims that words prove.
func Load(p Project) (map[string]Claim, []Word, error) {
	paths, err := filepath.Glob(filepath.Join(p.Dir(), "words", "*", "word.md"))
	if err != nil {
		return nil, nil, err
	}
	claims, err := LoadClaims(p)
	if err != nil {
		return nil, nil, err
	}
	words := make([]Word, 0, len(paths))
	for _, path := range paths {
		word, err := loadWord(filepath.Dir(path), claims)
		if err != nil {
			return nil, nil, err
		}
		words = append(words, word)
	}
	cover(claims, words)
	return claims, words, nil
}

func loadWord(path string, claims map[string]Claim) (Word, error) {
	file := filepath.Join(path, "word.md")
	text, err := os.ReadFile(file)
	if err != nil {
		return Word{}, err
	}
	parts := strings.SplitN(string(text), "---", 3)
	if !strings.HasPrefix(string(text), "---") || len(parts) < 3 {
		return Word{}, fmt.Errorf("%s: missing YAML frontmatter", file)
	}
	meta, err := mapping([]byte(parts[1]))
	if err != nil {
		return Word{}, fmt.Errorf("%s: malformed YAML frontmatter: %v", file, err)
	}
	name, ok := meta["word"].(string)
	if !ok || name != filepath.Base(path) {
		return Word{}, fmt.Errorf("%s: 'word' must equal the directory name %s", file, quote(filepath.Base(path)))
	}
	if !Executable(filepath.Join(path, "run")) {
		return Word{}, fmt.Errorf("%s: missing executable 'run'", path)
	}
	w := Word{Name: name, Path: path, Timeout: 1800}
	for _, field := range []struct {
		key  string
		dest *[]string
	}{
		{"implements", &w.Implements}, {"args", &w.Args}, {"requires", &w.Requires}, {"provides", &w.Provides},
		{"inputs", &w.Inputs}, {"env", &w.Env},
	} {
		*field.dest, err = stringsList(meta[field.key])
		if err != nil {
			return Word{}, fmt.Errorf("%s: '%s' must be a list of strings", file, field.key)
		}
	}
	// An empty list covers no product paths, so it proves nothing and counts as undeclared.
	w.InputsDeclared = len(w.Inputs) > 0
	for _, input := range w.Inputs {
		if strings.TrimSpace(input) == "" {
			return Word{}, fmt.Errorf("%s: 'inputs' entries must be paths", file)
		}
	}
	for _, name := range w.Env {
		if !envName.MatchString(name) {
			return Word{}, fmt.Errorf("%s: 'env' entry %s is not a variable name", file, quote(name))
		}
	}
	if raw, ok := meta["read_only"]; ok {
		if w.ReadOnly, ok = raw.(bool); !ok {
			return Word{}, fmt.Errorf("%s: 'read_only' must be true or false", file)
		}
		if w.ReadOnly && len(w.Provides) > 0 {
			return Word{}, fmt.Errorf("%s: a 'read_only' word changes nothing, so it provides no states", file)
		}
	}
	if raw, ok := meta["entry"]; ok {
		if w.Entry, ok = raw.(string); !ok {
			return Word{}, fmt.Errorf("%s: 'entry' must name one entry point", file)
		}
	}
	if raw, ok := meta["claim"]; ok {
		pin, ok := raw.(string)
		if !ok {
			return Word{}, fmt.Errorf("%s: 'claim' must pin a claim version, <claim>@<version> as `verilex claims` prints it", file)
		}
		if err = bindClaim(&w, file, pin, claims); err != nil {
			return Word{}, err
		}
	} else if len(w.Implements) == 0 {
		return Word{}, fmt.Errorf("%s: 'implements' must point at the verify skill's feature map, or 'claim' must pin the claim the word proves", file)
	}
	if !Truthy(meta["promise"]) || strings.TrimSpace(fmt.Sprint(meta["promise"])) == "" {
		return Word{}, fmt.Errorf("%s: 'promise' is required", file)
	}
	w.Promise = strings.Join(strings.Fields(fmt.Sprint(meta["promise"])), " ")
	if err = undeclared(file, append(append([]string{}, w.Requires...), w.Provides...), w.Args); err != nil {
		return Word{}, err
	}
	if raw, ok := meta["timeout"]; ok {
		switch value := raw.(type) {
		case int:
			w.Timeout = value
		case float64:
			w.Timeout = int(value)
		case bool:
			if value {
				w.Timeout = 1
			} else {
				w.Timeout = 0
			}
		case string:
			w.Timeout, err = strconv.Atoi(strings.TrimSpace(value))
		default:
			err = fmt.Errorf("invalid timeout")
		}
		if err != nil || w.Timeout <= 0 {
			return Word{}, fmt.Errorf("%s: 'timeout' must be a positive integer", file)
		}
	}
	return w, nil
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var placeholder = regexp.MustCompile(`\{([\pL\pN_]+)\}`)

func ParseChain(chain string, words []Word) ([]Step, error) {
	steps := []Step{}
	for _, segment := range strings.Split(chain, "|") {
		tokens, err := splitShell(segment)
		if err != nil {
			return nil, fmt.Errorf("invalid word syntax in chain %s: %v", quote(chain), err)
		}
		if len(tokens) == 0 {
			return nil, fmt.Errorf("empty word in chain %s", quote(chain))
		}
		var word *Word
		for i := range words {
			if words[i].Name == tokens[0] {
				word = &words[i]
				break
			}
		}
		if word == nil {
			return nil, fmt.Errorf("unknown word %s; `verilex words` lists the dictionary", quote(tokens[0]))
		}
		if word.Stale != "" {
			return nil, errors.New(word.Stale)
		}
		argv := tokens[1:]
		if len(argv) != len(word.Args) {
			return nil, fmt.Errorf("%s takes %d argument(s) %s, got %s", word.Name, len(word.Args), repr(word.Args), repr(argv))
		}
		bound := map[string]string{}
		for i, arg := range word.Args {
			bound[arg] = argv[i]
		}
		bind := func(states []string) []string {
			result := make([]string, 0, len(states))
			for _, state := range states {
				result = append(result, placeholder.ReplaceAllStringFunc(state, func(m string) string { return bound[m[1:len(m)-1]] }))
			}
			return result
		}
		steps = append(steps, Step{Word: *word, Argv: argv, Requires: bind(word.Requires), Provides: bind(word.Provides), Pinned: bind(word.Pinned)})
	}
	return steps, nil
}

func CheckOrder(steps []Step) error {
	available := map[string]bool{}
	for _, step := range steps {
		for _, state := range step.Requires {
			if !available[state] {
				pinned := ""
				if slices.Contains(step.Pinned, state) {
					pinned = ", pinned by claim " + step.Word.Claim.Name
				}
				return fmt.Errorf("%s requires %s%s; nothing earlier provides it", step.Label(), state, pinned)
			}
		}
		for _, state := range step.Provides {
			available[state] = true
		}
	}
	return nil
}

func stringsList(value any) ([]string, error) {
	result := []string{}
	if value == nil {
		return result, nil
	}
	list, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("expected list")
	}
	for _, v := range list {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("expected string")
		}
		result = append(result, s)
	}
	return result, nil
}

func readYAML(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m, err := mapping(data)
	if err != nil {
		return nil, fmt.Errorf("%s: malformed YAML: %v", path, err)
	}
	return m, nil
}

func mapping(data []byte) (map[string]any, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	legacyYAML(&document)
	var value any
	if err := document.Decode(&value); err != nil {
		return nil, err
	}
	if !Truthy(value) {
		return map[string]any{}, nil
	}
	m, ok := value.(map[string]any)
	if raw, mixed := value.(map[any]any); mixed {
		m = map[string]any{}
		for key, value := range raw {
			if name, ok := key.(string); ok {
				m[name] = value
			}
		}
		ok = true
	}
	if !ok {
		return nil, fmt.Errorf("expected a mapping")
	}
	return m, nil
}

// PyYAML accepts YAML 1.1 booleans and keeps the last duplicate mapping key.
func legacyYAML(node *yaml.Node) {
	if node.Kind == yaml.ScalarNode && node.Style == 0 && node.Tag == "!!str" {
		switch node.Value {
		case "yes", "Yes", "YES", "on", "On", "ON":
			node.Tag = "!!bool"
			node.Value = "true"
		case "no", "No", "NO", "off", "Off", "OFF":
			node.Tag = "!!bool"
			node.Value = "false"
		}
	}
	for _, child := range node.Content {
		legacyYAML(child)
	}
	if node.Kind == yaml.MappingNode {
		last := map[string]int{}
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			last[key.Tag+"\x00"+key.Value] = i
		}
		content := make([]*yaml.Node, 0, len(node.Content))
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if last[key.Tag+"\x00"+key.Value] == i {
				content = append(content, key, node.Content[i+1])
			}
		}
		node.Content = content
	}
}

func Truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case int:
		return x != 0
	case int64:
		return x != 0
	case uint:
		return x != 0
	case uint64:
		return x != 0
	case float64:
		return x != 0
	case float32:
		return x != 0
	case []any:
		return len(x) > 0
	case []string:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	case map[any]any:
		return len(x) > 0
	}
	return true
}

func quote(s string) string {
	q := '\''
	if strings.ContainsRune(s, q) && !strings.ContainsRune(s, '"') {
		q = '"'
	}
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, string(q), "\\"+string(q))
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return string(q) + s + string(q)
}

func repr(values []string) string {
	result := []string{}
	for _, v := range values {
		result = append(result, quote(v))
	}
	return "[" + strings.Join(result, ", ") + "]"
}

// splitShell follows POSIX shlex quoting without expanding variables or commands.
func splitShell(s string) ([]string, error) {
	var result []string
	var token strings.Builder
	quote := rune(0)
	active := false
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		if quote == '\'' {
			if c == quote {
				quote = 0
			} else {
				token.WriteRune(c)
			}
			continue
		}
		if c == '\\' && quote != '\'' {
			if i+1 == len(runes) {
				return nil, fmt.Errorf("No escaped character")
			}
			i++
			next := runes[i]
			if quote == '"' && next != '"' && next != '\\' {
				token.WriteRune('\\')
			}
			token.WriteRune(next)
			active = true
			continue
		}
		if quote == '"' {
			if c == quote {
				quote = 0
			} else {
				token.WriteRune(c)
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			active = true
			continue
		}
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			if active {
				result = append(result, token.String())
				token.Reset()
				active = false
			}
			continue
		}
		token.WriteRune(c)
		active = true
	}
	if quote != 0 {
		return nil, fmt.Errorf("No closing quotation")
	}
	if active {
		result = append(result, token.String())
	}
	return result, nil
}
