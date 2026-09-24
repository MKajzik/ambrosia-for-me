# Agent Guide

Tool-neutral guidance for any coding agent in repo. `CLAUDE.md` holds project facts + commands; this file holds how to work.

## Workflow

1. **Spec first.** Non-trivial work traces to design spec in `docs/superpowers/specs/`. Behaviour unspecified → propose spec change before coding.
2. **Plan.** Execute from plan in `docs/superpowers/plans/`, task by task.
3. **Tests first.** Write failing test, watch fail, write minimum code, watch pass. Applies to all service logic + every bug fix.
4. **Contract first.** API shape changes → edit `openapi.yaml`, run `make lint-api`, then `make generate` before touching handler code.
5. **Verify before claiming done.** Run commands, read output. No "should work".
6. **Commit small.** One logical change per commit, clear message.

## Skills to use

| Area | Skills |
|---|---|
| Any new feature or design | `superpowers:brainstorming`, then `superpowers:writing-plans` |
| Executing a plan | `superpowers:subagent-driven-development` or `superpowers:executing-plans` |
| Any bug or failing test | `superpowers:systematic-debugging` |
| Writing code | `superpowers:test-driven-development` |
| Finishing work | `superpowers:verification-before-completion`, `superpowers:requesting-code-review` |
| Web UI | `frontend-design` |
| iOS | `apple-skills:*` (`swiftui`, `swift`, `swiftdata`, `design`, `testing`, `security`) + `apple:*` for build, test, accessibility, release |

## Boundaries

- Never hand-edit generated code (`backend/internal/api/api.gen.go`, `backend/internal/store/sqlc/`, API clients). Change source, regenerate.
- Never commit secrets, `.env` files, tokens.
- Never run `redocly lint --generate-ignore-file` on project already having `.redocly.lint-ignore.yaml`: overwrites file. Add ignore entries by hand.
- Never change API without updating `openapi.yaml` in same change.
- No SQL outside `backend/internal/store`; no business/sharing rules in handlers.
- Never store computed nutrition; compute on read.
- No features listed as non-goals in spec (section 1) without approved spec change.
- No force-push, no rewriting shared history, no deleting branches you didn't create.

## Definition of done

- Behaviour matches spec; spec + `openapi.yaml` match code.
- New logic has tests that failed before change, pass after.
- `make check` passes, plus package-specific checks for touched package.
- No unrelated changes in diff.
- `CLAUDE.md` / `AGENTS.md` updated if commands, conventions or decisions changed.