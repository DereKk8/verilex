---
name: pr-author-prompts
description: "Post one `## Author prompts` comment on the PR: a self-contained paragraph on why the PR exists, then the author's own sanitized prompts that led to it; use whenever you open a PR or push to one. Asks the author once for consent and saves the answer for every repository and future session."
---

Skill version: 1.1.1

# PR author prompts

Every PR carries why it exists and the author's own words that led to it, so reviewers and later agents can check the code against the original intent without the conversation. Follow these steps each time you open a PR or push to one.

## 1. Check consent

The answer lives in one file per person, shared by every repository that uses this skill:

```bash
consent="${XDG_CONFIG_HOME:-$HOME/.config}/pr-prompts/consent"
cat "$consent" 2>/dev/null
```

- `yes`: go to step 2.
- `no`: stop. Post nothing.
- Missing: ask the author in chat, before the first PR: "May I post your sanitized prompts that led to each PR as a PR comment, from now on, in every repository that uses this skill?" Save the answer and never ask again:

  ```bash
  mkdir -p "$(dirname "$consent")" && printf 'yes\n' > "$consent"   # or no
  ```

- Missing and the author cannot answer (headless run, launched by an orchestrator): post nothing, and say in your report that author prompts were skipped because consent is not saved.

Tell the author once, when you save the answer, that they change it by editing or deleting that file. Never store the answer in `git config`.

## 2. Collect the prompts

- Take only the author's own prompts that led to this change. Put the most relevant first.
- Keep the author's words. Never include the full conversation, agent replies, tool output or your summary as a quote.
- When an orchestrator's instructions quote the author (for example a section that quotes the author's request), those quoted words are the author prompts.
- Never quote orchestrator-written briefing as an author prompt. Task instructions often add paragraphs the author did not write (for example a weekly sync summary). Use those facts only to write the Why paragraph in step 3; do not paste them under the quotes.
- Leave out prompts about unrelated work, including a session-wide request to land all work when it says nothing about this PR.
- If no author prompt relates to this PR, omit the quotes. End the Why paragraph with `No PR-specific author prompt is available.` Never quote briefing or invent a narrower author request to fill the gap.

## 3. Pass the cold-read test

Ask, on every invocation: could a person or agent who never saw the conversation read this comment and understand why the PR exists, and roughly predict what it changes?

The latest prompt often fails that test. It can be an answer to a question the reader never sees ("1. yeah review 2. A"), a go-ahead ("send patching for the review findings"), or a general rule that does not name this change. Such a fragment is not the reason the PR exists.

Keep the author's motive separate from the concrete problem and change. Derive the motive only from the author's own words. Use the diff, issue, test result or task brief to explain what this PR fixes and changes. A broad goal such as "land all active work safely" is not a concrete problem. Do not turn a technical finding into a motive the author never gave.

Every Why paragraph states the concrete problem or missing behavior and what this PR changes, even when the author's only request is broad. For a bug, name the failing condition and its visible result. For example, a 300-character failure message cut off the reason when a path was long. Say that the PR moves the reason before the paths, rather than saying only that it fixes a guard. For a new capability, name what users cannot do and what they will be able to do.

When the latest prompt alone fails the test:

- Trace back through the author's own words: the earlier prompts in the conversation, and the author's words quoted in the task instructions. Find the prompt where the author first said what they want and why.
- Write the **Why this PR exists** paragraph. Start it with the author as the subject ("The author wants ...", "The author asked ..."). State their request and reason only as far as their words support them. Then name the concrete problem and change, preferably with "This PR ...". Write the paragraph in your own words and never present it as a quote.
- Put every fact a cold reader needs into that Why paragraph as plain prose. Agents are the main readers; humans read it to learn intent. Lead with the point. Use short literal sentences, one idea each, active voice, and common words. Do not use jargon, noun stacks, or em dashes (use "-").
- Keep each Why sentence short and literal, with one idea, at about 25 words at most.
- Define every Verilex-specific term on first use, or replace it with plain words. Use `term (plain definition)` with at least three words for the definition. For example, `word (an executable verification check)` or `canary guard (a test of Verilex's own verification)`. Define a term used in a quote in the Why first, so the quote can stay unchanged. Literal commands and paths in backticks need no glossary expansion.
- The checker knows these overloaded terms: word, claim, frame, brain, chain, canary guard, onboarding evidence, proof stamp, drift-suspect, onboarding, admission, admitted, provisional and inconclusive. It checks plural forms where applicable and exempts ordinary phrases such as "plain words" and "the author's own words". You must also explain other technical terms that a cold reader would not know.
- The comment has no separate context section. Do not add a line or paragraph labeled `Context:`, `Background:`, or similar between the Why paragraph and the quotes. Never paste orchestrator briefing there as a pointer; fold the needed facts into the Why paragraph instead.
- Technical sources (the linked issue, a review report, the PR description) may add facts a reader needs, after the author's motive. They never replace it.
- Keep a fragment quote only when it adds the author's voice about this PR on top of that paragraph. Drop answer lists that only make sense next to their questions, and omit broad requests that could be attached to any PR. Never add unrelated quotes just to satisfy the checker.
- When the author's words never say why, say plainly that they gave no PR-specific reason. Still explain the concrete problem and change from the technical sources. Do not invent a motive.

A quote that names an option or item label ("option B", "Item 43, approved") means nothing to a reader who never saw the question. Find what the label meant in the author's earlier words or the task instructions, and append the resolution to that quote as `[option B: <self-contained sentence>]` or `[item 43: <self-contained sentence>]`, with at least 8 words. The label must match, and the author's words stay unchanged, as with `[redacted: kind]`. The checker catches only the explicit shapes "option X" (X one uppercase letter) and "item N" (N a number), and the labels listed right after them ("option B or C"). It does not catch other ways to point at a choice, so resolve each of those by hand too. Example: `> lets go with B [option B: rebuild the importer on the existing parser, keep the old file format readable, and ship it as one pull request]`.

Always write the paragraph, even when the quotes are clear or no relevant quote exists. Rewrite it on a later push when the PR's purpose has grown.

Wrong (pointer paragraph and orchestrator text in quotes):

```markdown
**Why this PR exists:** The author asked to start the weekly refit.

Context: this is the weekly refit full sync of the fork with upstream ...

> Context: this is the weekly refit full sync ...
```

Right (facts in the Why paragraph; quotes are only the author's words):

```markdown
**Why this PR exists:** The author asked to start the weekly refit and merge reconciliation, then run validation on Opus. They want the fork brought up to date with upstream before the usual pipeline checks.

> Hey mate let's start with our weekly /refit send a sol worker on the merge reconciliation then validation on opus
```

A quote may still use the word "context" in a normal sentence (for example "no context bloat"); only a labeled pointer paragraph or block is forbidden.

## 4. Sanitize

Remove each of these and put `[redacted: <kind>]` in its place:

- secrets, tokens, API keys, passwords, credentials, connection strings
- personal data about anyone other than the author
- patient, clinic or tenant data
- internal hostnames, IP addresses and private URLs
- product- or company-specific material from other projects

Sanitize the paragraph the same way. Inside quotes change nothing else: no rewording, no spelling fixes. When unsure, redact.

## 5. Check, then post or update the comment

Write the comment body to a file in this exact shape:

```markdown
## Author prompts

**Why this PR exists:** The author wants ... (what they asked for, why in their terms, and what it means for this change), readable without the conversation.

> first prompt, most relevant, in the author's words

> next prompt, with a [redacted: token] where one was removed
```

Post it only through this skill's script, run from the repository root. It runs `check.sh` first and posts nothing when the check refuses; fix every refusal and run it again:

```bash
.agents/skills/pr-author-prompts/post.sh "$PR" prompts.md
```

The checker refuses a missing or thin Why paragraph, one that does not start from the author, one that points at missing context, separate context blocks, numbered answer fragments and unresolved option/item labels. It also requires an action from its finite vocabulary and at least two topic words beyond shared goals. Each quote must share at least one topic word with the Why, after case folding, removing a trailing `s`, and excluding common goal words. It requires inline definitions for its known Verilex terms. Without quotes, the Why must include the exact no-prompt statement from step 2.

These are mechanical checks, not proof of meaning. The checker cannot establish that a problem is true, that an unfamiliar term is plain, or that a shared word makes a quote relevant. Read the comment against the diff and apply step 3 even when it passes. If a relevant quote uses different words, explain that relationship truthfully in the Why without changing the quote. The script keeps one comment per PR: it edits the existing `## Author prompts` comment in place and creates one only when none exists. Never post or edit the comment with `gh` directly.

On a later push, add any new author prompts for that push, redo step 3, and edit the same comment.
