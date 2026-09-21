# Agent Guide

Tool-neutral guidance for any coding agent working in this repo. `CLAUDE.md` holds the project facts and commands; this file holds how to work.

## Workflow

1. **Spec first.** Non-trivial work traces to the design spec in `docs/superpowers/specs/`. If behaviour is not specified, propose a spec change before coding.
2. **Plan.** Work is executed from a plan in `docs/superpowers/plans/`, task by task.
3. **Tests first.** Write the failing test, watch it fail, write the minimum code, watch it pass. Applies to all service logic and every bug fix.
4. **Contract first.** If an API shape changes, edit `openapi.yaml`, run `make lint-api`, then `make generate` before touching handler code.
5. **Verify before claiming done.** Run the commands and read the output. No "should work".
6. **Commit small.** One logical change per commit, with a clear message.

## Skills to use

| Area | Skills |
|---|---|
| Any new feature or design | `superpowers:brainstorming`, then `superpowers:writing-plans` |
| Executing a plan | `superpowers:subagent-driven-development` or `superpowers:executing-plans` |
| Any bug or failing test | `superpowers:systematic-debugging` |
| Writing code | `superpowers:test-driven-development` |
| Finishing work | `superpowers:verification-before-completion`, `superpowers:requesting-code-review` |
| Web UI | `frontend-design` |
| iOS | `apple-skills:*` (`swiftui`, `swift`, `swiftdata`, `design`, `testing`, `security`) and `apple:*` for build, test, accessibility and release |

## Boundaries

- Never hand-edit generated code (`backend/internal/api/api.gen.go`, `backend/internal/store/sqlc/`, API clients). Change the source and regenerate.
- Never commit secrets, `.env` files or tokens.
- Never run `redocly lint --generate-ignore-file` on a project that already has `.redocly.lint-ignore.yaml`: it overwrites the file. Add ignore entries by hand.
- Never change the API without updating `openapi.yaml` in the same change.
- Never put SQL outside `backend/internal/store`, and never put business or sharing rules in handlers.
- Never store computed nutrition; compute it on read.
- Do not add features listed as non-goals in the spec (section 1) without an approved spec change.
- Do not force-push, rewrite shared history, or delete branches you did not create.

## Definition of done

- The behaviour matches the spec, and the spec and `openapi.yaml` match the code.
- New logic has tests that failed before the change and pass after.
- `make check` passes, and package-specific checks pass for the package touched.
- No unrelated changes in the diff.
- `CLAUDE.md` / `AGENTS.md` updated if commands, conventions or decisions changed.
