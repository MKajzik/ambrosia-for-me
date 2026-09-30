# Handoff: iOS app brainstorm (spec not written yet)

## State
- Repo `/home/kazik/mealPlanner` (GitHub MKajzik/ambrosia-for-me), `master` at `d96538b`. All four web plans are merged (#10 to #13), worktrees and branches cleaned up. Only untracked file: `.mcp.json` (the user's, leave it).
- `ios/` does not exist. No iOS spec, plan or code yet. Nothing is committed for iOS.
- I am mid-**superpowers:brainstorming**, path classified **architectural** (new subsystem). Checklist position: context explored, clarifying questions in progress (2 answered, a 3rd was rejected for clarification). No design approved, no spec written. HARD-GATE: no code or scaffolding before the written spec is approved, then writing-plans.

## Decisions so far (from the user)
1. **Build env:** a Mac is available, sharing the repo via git. This box is WSL2/Linux and cannot compile Swift. So plan tasks need exact `xcodebuild`/`swift test` commands, and execution happens on the Mac (a Mac-side Claude Code session or the user pasting results). Write the spec/plans here, design tasks so they are verifiable on the Mac.
2. **Decomposition approved as proposed:** one brainstorm and spec for the whole iOS app, then separate plans in this order: (1) Foundation (Xcode project, generated client, Keychain auth + refresh, email sign-in, tab shell, macOS CI), (2) Meals (ingredient search, meal library and editor, partner meals), (3) Plan and Today (rings, week view, diet templates), (4) Shopping and Profile (lists, offline queue with SwiftData, SSE live updates, targets, partner link, account deletion), (5) Sign in with Apple last (needs backend `/auth/apple`, which is not in `openapi.yaml` yet, plus Apple Developer setup).

## Open question the user wanted to clarify
I asked: "How should the Xcode project be structured?" with options (a) **XcodeGen + local Swift package** (my recommendation: `project.yml` generates a thin app target, no hand-edited `.pbxproj`; almost all code in a local package built and tested with `swift test`/`xcodebuild`), (b) plain Xcode project, (c) Tuist. The user rejected the question and said they want to clarify it. I replied asking what to clarify; **no answer yet**. Start the next session by asking what was unclear (or rephrase simpler: why it matters is that I cannot edit `.pbxproj` reliably and cannot compile here), then continue.

## Remaining brainstorm topics (one question per message)
- Project structure (above); cache breadth (parent spec: SwiftData makes launches instant, shopping list is offline-critical; decide whether other areas are cached or online-only); offline queue semantics (spec §4.3: intents check/uncheck/add/remove replayed in order, check is last-write-wins); SSE on iOS (URLSession stream, refetch on foreground/reconnect); Swift OpenAPI Generator vs the spec's locked choice (openapi is 3.0.3, fine); design/visual direction (Liquid Glass, SF Symbols, `apple-skills:design`); testing split (Swift Testing for view models/repositories, XCUITest for sign-in, meal editing, shopping check-off, `apple:accessibility` audit); CI on a macOS runner; `ios/CLAUDE.md` content.
- Then: propose 2-3 approaches, present design in sections, write spec to `docs/superpowers/specs/<date>-ios-app-design.md` (note: environment date is 2026-09-30, earlier plans use dates up to 2026-10-02), self-review, user reviews, then `superpowers:writing-plans` for plan 1.
- Offer the visual companion only if a real mockup question arises (its own message).

## Where the facts live (do not re-derive)
- Parent spec: `docs/superpowers/specs/2026-09-21-meal-planner-design.md` (§2.5 iOS, §4.3 shopping sync, §5.2 visual direction, §5.4 iOS specifics, §6 testing, §7 delivery, §9 order).
- Web spec as the parity reference: `docs/superpowers/specs/2026-09-25-web-app-design.md`. Web plans in `docs/superpowers/plans/` show the plan style and the recurring defect classes.
- Contract: `openapi.yaml` (3.0.3, source of truth). Agent workflow and boundaries: `AGENTS.md`. Project facts: `CLAUDE.md`. Behaviours iOS should mirror from web (optimistic check-off, 409 `version_conflict` with `current`, SSE rules: ignore event at or below cached version, refetch on gap/`list_changed`/reopen, 404 means access lost) are recorded in `web/CLAUDE.md` Gotchas and the web spec.
- Known API gaps (fix spec-first when they bite): no "custom only" filter on `GET /ingredients`; no entry-addressed plan endpoint (snacks cannot be edited singly).

## Suggested skills
- `superpowers:brainstorming` (already in progress; resume it, do not restart), then `superpowers:writing-plans`.
- For iOS content: `apple-skills:swiftui`, `apple-skills:swift`, `apple-skills:swiftdata`, `apple-skills:design`, `apple-skills:testing`, and `apple:*` (`apple:plan`, `apple:build`, `apple:testflight`, `apple:accessibility`) as AGENTS.md names them.
- Execution later on the Mac: `superpowers:executing-plans` or `superpowers:subagent-driven-development`, `superpowers:using-git-worktrees`, `superpowers:verification-before-completion`, `superpowers:finishing-a-development-branch`.

## User preferences observed
- Short updates, one clear question plus a recommendation at each step, PR-per-plan flow, git worktrees (`.claude/worktrees/feat+<name>`), Native execution for plans 2 to 4. Confirms irreversible cleanup explicitly. Global instructions say command output is condensed by `rtk` (use `rtk proxy <cmd>` if a result looks truncated or empty). Bash auto-mode classifier sometimes answers "no verdict": retry once.
