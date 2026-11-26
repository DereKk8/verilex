package curation

import "testing"

// SetAfterRead runs f at admit's test seam (see afterRead) until the test ends.
func SetAfterRead(t testing.TB, f func()) {
	afterRead = f
	t.Cleanup(func() { afterRead = nil })
}

// SetAfterFiles runs f at propose's test seam (see afterFiles) until the test ends.
func SetAfterFiles(t testing.TB, f func()) {
	afterFiles = f
	t.Cleanup(func() { afterFiles = nil })
}
