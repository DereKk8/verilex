package curation

import "testing"

// SetAfterRead runs f at admit's test seam (see afterRead) until the test ends.
func SetAfterRead(t testing.TB, f func()) {
	afterRead = f
	t.Cleanup(func() { afterRead = nil })
}
