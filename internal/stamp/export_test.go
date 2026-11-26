package stamp

import "testing"

// SetAfterStamp runs f at Chain's test seam (see afterStamp) until the test ends.
func SetAfterStamp(t testing.TB, f func(word string)) {
	afterStamp = f
	t.Cleanup(func() { afterStamp = nil })
}
