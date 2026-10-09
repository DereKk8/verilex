# Development

```
go test ./...
go vet ./...
go build -o verilex ./cmd/verilex
```

The Go behavior tests drive the compiled CLI against tally. The sample product and its words
use Python 3, so tests require `python3` and a POSIX shell. The verilex core does not require Python.
The launcher's tests (`cd agent && go test ./...`) run each brain in the [sandbox](sandbox.md), so they
also need `bwrap` on Linux or `sandbox-exec` on macOS.

## CI and the canary

`scripts/ci` runs the whole blocking set that CI runs. Its `canary` step has verilex prove its own
claims: the repository's `.verilex/` holds words that build the verilex under test from the checkout and drive it
on a scratch tally, each claim anchored on a `verify-verilex` sub-feature. A known-good build pinned in
`scripts/ci` (`canary_driver`) runs them live in throwaway state, never the build under test, and the step fails
on a red or inconclusive run and on drift. Onboard a changed canary word with that pinned build.
Its `canary-guards` step (`scripts/canary-guards`) replays known attacks on the canary step, each on a scratch
copy of the checkout, and each must fail it.

## The docs site

`site/` serves `docs/` as a web app: instant search (`⌘K` or `/`), and Ask (`⌘I`), which answers questions from these pages and links to the sections it used. `docs/README.md` is its navigation: each `##` heading is a section and each list item a page, so a new page joins the site by joining the index.

```
scripts/docs                    # serve on http://127.0.0.1:4173
scripts/docs build -o DIR       # write a static site to DIR; it also opens from disk
scripts/docs check              # validate the index, page titles, links and anchors
```

The server reads `docs/` on every page load, so an edit shows on the next refresh.

### Who answers Ask

Ask answers with the provider you last chose, or else the first one that is ready; its menu switches between them:

| Provider | Answers with | Sign in |
|---|---|---|
| Claude | your Claude plan, through Claude Code (`claude`) | Sign in in the Ask menu, or `claude auth login` |
| ChatGPT | your ChatGPT plan, through the Codex CLI (`codex`) | Sign in in the Ask menu (a device code), or `codex login` |
| Grok | your SuperGrok or X Premium+ plan, through Grok Build (`grok`) | run `grok` once in a terminal |
| Claude API | `ANTHROPIC_API_KEY`, or a profile from `ant auth login` | restart `scripts/docs` with the key |
| Docs only | no model: it quotes the passages that match best | none |

Sign in in the Ask menu runs the vendor's own sign-in command on your machine and shows the page or code it asks for. The docs server never reads a token. For a plan, it runs the vendor's CLI in an empty scratch directory, with its tools off where the CLI allows it (Claude Code: `--tools ""` and no MCP servers; Codex: a read-only sandbox), and the docs as its only input. It removes `ANTHROPIC_API_KEY` from Claude Code's environment and `CODEX_API_KEY` from Codex's, so the plan is used, not a key. Every answer is grounded in the whole of `docs/` and cites the sections it used.

`-claude-model` and `-codex-model` pick the model a plan answers with (default: the CLI's own), and `-model` the API's (default `claude-opus-5-5`); `-effort` sets the effort for Claude Code, Codex and the API (default `low`). Grok Build's headless flags are not settled, so `-grok` sets its command (default `grok -p {prompt}`).

Plans are personal. The server listens on `127.0.0.1` and answers only requests from its own pages, so another site cannot spend a plan or start a sign-in. With `-addr` on another interface, plans and sign-in are off, and only the API key answers. A static build has no server: Ask quotes the docs, and when the page runs as a claude.ai artifact, it answers on the reader's own claude.ai account.

### The module

`site/` is a Go module of its own, outside the workspace in `go.work`, so the verilex core and the launcher keep no dependencies. `scripts/docs` builds it with `GOWORK=off`. Because it is a server, its `go.mod` requires Go 1.27.2, the first release with that line's `net/http` and `crypto/tls` fixes; the `go` command downloads it when yours is older, unless `GOTOOLCHAIN=local` is set. `scripts/ci site` vets and tests it and checks the docs; CI runs that step with `lint`.
