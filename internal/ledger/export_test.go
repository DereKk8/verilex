package ledger

import "testing"

// SetAfterBacked runs f at add's test seam (see afterBacked) until the test ends.
func SetAfterBacked(t testing.TB, f func(evidence string)) {
	afterBacked = f
	t.Cleanup(func() { afterBacked = nil })
}
