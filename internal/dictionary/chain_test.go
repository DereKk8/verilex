package dictionary

import (
	"slices"
	"testing"
)

// A step ChainStep writes parses back to the same arguments; one it cannot write it refuses.
func TestChainStepRoundTrips(t *testing.T) {
	word := Word{Name: "item-stored", Args: []string{"name"}}
	for _, arg := range []string{"apple", "big apple", "", " ", "it's", `say "hi"`, `back\slash`, "tab\there", "line\nbreak", "$HOME", "*", "ä ö", "''", `'"'`} {
		step, err := ChainStep(word.Name, []string{arg})
		if err != nil {
			t.Fatalf("%q: %v", arg, err)
		}
		steps, err := ParseChain("store-open | "+step, []Word{{Name: "store-open"}, word})
		if err != nil || !slices.Equal(steps[1].Argv, []string{arg}) {
			t.Fatalf("%q wrote %q, which parses to %v (%v)", arg, step, steps, err)
		}
	}
	if step, err := ChainStep(word.Name, []string{"a|b"}); err == nil || err.Error() != `item-stored argument 'a|b' cannot be written in a chain, which splits steps on '|'` {
		t.Fatalf("a|b wrote %q, %v", step, err)
	}
}
