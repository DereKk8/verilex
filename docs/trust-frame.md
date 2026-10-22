# The trust frame

Every run goes through the project's frame, and no word can opt out of it.

## The five steps
<!-- claims: chain-runs-live -->

1. **Launch** creates an instance owned by this run. verilex never attaches to an instance it did not launch; `--continue` only takes over one that an earlier `--keep` run launched.
2. **Doctor** confirms the instance is this run's and worth driving, before the first word and again after any failure. A refusal stops the run as `inconclusive`.
3. **Evidence** for every step goes to `~/.local/state/verilex/<project>/runs/<run>/` (override with `VERILEX_HOME`), outside the product and every repository.
4. **Honesty rules** turn a word's report into `inconclusive` when it is not backed, so it is never green or red:
   - `pass` without a second observation;
   - `fail` without stating that its preconditions held;
   - an exit code that disagrees with the verdict, or no result JSON;
   - a secret pattern (private key block, GitHub, AWS, Slack or Anthropic token, or a project pattern) anywhere in its evidence, file and directory names, links and every field of the decoded report included. A token counts after any character that cannot continue an identifier and after an encoded byte such as `%3D`, `%253D`, `=3D`, `\n`, `\x3d`, `\075` or `\u003c`. verilex also reads every evidence file with its JSON escapes decoded, so a token written as `ghp\u005f...` counts in any file, even in a report that does not decode whole. verilex replaces each match with `[redacted]` in the evidence before it reads the report, the escapes it spans included, replaces a whole `stdout` whose secret only a decoded value shows (such as a project pattern anchored to a whole value), and turns a link whose target or target path holds a secret into a file holding only `[redacted]`, so no run record, usage ledger or output holds the secret. A link to a directory stays a link and verilex does not follow it, because its target lies outside the evidence and can be of any size: only the link's target path is checked. A file another user owns, such as one a container wrote as root, keeps its secret, because verilex cannot rewrite it; the run still names the leak.
   - evidence verilex cannot read, such as a file or directory another user owns and keeps closed: verilex cannot show that it holds no secret, so the word is `inconclusive` with `cannot scan evidence <name>: permission denied`, and the rest of its evidence is still scanned.
   Workstation and environment trouble is `inconclusive`, never `red`.
5. **Cleanup** always runs, tears down only what the run started, and verilex then confirms the evidence survived.

## Parallel runs

Many runs may target one product at once, from one machine or from many stateless instances, and no two of them share a mutable instance. Each run gets an id no other run holds (`<epoch>-<12 hex digits>`) and passes it to every frame step and word as `VERILEX_RUN`. Launch labels the instance it creates with that id, the doctor refuses an instance that does not carry it before any word runs, and cleanup removes only what carries it. A run holds its instance for as long as its process lives:

- `verilex cleanup <run>` and `verilex run --continue <run>` refuse a run that is still going, before anything starts: `verilex: refused: <run> is still running and owns its instance; wait until it finishes`.
- A kept instance goes to exactly one continuing run. Every other attempt to take it over is refused and records no run.
- A run whose process died holds nothing: `verilex runs` shows it `died`, and `verilex cleanup <run>` tears down the instance it left behind.
- Reading a run never blocks another: concurrent `verilex plan --continue <run>` calls all answer.
