# The trust frame

Every run goes through the project's frame, and no word can opt out of it.

## The five steps

1. **Launch** creates an instance owned by this run. verilex never attaches to an instance it did not launch; `--continue` only takes over one that an earlier `--keep` run launched.
2. **Doctor** confirms the instance is this run's and worth driving, before the first word and again after any failure. A refusal stops the run as `inconclusive`.
3. **Evidence** for every step goes to `~/.local/state/verilex/<project>/runs/<run>/` (override with `VERILEX_HOME`), outside the product and every repository.
4. **Honesty rules** turn a word's report into `inconclusive` when it is not backed, so it is never green or red:
   - `pass` without a second observation;
   - `fail` without stating that its preconditions held;
   - an exit code that disagrees with the verdict, or no result JSON;
   - a secret pattern (private key block, GitHub, AWS, Slack or Anthropic token, or a project pattern) anywhere in its evidence.
   Workstation and environment trouble is `inconclusive`, never `red`.
5. **Cleanup** always runs, tears down only what the run started, and verilex then confirms the evidence survived.

## Parallel runs

Many runs may target one product at once, from one machine or from many stateless instances, and no two of them share a mutable instance. Each run gets an id no other run holds (`<epoch>-<12 hex digits>`) and passes it to every frame step and word as `VERILEX_RUN`. Launch labels the instance it creates with that id, the doctor refuses an instance that does not carry it before any word runs, and cleanup removes only what carries it. A run holds its instance for as long as its process lives:

- `verilex cleanup <run>` and `verilex run --continue <run>` refuse a run that is still going, before anything starts: `verilex: refused: <run> is still running and owns its instance; wait until it finishes`.
- A kept instance goes to exactly one continuing run. Every other attempt to take it over is refused and records no run.
- A run whose process died holds nothing: `verilex runs` shows it `died`, and `verilex cleanup <run>` tears down the instance it left behind.
- Reading a run never blocks another: concurrent `verilex plan --continue <run>` calls all answer.
