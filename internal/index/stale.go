package index

import (
	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/runner"
)

// StaleClaim is a claim whose planned proof chain cannot be skipped. Inconclusive means the
// chain could not be planned or its ledger could not be read, not that the product failed.
type StaleClaim struct {
	Claim        string
	Reason       string
	Inconclusive bool
}

// Stale checks every grouped claim, including dormant claims and claims without a word. It
// uses the claim planner's word selection, argument binding and dependency order, then the
// exact skip decision run and plan use. It writes nothing and executes no word or frame step.
func (ix Index) Stale(p dictionary.Project) []StaleClaim {
	rows := []StaleClaim{}
	b := ix.bindings(p)
	for _, claim := range ix.Claims {
		plan, err := ix.proofPlan(p, b, claim.Claim)
		if err != nil {
			rows = append(rows, StaleClaim{Claim: claim.Claim, Reason: err.Error(), Inconclusive: true})
		} else if !plan.Skipped() {
			rows = append(rows, StaleClaim{Claim: claim.Claim, Reason: plan.Rerun})
		}
	}
	return rows
}

func (ix Index) proofPlan(p dictionary.Project, b binding, claim string) (runner.Plan, error) {
	chain, _, err := ix.order(b, []string{claim})
	if err != nil {
		return runner.Plan{}, err
	}
	steps, err := dictionary.ParseChain(chain, ix.words)
	if err != nil {
		return runner.Plan{}, err
	}
	if err = dictionary.CheckOrder(steps); err != nil {
		return runner.Plan{}, err
	}
	plan, err := runner.Decide(p, steps, runner.Options{})
	if err == nil {
		err = plan.Unreadable
	}
	return plan, err
}
