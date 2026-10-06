// Package ticket owns the run ticket: what one verification run is about and which brain drives
// it. A field comes from the first level that sets it: the ticket, its named profile, the
// project's defaults, the user's defaults, then verilex's built-ins. verilex only reads these
// files, so runs with different tickets never conflict. It carries and validates the ticket and
// never launches a harness or calls a model; that belongs to a companion launcher.
package ticket

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Efforts are the effort levels a ticket may name; the launcher maps them onto its harness.
var Efforts = []string{"minimal", "low", "medium", "high", "xhigh", "max"}

// brain lists the fields every level may set, in output order.
var brain = []string{"harness", "model", "effort", "token_budget", "time_budget"}

// builtIn is the lowest level. It names no harness or model: verilex stays harness-agnostic.
var builtIn = map[string]any{"effort": "medium"}

var (
	plainName   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+\[\]-]*$`)
	profileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// Ticket is one run's resolved ticket. From names the level that set each resolved field.
type Ticket struct {
	Intent      string            `json:"intent,omitempty"`
	Diff        string            `json:"diff,omitempty"`
	Profile     string            `json:"profile,omitempty"`
	Harness     string            `json:"harness"`
	Model       string            `json:"model"`
	Effort      string            `json:"effort"`
	TokenBudget int               `json:"token_budget,omitempty"`
	TimeBudget  string            `json:"time_budget,omitempty"`
	From        map[string]string `json:"from"`
}

// Sources are the profiles files below the ticket: the project's and the user's. A missing
// file sets nothing.
type Sources struct{ Project, User string }

// ProjectFile is the project's profiles file inside a .verilex directory.
func ProjectFile(verilexDir string) string { return filepath.Join(verilexDir, "profiles.yaml") }

// UserFile is the user's profiles file: $XDG_CONFIG_HOME/verilex/profiles.yaml, by default under ~/.config.
func UserFile() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "verilex", "profiles.yaml")
}

// level is one precedence level: its name, as From reports it, and the fields it sets.
type level struct {
	name   string
	values map[string]any
}

// profiles is one profiles file: its defaults and its named profiles.
type profiles struct {
	defaults map[string]any
	named    map[string]map[string]any
}

// Resolve reads the ticket at path, validates every level, and resolves each field by precedence.
// An error names the file and the field it refuses.
func Resolve(path string, src Sources) (Ticket, error) {
	wrap := func(err error) error { return fmt.Errorf("ticket %s: %w", path, err) }
	doc, err := document(path)
	if err != nil {
		return Ticket{}, err
	}
	fields, err := settings(doc, "", []string{"intent", "diff", "profile"})
	if err != nil {
		return Ticket{}, wrap(err)
	}
	t := Ticket{From: map[string]string{}}
	t.Intent, _ = fields["intent"].(string)
	t.Diff, _ = fields["diff"].(string)
	if t.Intent == "" && t.Diff == "" {
		return Ticket{}, wrap(errors.New("intent or diff: a ticket names at least one"))
	}
	project, err := load(src.Project)
	if err != nil {
		return Ticket{}, err
	}
	user, err := load(src.User)
	if err != nil {
		return Ticket{}, err
	}
	levels := []level{{"ticket", fields}}
	for _, l := range []level{{"ticket", fields}, {"project default", project.defaults}, {"user default", user.defaults}} {
		if name, ok := l.values["profile"].(string); ok {
			t.Profile, t.From["profile"] = name, l.name
			break
		}
	}
	if t.Profile != "" {
		switch {
		case project.named[t.Profile] != nil:
			levels = append(levels, level{"project profile " + t.Profile, project.named[t.Profile]})
		case user.named[t.Profile] != nil:
			levels = append(levels, level{"user profile " + t.Profile, user.named[t.Profile]})
		default:
			return Ticket{}, wrap(fmt.Errorf("profile: no profile named %s in %s or %s", t.Profile, src.Project, src.User))
		}
	}
	levels = append(levels, level{"project default", project.defaults}, level{"user default", user.defaults}, level{"built-in", builtIn})
	for _, field := range brain {
		for _, l := range levels {
			value, ok := l.values[field]
			if !ok {
				continue
			}
			t.From[field] = l.name
			switch field {
			case "harness":
				t.Harness = value.(string)
			case "model":
				t.Model = value.(string)
			case "effort":
				t.Effort = value.(string)
			case "token_budget":
				t.TokenBudget = value.(int)
			case "time_budget":
				t.TimeBudget = value.(string)
			}
			break
		}
	}
	for _, field := range []string{"harness", "model"} {
		if t.From[field] == "" {
			return Ticket{}, wrap(fmt.Errorf("%s: not set by the ticket, a profile, or the project or user defaults", field))
		}
	}
	return t, nil
}

// load reads one profiles file: 'defaults' and 'profiles', each validated field by field.
func load(path string) (profiles, error) {
	result := profiles{defaults: map[string]any{}, named: map[string]map[string]any{}}
	if path == "" {
		return result, nil
	}
	doc, err := document(path)
	if errors.Is(err, fs.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	wrap := func(err error) error { return fmt.Errorf("%s: %w", path, err) }
	for _, key := range keys(doc) {
		switch key {
		case "defaults":
			section, ok := doc[key].(map[string]any)
			if !ok {
				return result, wrap(errors.New("defaults: must be a mapping of fields"))
			}
			if result.defaults, err = settings(section, "defaults.", []string{"profile"}); err != nil {
				return result, wrap(err)
			}
		case "profiles":
			section, ok := doc[key].(map[string]any)
			if !ok {
				return result, wrap(errors.New("profiles: must map each profile name to its fields"))
			}
			for _, name := range keys(section) {
				where := "profiles." + name
				if !profileName.MatchString(name) {
					return result, wrap(fmt.Errorf("%s: a profile name is letters, digits, '.', '_' or '-'", where))
				}
				fields, ok := section[name].(map[string]any)
				if !ok {
					return result, wrap(fmt.Errorf("%s: must be a mapping of fields", where))
				}
				if result.named[name], err = settings(fields, where+".", nil); err != nil {
					return result, wrap(err)
				}
			}
		default:
			return result, wrap(fmt.Errorf("%s: unknown field; a profiles file takes defaults and profiles", key))
		}
	}
	return result, nil
}

// document reads a YAML (or JSON) file whose top level is a mapping.
func document(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var value any
	if err = yaml.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("%s: malformed YAML: %v", path, err)
	}
	doc, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: must be a mapping of fields", path)
	}
	return doc, nil
}

// settings validates the fields one level sets: the brain fields plus the extra ones it allows.
func settings(doc map[string]any, prefix string, extra []string) (map[string]any, error) {
	allowed := append(append([]string{}, extra...), brain...)
	result := map[string]any{}
	for _, key := range keys(doc) {
		if !slices.Contains(allowed, key) {
			return nil, fmt.Errorf("%s%s: unknown field; expected one of %s", prefix, key, strings.Join(allowed, ", "))
		}
		value, err := check(key, doc[key])
		if err != nil {
			return nil, fmt.Errorf("%s%s: %v", prefix, key, err)
		}
		result[key] = value
	}
	return result, nil
}

// check validates one field's value and returns it as Ticket stores it.
func check(field string, value any) (any, error) {
	if field == "token_budget" {
		n, ok := value.(int)
		if !ok || n <= 0 {
			return nil, fmt.Errorf("must be a positive whole number of tokens, not %s", shown(value))
		}
		return n, nil
	}
	s, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("must be a string, not %s", shown(value))
	}
	switch field {
	case "intent":
		if strings.TrimSpace(s) == "" {
			return nil, errors.New("must say what the run is meant to prove")
		}
	case "diff":
		if s == "" || strings.HasPrefix(s, "-") || strings.ContainsFunc(s, func(r rune) bool { return r <= ' ' }) {
			return nil, fmt.Errorf("must be a git revision or range such as main...HEAD, not %s", shown(value))
		}
	case "profile":
		if !profileName.MatchString(s) {
			return nil, fmt.Errorf("a profile name is letters, digits, '.', '_' or '-', not %s", shown(value))
		}
	case "harness", "model":
		if !plainName.MatchString(s) {
			return nil, fmt.Errorf("must be a plain name without spaces, not %s", shown(value))
		}
	case "effort":
		if !slices.Contains(Efforts, s) {
			return nil, fmt.Errorf("%s is not one of %s", shown(value), strings.Join(Efforts, ", "))
		}
	case "time_budget":
		if d, err := time.ParseDuration(s); err != nil || d <= 0 {
			return nil, fmt.Errorf("must be a positive duration such as 30m or 1h30m, not %s", shown(value))
		}
	}
	return s, nil
}

func shown(value any) string {
	if s, ok := value.(string); ok {
		return fmt.Sprintf("%q", s)
	}
	return fmt.Sprintf("%v", value)
}

func keys(m map[string]any) []string {
	result := make([]string, 0, len(m))
	for key := range m {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}
