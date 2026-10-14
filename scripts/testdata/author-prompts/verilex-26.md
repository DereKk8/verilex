## Author prompts

**Why this PR exists:** The author wanted the follow-up to the launcher work in PR 17 done: two older agent tests, TestBudgetBoundsTheRunWhateverTheBrainStarts (3s) and TestTimeBudgetWithoutAVerdictIsInconclusive (2s), put a wall-clock limit on the whole launch, so a very slow setup could exceed them and make CI flaky. The author approved counting both limits from the end of the budget through the injected run.Timer, as the row 14 and 15 tests already do, and approved that this changes what the two tests assert.

> The task is the follow-up from the launcher work in https://github.com/DereKk8/verilex/pull/17 (merged as 91021aa). Two older agent tests still put a wall-clock limit on the whole launch: TestBudgetBoundsTheRunWhateverTheBrainStarts (3s) and TestTimeBudgetWithoutAVerdictIsInconclusive (2s). They pass in local scripts/ci and PR CI but were not in the load runs, so a very slow setup could exceed them (flaky-CI risk). The approved fix: count both limits from the end of the budget through the injected run.Timer, as rows 14 and 15 already do. This changes what those two tests assert, and that change is approved.

> dispatch the verilex test launcher test task on opus
