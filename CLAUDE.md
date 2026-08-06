# CLAUDE.md

@AGENTS.md

Everything shared with other tools lives in `AGENTS.md` (Cursor, Codex and the
rest read that one). Below are behavioural rules for Claude Code specifically,
adapted from Andrej Karpathy's published guidelines with one deliberate change
for hackathon pace.

Both files are English because nothing but an agent ever reads them. Docs written
for the team, and all code comments, stay in Russian — see "Language" in `AGENTS.md`.

## Think before writing code

- **Don't fill in gaps silently, don't hide confusion.** If a task has several
  readings, name them instead of picking one quietly. If something is unclear,
  stop and ask.
- **Minimal code.** No abstractions and no "flexibility" nobody asked for. If you
  wrote 200 lines where 50 would do, rewrite it shorter.
- **Scoped edits, not surgery at any cost.** Karpathy asks you to touch only lines
  that trace directly to the task. On a 7-day hackathon that is sometimes too
  strict: if something in the way must be fixed for the task to be finishable at
  all — or it's an obvious neighbouring bug in the same file — fix it and say so in
  the first line of your reply. Fixing silently, and rewriting style or formatting
  to taste, are different things and both stay forbidden.
- **Define done before starting.** "Fix the bug" → "write a test that reproduces
  it, then make it green". A weak criterion needs constant clarification; a strong
  one lets you work to completion on your own.

## Verification

Don't add separate verification passes or verifier subagents on top of what you
already do. The real gates are `/verify`, the Stop hook and CI; anything else
burns tokens for nothing.

The exception is not self-checking but correctness: the race test for reward
claims and the idempotency test against a real Postgres. Always write those.

## Subagents

Only for genuinely independent, large branches of work — never to double-check
yourself. There is no project subagent: a reviewer that re-reads your own diff is
exactly the self-checking this rule forbids, and once codegen lands the compiler
catches contract drift anyway.

## Reply format

One line about what you're about to do, before the block of work. Then an update
when you find something or change direction. A conclusion at the end, not a
retelling of the process.

Correct something you said earlier if it changes a number or a conclusion. Minor
slips: just fix them silently.

## Slash commands

`.claude/commands/` holds `/plan`, `/implement`, `/verify`, `/review`. Run
`/verify` before calling anything done. They are a Claude Code convenience and
exist nowhere else — Cursor and Codex read `AGENTS.md`, which deliberately does
not depend on them. `make verify` is the part everyone shares.

## Hooks

`.claude/settings.json` is committed and shared by the team. Personal settings go
in `.claude/settings.local.json` (gitignored).

Two hooks have already shipped broken and silently passed everything — treat any
hook you write as broken until you have watched it fail. `.claude/hooks/guard_test.sh`
exists for exactly that reason and runs inside `make verify`.
