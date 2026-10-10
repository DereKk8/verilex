---
name: pr-author-prompts
description: "Post one `## Author prompts` comment on the PR: a self-contained paragraph on why the PR exists, then the author's own sanitized prompts that led to it; use whenever you open a PR or push to one. Asks the author once for consent and saves the answer for every repository and future session."
---

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
- Leave out prompts about unrelated work.

## 3. Pass the cold-read test

Ask, on every invocation: could a person or agent who never saw the conversation read this comment and understand why the PR exists, and roughly predict what it changes?

The latest prompt often fails that test. It can be an answer to a question the reader never sees ("1. yeah review 2. A"), a go-ahead ("send patching for the review findings"), or a general rule that does not name this change. Such a fragment is not the reason the PR exists.

When the prompts alone fail the test:

- Trace back to the original problem: the earlier prompts, the task instructions, the linked issue, the review report or the PR description.
- Write the **Why this PR exists** paragraph from that: the problem, the intended outcome, and the context a reader needs. Write it in your own words and never present it as a quote.
- Keep a fragment quote only when it adds the author's voice on top of that paragraph; drop answer lists that only make sense next to their questions.

Always write the paragraph, even when the quotes are clear. Rewrite it on a later push when the PR's purpose has grown.

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

**Why this PR exists:** the problem, the intended outcome, and the context, readable without the conversation.

> first prompt, most relevant, in the author's words

> next prompt, with a [redacted: token] where one was removed
```

Post it only through this skill's script, run from the repository root. It runs `check.sh` first and posts nothing when the check refuses; fix every refusal and run it again:

```bash
.agents/skills/pr-author-prompts/post.sh "$PR" prompts.md
```

The checker refuses a missing or thin Why paragraph, one that points at context the reader lacks, and quotes that are numbered answers to unseen questions. It cannot judge meaning, so passing it does not replace step 3. The script keeps one comment per PR: it edits the existing `## Author prompts` comment in place and creates one only when none exists. Never post or edit the comment with `gh` directly.

On a later push, add any new author prompts for that push, redo step 3, and edit the same comment.
