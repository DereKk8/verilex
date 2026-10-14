## Author prompts

**Why this PR exists:** The author wants a bug that turns up during a pull request review fixed right away, not parked as a follow-up. A review of the launcher's budget tests found that a run's stop ends only the brain's process group, so a child that calls setsid could outlive the run on macOS, where no pid namespace ends it. The author asked to prove or disprove that on macOS and, if it reproduces, fix it in the same change with the test as proof.

> pre-existing bug found during a PR review is fixed immediately, not parked as a follow-up.
