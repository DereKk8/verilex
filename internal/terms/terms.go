// Package terms owns the plain-text matching that onboarding and the index share: significant
// terms, literal values and how alike two texts are. It is deterministic and calls no model.
package terms

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
)

var (
	codeSpan = regexp.MustCompile("`([^`\n]*)`")
	// placeholder is a stand-in for a value: an upper-case word such as NAME, or {name}.
	placeholder = regexp.MustCompile(`\{[\pL\pN_]+\}|\b\p{Lu}[\p{Lu}\pN_]+\b`)
	number      = regexp.MustCompile(`^\d+(?:\.\d+)?$`)
)

// stop words carry no meaning of their own in a claim or an intent.
var stop = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a an the and or but of to in on at by for from with as into onto is are was were be been being
		it its this that these those there here then than so not no does do did can could should would will shall may must
		now still once when while after before again also only just very all any each every some such own same other
		prove proves proven verify verifies check checks confirm confirms ensure ensures make makes sure whether
		i we you he she they me us them my our your his her their`) {
		stop[w] = true
	}
}

// Of lists the significant terms of text, stemmed and sorted, each once: placeholders, stop
// words and one-letter tokens drop out, so wording and inflection matter less than meaning.
func Of(text string) []string {
	text = placeholder.ReplaceAllString(text, " ")
	result := []string{}
	for _, token := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '.' && r != '/' && r != '_'
	}) {
		token = strings.Trim(token, "._/")
		if len([]rune(token)) < 2 || stop[token] {
			continue
		}
		if token = stem(token); !slices.Contains(result, token) {
			result = append(result, token)
		}
	}
	slices.Sort(result)
	return result
}

// stem strips one common English suffix, then a trailing e, so add, adds and added, or store,
// stores and stored, meet.
func stem(word string) string {
	if strings.ContainsAny(word, "./_") || number.MatchString(word) {
		return word
	}
	for _, suffix := range []string{"ing", "ed", "es", "s"} {
		if base, ok := strings.CutSuffix(word, suffix); ok && len([]rune(base)) >= 3 && !strings.HasSuffix(word, "ss") {
			word = base
			break
		}
	}
	if base, ok := strings.CutSuffix(word, "e"); ok && len([]rune(base)) >= 3 {
		word = base
	}
	if r := []rune(word); len(r) >= 4 && r[len(r)-1] == r[len(r)-2] && !strings.ContainsRune("aeiousl", r[len(r)-1]) {
		word = string(r[:len(r)-1])
	}
	return word
}

// Literals lists the literal values a text names, normalized and sorted, each once: the contents
// of code spans, numbers, and file-like words such as store.json. A placeholder inside a literal
// becomes `_`, so `added NAME` and `added ITEM` are one literal.
func Literals(text string) []string {
	result := []string{}
	add := func(literal string) {
		if literal = Normalize(literal); literal != "" && !slices.Contains(result, literal) {
			result = append(result, literal)
		}
	}
	for _, m := range codeSpan.FindAllStringSubmatch(text, -1) {
		add(m[1])
	}
	for _, token := range strings.Fields(codeSpan.ReplaceAllString(text, " ")) {
		token = strings.Trim(token, ".,;:!?()[]\"'")
		if number.MatchString(token) || (strings.ContainsAny(token, "./") && strings.IndexFunc(token, unicode.IsLetter) >= 0) {
			add(token)
		}
	}
	slices.Sort(result)
	return result
}

// Normalize lower-cases text, turns every placeholder into `_`, drops backticks and collapses
// whitespace: the form literals are compared in.
func Normalize(text string) string {
	text = strings.ReplaceAll(placeholder.ReplaceAllString(text, "_"), "`", "")
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

// Shows reports whether text names literal: a number must appear as a whole word, any other
// literal as a substring of the normalized text, with `_` standing for any one word.
func Shows(text, literal string) bool {
	normal := Normalize(text)
	if number.MatchString(literal) {
		return slices.ContainsFunc(strings.FieldsFunc(normal, func(r rune) bool { return !unicode.IsNumber(r) && r != '.' }), func(word string) bool {
			return strings.Trim(word, ".") == literal
		})
	}
	parts := strings.Split(literal, "_")
	for i := range parts {
		parts[i] = regexp.QuoteMeta(parts[i])
	}
	return regexp.MustCompile(strings.Join(parts, `\S+`)).MatchString(normal)
}

// Jaccard is how alike two term sets are: shared terms over all terms, 1 for two empty sets.
func Jaccard(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1
	}
	shared := 0
	for _, term := range a {
		if slices.Contains(b, term) {
			shared++
		}
	}
	return float64(shared) / float64(len(a)+len(b)-shared)
}

// Shared lists the terms a and b have in common.
func Shared(a, b []string) []string {
	result := []string{}
	for _, term := range a {
		if slices.Contains(b, term) {
			result = append(result, term)
		}
	}
	return result
}
