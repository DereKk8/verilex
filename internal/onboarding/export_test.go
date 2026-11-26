package onboarding

import "testing"

// SetAfterReview runs f at Onboard's test seam (see afterReview) until the test ends.
func SetAfterReview(t testing.TB, f func()) {
	afterReview = f
	t.Cleanup(func() { afterReview = nil })
}
