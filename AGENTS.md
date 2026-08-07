# Avito Tamagochi

Go backend + React frontend. Hackathon, ~7 days, four people, one repository.

**This file is in English on purpose: it is loaded into every agent session by
Claude, Cursor and Codex, and most of its vocabulary is English anyway.**
Docs written for people to read — `README.md`, `docs/SETUP.md`, `docs/DECISIONS.md` —
stay in Russian, and so do code comments. See "Language" below.

`[agent]` marks lines that only apply to AI assistants; a human skips those.

## Ground truth

`docs/openapi.json` is the contract. If you change the shape of a request or a
response, edit the spec in the same diff.

⚠️ Two exceptions, both written as conditions rather than as claims about today —
check them, don't trust this file to have been updated:

- **If `make gen` prints that codegen is not wired**, then types are written by
  hand: copy the spec literally, field for field, and say so in the pull request.
  Once it is wired, generated types win and hand-written ones are a bug.
- **`nickname` in the spec is a display name, not a credential.** Sign-in is
  `login` + `password` (decided 06.08). `Me.nickname` and the leaderboard use the
  display name and are unrelated to auth — don't "unify" them.

`make reconcile` answers both mechanically — it reads the repository, not this file.

## Never

- Never award XP for an action without writing an entry to the action log with an
  idempotency key and a daily-cap check. Care actions *do* grant XP (decided
  06.08), so what protects against farming is the log and the cap — not refusing
  the action.
- Never hand out a reward as a code string that can be forwarded to someone else.
- Never lose `source: gamified|organic` between `event_inbox` and `energy_ledger`.
- Never reset a user's progress in a migration.
- Never draw the Avito mark (the four circles) as the pet character.
- Never call `time.Now()` inside domain logic — pass time in as a parameter.
- Never introduce an interface before a second real implementation exists. The one
  exception is `repo.go` behind an interface: the second implementation is the
  fake used in domain tests.
- Never let `internal/<tag>/service.go` depend on `net/http` or Gin.
- Never reach the database from `handler.go` or `service.go` — only through `repo.go`.

## Always

- Branches `feat/<area>`, `fix/<area>`, `chore/<area>`; small pull requests; `main`
  always works. `pre-push` and `commit-msg` hint at the branch format and
  Conventional Commits, but they only warn — following it is still on you.
- Conventional Commits (`feat(rewards): ...`), from your own account.
- **Code comments and doc comments are written in Russian**, which is what the
  team already does in `backend/`. This file being English does not change that.
- If you touch someone else's package, `docs/openapi.json`, or the `event_inbox` /
  `energy_ledger` / `reward_grants` tables, say so explicitly in the PR description.
- Spot a problem next to your task — name it, don't silently fix it. The exception
  is something small you must fix to finish the task at all: then fix it and say so.
- `[agent]` Same thing, but in the first line of your reply, not a footnote at the end.
- `[agent]` Show command output, not a summary of it. "passed" is not a result.
- `[agent]` For an unfamiliar library API, check the docs of the installed version
  instead of writing from memory. Gin majors, gorilla/websocket and especially
  Chakra differ a lot between versions.

## Stop questions — the team decides, not the agent

Hit any of these and stop and ask. Details in `docs/DECISIONS.md` → «Открытое».

**Reward entitlement** (may a `promoCode` be handed out as a string) · **the level
curve** · **the pet concept** · **Redis** (default is no).

Energy/progress, the auth model and Chakra (v3, picked by the frontend in code)
are no longer stop questions. Everything else: decide it yourself, don't ask.

## Layout

One package per OpenAPI tag, three files inside: `handler.go` (HTTP),
`service.go` (domain, pure functions, time as a parameter), `repo.go` (the only
place with SQL). Shared infrastructure lives in `backend/pkg/`, below all features.

Full tree, the "who may import what" table and the invariants are in
`docs/ARCHITECTURE.md`. Package boundaries are contract boundaries.

## Commands

`make` is the only entry point — don't assemble flags from memory. Full list and
what each one checks: `README.md`. Minimum before a PR: `make verify`.

Useful: `make doctor` (is the local toolchain complete), `make reconcile` (what is
still left over from merging the feature branches, checked against the code).

A target with nothing to do says so itself when you run it. There is deliberately
no list of unfinished targets here: such a list goes stale the moment someone
finishes one, and a target does not.

`[agent]` Several agents on one repository — each in its own git worktree.
Claude Code additionally has project slash commands; they are described in
`CLAUDE.md` and do not exist in other tools, so nothing here depends on them.

## Language

- **English:** this file and `CLAUDE.md` — they exist to be loaded into agent context.
- **Russian:** `README.md`, `docs/SETUP.md`, `docs/DECISIONS.md`,
  `docs/ARCHITECTURE.md`, `docs/RECONCILIATION.md`, `docs/AI-USAGE.md`, all code
  comments, commit messages, PR descriptions. The team is Russian-speaking and the
  defense is in Russian; `docs/DECISIONS.md` in particular doubles as defense notes.

## Other files

Not loaded automatically — read them when the task touches them:
`README.md` (run and verify) · `docs/SETUP.md` (what to install) ·
`docs/DECISIONS.md` (decided, open, deferred) ·
`docs/ARCHITECTURE.md` (layout, layers, invariants) ·
`docs/RECONCILIATION.md` (post-merge fixes; the file disappears when done) ·
`docs/AI-USAGE.md` (for the defense, assembled on day 6).
