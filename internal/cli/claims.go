package cli

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/featuremap"
	"github.com/DereKk8/verilex/internal/lifecycle"
)

// claimView is one claim as `verilex claims --json` prints it.
type claimView struct {
	Claim         string              `json:"claim"`
	Version       string              `json:"version"`
	Fingerprint   string              `json:"fingerprint"`
	Sentence      string              `json:"sentence"`
	Args          []string            `json:"args"`
	Entry         []string            `json:"entry"`
	Preconditions []string            `json:"preconditions"`
	Requires      []string            `json:"requires"`
	Provides      []string            `json:"provides"`
	Evidence      dictionary.Evidence `json:"evidence"`
	Sources       []featuremap.Anchor `json:"sources"`
	// Review lists why the claim needs review; empty while the verify skill still holds it.
	Review []string `json:"review,omitempty"`
	// Words prove the current version; Stale maps each word that pins another version to its pin.
	Words []string          `json:"words"`
	Stale map[string]string `json:"stale,omitempty"`
}

// claims prints every claim with its current version, the words that prove it and, only when
// the verify skill no longer holds it, why it needs review.
func claims(project dictionary.Project, asJSON bool, out io.Writer, refuse func(error) int) int {
	all, words, err := lifecycle.Load(project)
	if err != nil {
		return refuse(err)
	}
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	slices.Sort(names)
	views := []claimView{}
	for _, name := range names {
		c := all[name]
		anchors, review, err := lifecycle.Review(project, c)
		if err != nil {
			return refuse(err)
		}
		v := claimView{Claim: c.Name, Version: c.Version(), Fingerprint: c.Fingerprint, Sentence: c.Sentence, Args: c.Args, Entry: c.Entry, Preconditions: c.Preconditions,
			Requires: c.Requires, Provides: c.Provides, Evidence: c.Evidence, Sources: anchors, Review: review, Words: []string{}, Stale: map[string]string{}}
		for _, w := range words {
			switch {
			case w.Claim == nil || w.Claim.Name != c.Name:
			case w.Stale != "":
				v.Stale[w.Name] = w.Proves()
			default:
				v.Words = append(v.Words, w.Name)
			}
		}
		views = append(views, v)
	}
	if asJSON {
		if err = encode(out, views); err != nil {
			return refuse(err)
		}
		return 0
	}
	for _, v := range views {
		proven := slices.Clone(v.Words)
		for _, name := range slices.Sorted(maps.Keys(v.Stale)) {
			proven = append(proven, "stale: "+name+" pins "+v.Stale[name])
		}
		fmt.Fprintf(out, "%s@%s  %s\n  entry: %s  words: %s\n", v.Claim, v.Version, v.Sentence, strings.Join(v.Entry, ", "), states(proven))
		for _, why := range v.Review {
			fmt.Fprintf(out, "  review: %s\n", why)
		}
	}
	return 0
}
