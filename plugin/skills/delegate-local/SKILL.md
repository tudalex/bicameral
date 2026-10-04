---
name: delegate-local
description: How and when to hand work to the local-worker subagent, which runs on a local model on this machine (free, private, slower, less capable). Use whenever a task contains well-specified mechanical steps — targeted edits, renames, applying a known fix, running tests or builds and fixing obvious failures, collecting file contents or command output — that don't need your judgment.
---

# Delegating to the local model

This session runs under bicameral: the `local-worker` subagent is served by a
local model, not Claude. Its tokens cost nothing and never leave the machine.
It is good at following precise instructions and weak at ambiguity, design,
and long open-ended investigations.

You are the deep thinker; it is the hands. Decide *what* to do, let it *do* it.

## Delegate when

- The change is fully specified: you can name the files, the functions, and
  what "done" looks like.
- The work is bulky but simple: mechanical edits across files, boilerplate,
  applying a pattern you've already shown once.
- It's a run-check-fix loop with an obvious target (tests, lint, build).
- You need raw material gathered (file contents, grep results, command output)
  and don't need to reason while gathering it.

Keep for yourself: diagnosing unclear bugs, design decisions, anything
security-sensitive, and anything where a wrong-but-plausible result is costly.

## Writing the brief

The local model only knows what you put in the prompt. Include:

1. Absolute paths of the files to touch (and which not to touch).
2. The exact change, as specific as you can — function names, expected
   behaviour, code snippets if the edit is subtle.
3. The verification command, and that it must report its output verbatim.
4. The stop rule: if stuck after two attempts, report the error and stop.

Split big jobs into several focused delegations rather than one sprawling one;
independent pieces can run in parallel.

## After it returns

Don't trust the summary — verify. Check `git diff` for the files it touched
and re-run the verification command yourself before reporting success. If it
failed or went off-scope, either fix it yourself or send a corrected, more
specific brief.
