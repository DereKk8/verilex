# Run tickets

A run ticket fixes everything about one verification run: what it is meant to prove and which brain (harness, model and effort) drives it. Many independent runs can each choose their own brain without editing a shared file. verilex carries and validates the ticket and never acts on its brain fields: it launches no harness and calls no model. A companion launcher reads the resolved ticket and starts the harness.

```yaml
intent: prove a renamed item keeps its count   # what the run is meant to prove
diff: main...HEAD          # the change to verify, as a git revision or range; a ticket names intent, diff or both
profile: deep-verify       # a named profile, optional
harness: claude-code       # optional when a lower level sets it
model: claude-opus-5-5
effort: high               # minimal, low, medium, high, xhigh or max
token_budget: 400000       # optional, a positive whole number
time_budget: 45m           # optional, a positive duration such as 30m or 1h30m
```

## Precedence

Each of `harness`, `model`, `effort`, `token_budget` and `time_budget` comes from the first level that sets it:

1. the ticket;
2. its named profile;
3. the project default, in `.verilex/profiles.yaml`;
4. the user default, in `$XDG_CONFIG_HOME/verilex/profiles.yaml` (by default `~/.config/verilex/profiles.yaml`);
5. the built-in, which sets only `effort: medium`.

## Profiles

A profiles file holds `defaults` (any of those fields, plus a default `profile`) and named `profiles`:

```yaml
defaults:
  profile: quick-verify
  harness: claude-code
profiles:
  quick-verify: {model: claude-haiku-4-5, effort: low, time_budget: 10m}
  deep-verify: {model: claude-opus-5-5, effort: xhigh, token_budget: 400000}
```

The profile is the ticket's `profile`, else the project default's, else the user default's. A project profile replaces a user profile of the same name as a whole. A resolved ticket must name a harness and a model.

## Validating a ticket

`verilex ticket <file>` prints the resolved ticket, each field with the level it came from; `--json` prints it whole, with `from` naming each level:

```
$ verilex ticket deep.yaml
intent: prove a renamed item keeps its count
profile: deep-verify  from ticket
harness: claude-code  from user default
model: claude-opus-5-5  from project profile deep-verify
effort: xhigh  from project profile deep-verify
token_budget: 400000  from project profile deep-verify
```

`verilex run --ticket <file>` and `verilex plan --ticket <file>` resolve the ticket before anything starts, and refuse an invalid one with exit code `2`, naming the file and the field:

```
$ verilex run --ticket deep.yaml 'store-open | item-stored apple'
verilex: refused: ticket deep.yaml: effort: "turbo" is not one of minimal, low, medium, high, xhigh, max
```

## What a ticket changes

A run records its resolved ticket in its own run record (`ticket` in `--json` and `run.json`), and a plan prints it in `--json`. verilex only reads the ticket and profiles files and writes nothing shared for a ticket, so concurrent runs with different tickets never conflict. The ticket never changes what runs or what is skipped: it is not part of any proof stamp, and `.verilex/profiles.yaml` is not either.
