---
name: pr-author-prompts
description: "Post the PR author's own sanitized prompts, the words that led to the change, as one `## Author prompts` comment on the PR; use whenever you open a PR or push to one. Asks the author once for consent and saves the answer for every repository and future session."
---

# PR author prompts

Every PR carries the author's own words that led to it, so reviewers and later agents can check the code against the original intent. Follow these steps each time you open a PR or push to one.

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
- Keep the author's words. Never include the full conversation, agent replies, tool output or your summary.
- When an orchestrator's instructions quote the author (for example a section that quotes the author's request), those quoted words are the author prompts.
- Leave out prompts about unrelated work.

## 3. Sanitize

Remove each of these and put `[redacted: <kind>]` in its place:

- secrets, tokens, API keys, passwords, credentials, connection strings
- personal data about anyone other than the author
- patient, clinic or tenant data
- internal hostnames, IP addresses and private URLs
- product- or company-specific material from other projects

Change nothing else: no rewording, no spelling fixes. When unsure, redact.

## 4. Post or update the comment

Write the comment body to a file in this exact shape:

```markdown
## Author prompts

> first prompt, most relevant, in the author's words

> next prompt, with a [redacted: token] where one was removed
```

Keep one comment per PR, not the PR body. Find an existing one and edit it in place. Create one only when none exists:

```bash
id=$(gh api --paginate "repos/{owner}/{repo}/issues/$PR/comments" \
  --jq '.[] | select(.body | startswith("## Author prompts")) | .id' | head -n1)
if [ -n "$id" ]; then
  gh api -X PATCH "repos/{owner}/{repo}/issues/comments/$id" -F body=@prompts.md
else
  gh pr comment "$PR" --body-file prompts.md
fi
```

On a later push, add any new author prompts for that push and edit the same comment.
