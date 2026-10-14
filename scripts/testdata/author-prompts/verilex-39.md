## Author prompts

**Why this PR exists:** The author wants Verilex in real use quickly, with actual testing and repeated feedback instead of perfect initial setup. The author also wants a scripted brain to try the sandbox's permitted actions. This change makes Linux CI exercise those capabilities and refusal edges without a model, then checks that sandbox attempts preserve the real green, red and inconclusive verdicts.

> I really like the idea of having a brain just try everything the sandbox allows.

> Start the pending Verilex tasks in parallel while we resolve the remaining questions. Prioritize getting Verilex into real use as soon as possible rather than perfecting its initial setup. Begin testing it, collecting actual usage data and evaluation metrics, identifying limitations, and iteratively patching and hardening the tool based on empirical findings. Optimize for rapid feedback and continuous improvement over upfront completeness.
