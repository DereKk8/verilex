# verilex

verilex lets verifier and QA agents run product verification as a chain of words, Unix-pipe style:

```
verilex run 'store-open | item-stored apple | item-listed apple'
```

A **word** is a reusable, executable piece that drives the product through a surface its users have and promises one product state. Someone who knows the product, but not how it is built, would recognize each word as a real moment in the product's life.

verilex holds execution only. The project's verification skill (a [pstack](https://github.com/cursor/plugins/tree/main/pstack)-style `verify-*` skill with its feature map) keeps the meaning: what a feature is, how a user reaches it, what proves it and its traps. A **claim** pins one piece of that meaning for verilex: what is true once a word passes, and the evidence that proves it. Every word proves a claim (or, in older projects, points straight at feature-map entries), and verilex never replaces the skill. When a word is not green, verilex prints the entries behind it so the agent can continue by hand.

This slice holds the chain runner, its trust frame, the three verdicts, quiet output, stamp-based skipping, `verilex plan`, live reuse of a kept instance (`verilex run --continue`), claims, run tickets, the word lifecycle (invention, onboarding and curation), and the generated word index.

## Install

```
go install github.com/DereKk8/verilex/cmd/verilex@latest
```

Use Go 1.27.1 or newer on a Unix system. Add `$(go env GOPATH)/bin` to your `PATH`.
The core is Go; frame steps and words can use any language installed on the product's workstation.

## A first look

`verilex run` prints the verdict first, then only the steps that are not green, each with a one-line cause and a path to its evidence:

```
$ verilex run 'store-open | item-stored apple | item-listed apple'
red: 1 green, 1 red, 1 not run; run 1767225600-a1b2c3d4e5f6
  red  item-stored apple: tally said 'added apple' but store.json lacks apple
    evidence: ~/.local/state/verilex/tally/runs/1767225600-a1b2c3d4e5f6/02-item-stored
    verify skill: verify-tally/features/items.md#item-add
```

| Verdict | Means | Exit |
|---|---|---|
| `green` | The product works, backed by evidence. | `0` |
| `red` | The product is broken, backed by evidence. | `1` |
| `inconclusive` | Environment or harness trouble, or a word's report that is not backed. Never reported as a product failure. | `2` |

A refused command also exits `2`. [Getting started](docs/getting-started.md) walks through a whole session on the sample product, from a first green run to a skipped one.

## Documentation

The documentation lives in [`docs/`](docs/README.md).

| | |
|---|---|
| **Start here** | [Getting started](docs/getting-started.md) · [Core concepts](docs/concepts.md) · [Commands](docs/commands.md) |
| **Running chains** | [The chain grammar](docs/chains.md) · [Verdicts](docs/verdicts.md) · [The trust frame](docs/trust-frame.md) · [Proof stamps and skipping](docs/skipping.md) · [Plan](docs/plan.md) · [Continuing a kept instance](docs/continue.md) |
| **Claims and words** | [Claims](docs/claims.md) · [The word lifecycle](docs/word-lifecycle.md) · [Onboarding](docs/onboarding.md) · [The word index](docs/word-index.md) |
| **Agents and the launcher** | [Run tickets](docs/tickets.md) · [Companion launcher](docs/launcher.md) · [Verdict rule](docs/verdict-rule.md) · [Sandbox](docs/sandbox.md) |
| **Your project** | [Adding verilex to a project](docs/adding-verilex.md) · [Development](docs/development.md) |
| **Decisions** | [Where the shared ledger lives](docs/ledger-placement.md) |

Browse the same pages as a site, with instant search and Ask, which answers from these docs with your own Claude, ChatGPT or Grok plan, the Claude API, or the docs alone:

```
scripts/docs            # http://127.0.0.1:4173
```

Sign in from the Ask menu; the server runs only on your machine. See [the docs site](docs/development.md#the-docs-site).

## Development

```
go test ./...
go vet ./...
go build -o verilex ./cmd/verilex
```

`scripts/ci` runs the whole blocking set that CI runs. See [Development](docs/development.md).
