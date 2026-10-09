# verilex documentation

verilex lets verifier and QA agents run product verification as a chain of words, Unix-pipe style. New here? Read [Getting started](getting-started.md), then [Core concepts](concepts.md).

This index is also the navigation of the docs site: each `##` heading is a section and each list item a page, in order. Browse it with instant search and Ask, which answers from these pages with your own Claude, ChatGPT or Grok plan, by running `scripts/docs` from the repository root. See [Development](development.md#the-docs-site).

## Start here

- [Getting started](getting-started.md): install verilex and run a first session on the sample product, from a green run to a skipped one.
- [Core concepts](concepts.md): words, chains, claims, verdicts and stamps on one page, with a glossary.
- [Commands](commands.md): every command, its flags and the exit codes.

## Running chains

- [The chain grammar](chains.md): how words compose with `|`, and the order of reality verilex enforces.
- [Verdicts](verdicts.md): green, red and inconclusive, and the quiet output that reports them.
- [The trust frame](trust-frame.md): launch, doctor, evidence, honesty rules and cleanup, and parallel runs.
- [Proof stamps and skipping](skipping.md): what a stamp covers, when a chain is skipped, and the shared ledger.
- [Plan](plan.md): the skip decision without a run, and claim plans for a diff.
- [Continuing a kept instance](continue.md): `--keep` and `--continue`, and why a word that changes state never runs twice.

## Claims and words

- [Claims](claims.md): what a claim pins, its versions, its sources and when it needs review.
- [The word lifecycle](word-lifecycle.md): invent, use, onboard or propose, admit and drift.
- [Onboarding](onboarding.md): the mechanical checks, trials and decisions that admit a word.
- [The word index](word-index.md): active claims, a claim's words and one word, generated on every call.

## Agents and the launcher

- [Run tickets](tickets.md): what a run proves and which brain drives it, with profiles and precedence.
- [Companion launcher](launcher.md): `verilex-agent`, which returns verilex's own verdict for a brain's runs.
- [Verdict rule](verdict-rule.md): how the launcher turns the brain's runs into one result, row by row.
- [Sandbox](sandbox.md): what the brain can read, write and reach, and what the sandbox does not cover.

## Your project

- [Adding verilex to a project](adding-verilex.md): the `.verilex` directory, the frame and the word contract.
- [Development](development.md): building and testing verilex, CI, the canary and the docs site.

## Design

- [Where the shared ledger lives](ledger-placement.md): why the ledger is a directory of pass files, with measurements.
