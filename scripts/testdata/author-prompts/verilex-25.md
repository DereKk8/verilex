## Author prompts

**Why this PR exists:** The author wants the PR authoring-prompt skill to carry, in every PR, the real reason the change exists, told in the author's own terms with the author as the subject, so a reader who never saw the conversation understands the intent and can roughly predict the change. The author also wants that motive reconstructed from the author's own prompts instead of the agent's diagnosis of the problem, because the author's desire and the capability they ask for are what the comment must preserve.

> the PR authoring-prompt skill so every author prompt embedded in a PR body is self-contained and communicates the original intent behind the change. Some entries currently capture isolated follow-up answers or responses to clarifying questions rather than the actual reason the PR exists. ... On every invocation, the skill must evaluate: Could a human or agent cold-reading this author prompt understand why the PR exists and intuitively anticipate what changes it introduces? If not, reconstruct the original problem, intended outcome, and relevant context rather than preserving an incomplete conversational fragment.

> we should derive the why from the author, not from your thought of the problem ... You need to come up with intuitive of why the author is prompting you his desire and capability
