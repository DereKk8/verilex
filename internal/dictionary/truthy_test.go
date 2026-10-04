package dictionary

import "testing"

func TestTruthy(t *testing.T) {
	cases := []struct {
		val  any
		want bool
	}{
		{nil, false},
		{false, false},
		{true, true},
		{"", false},
		{"non-empty", true},
		{"0", true},
		{0, false},
		{1, true},
		{-1, true},
		{int64(0), false},
		{int64(10), true},
		{uint(0), false},
		{uint(5), true},
		{uint64(0), false},
		{uint64(5), true},
		{float64(0.0), false},
		{float64(1.5), true},
		{float32(0.0), false},
		{float32(1.5), true},
		{[]any{}, false},
		{[]any{0}, true},
		{[]string{}, false},
		{[]string{""}, true},
		{map[string]any{}, false},
		{map[string]any{"a": 1}, true},
		{map[any]any{}, false},
		{map[any]any{0: 0}, true},
		{struct{}{}, true},
	}
	for _, tc := range cases {
		got := Truthy(tc.val)
		if got != tc.want {
			t.Errorf("Truthy(%#v) = %v, want %v", tc.val, got, tc.want)
		}
	}
}
