package verdict

import "testing"

func TestJudgeKeepsTheHonestyRules(t *testing.T) {
	exit := func(code int) *int { return &code }
	cases := []struct {
		name    string
		code    *int
		stdout  string
		verdict Verdict
		reason  any
	}{
		{"backed pass", exit(0), `{"verdict": "pass", "observation": "seen"}`, Green, nil},
		{"backed fail", exit(1), `{"verdict": "fail", "preconditions_held": true, "detail": "broken"}`, Red, "broken"},
		{"blocked", exit(2), `{"verdict": "blocked", "detail": "locked"}`, Inconclusive, "locked"},
		{"timeout", nil, ``, Inconclusive, "timed out after 5s"},
		{"pass without observation", exit(0), `{"verdict": "pass", "observation": " "}`, Inconclusive, "pass without a second observation"},
		{"fail without preconditions", exit(1), `{"verdict": "fail", "detail": "broken"}`, Inconclusive, "fail without stating that its preconditions held"},
		{"exit disagrees", exit(0), `{"verdict": "fail", "preconditions_held": true}`, Inconclusive, "exit 0 disagrees with verdict fail"},
		{"three-verdict words are not reports", exit(0), `{"verdict": "green", "observation": "seen"}`, Inconclusive, "result has no verdict of pass, fail or blocked"},
		{"not json", exit(0), `ok`, Inconclusive, "stdout is not one result JSON object"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			j := Judge(c.code, []byte(c.stdout), 5)
			if j.Verdict != c.verdict || j.Reason != c.reason {
				t.Fatalf("got %s %v, want %s %v", j.Verdict, j.Reason, c.verdict, c.reason)
			}
		})
	}
}

func TestInconclusiveOverridesRedButNothingOverridesGreenOrInconclusive(t *testing.T) {
	red, green, inconclusive := Red, Green, Inconclusive
	cases := []struct {
		current *Verdict
		next    Verdict
		want    bool
	}{
		{nil, Red, true}, {&red, Inconclusive, true}, {&red, Green, false},
		{&inconclusive, Red, false}, {&green, Inconclusive, false},
	}
	for _, c := range cases {
		if got := Overrides(c.current, c.next); got != c.want {
			t.Errorf("Overrides(%v, %s) = %v", c.current, c.next, got)
		}
	}
}

func TestLegacyLabelsAndExitCodes(t *testing.T) {
	for label, want := range map[string]Verdict{"pass": Green, "fail": Red, "blocked": Inconclusive, "unverified": Inconclusive, "": Inconclusive, "green": Green, "red": Red} {
		if got := Legacy(label); got != want {
			t.Errorf("Legacy(%q) = %s, want %s", label, got, want)
		}
	}
	for v, code := range map[Verdict]int{Green: 0, Red: 1, Inconclusive: 2} {
		if v.ExitCode() != code {
			t.Errorf("%s exits %d", v, v.ExitCode())
		}
	}
}
