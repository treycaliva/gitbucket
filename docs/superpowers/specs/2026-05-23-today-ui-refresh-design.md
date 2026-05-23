# Design · GitBucket "Shippable Today" UI refresh

**Date:** 2026-05-23
**Status:** Approved (approach A)
**Source of truth for visuals:** `design_handoff_today_v1/` (per-mock specs `00`–`06`, references in `references/`). This document is the *integration strategy*; the handoff is the *visual contract*.

## Goal

Refresh five surfaces of the existing React SPA using **only data the current backend already returns** (traced in `design_handoff_today_v1/06-api-contracts.md`). No new endpoints, entities, aggregations, or dependencies. Aesthetic: keep the dark/cyan/Outfit+JetBrains-Mono family but tighten it — solid surfaces instead of glass, hairline rules, monospace promoted for technical content, softened cyan (`#38bdf8`→`#5fc7f5`), warmer page background (`#06080d`→`#0a0d14`).

## Chosen approach (A): Foundation-first, primitives as components

Add the `--gb-*` token set to `index.css` *alongside* the existing `--bg-*`/`--accent`/`--text-*` tokens so the two systems coexist during migration. Build reusable primitive components, then implement surfaces in order, each leaning on those primitives. Rejected: surface-first (risks divergent chip/card treatments) and wholesale `gb-*` class port (handoff forbids it; bloats global CSS).

## Constraints (must hold for every commit)

- **No new dependencies.** `react@18`, `lucide-react`, `vite` only. Outfit + JetBrains Mono are already imported in `index.css`.
- **All data via `apiClient.get/post/delete/patch`.** No raw fetch.
- **`authService` and `DEV_MODE` mock-token support untouched.**
- **Backward-compatible URLs.** Existing paths keep working. Insights = `/:owner/:repo?tab=insights`; Profile = `/u/:username`.
- **Preserve the SPA fallback guard** in `main.go` (excludes `/api/` and `/r/`). No backend routing changes are needed for this track, but do not regress that guard if touched.
- **No new motion.** Remove the `.hoverable` `translateY(-4px)` lift during the Dashboard refactor.

## Architecture / integration points

**Router (`frontend/src/App.jsx`)** — custom switch-based router. Three functions change for Profile:
- `parseLocation()`: add a `/u/:username` → `{ page: 'profile', params: { username } }` branch.
- `navigate()`: add a `profile` → `/u/${username}` URL builder.
- `renderView()`: add a `case 'profile'` rendering `<Profile>`.
Insights needs **no router change** — `tab` already flows from `searchParams.get('tab')` into `<Repository initialTab=...>`.

**Tokens (`frontend/src/index.css`)** — add `--gb-*` custom properties from `design_handoff_today_v1/references/tokens.css`. Additive only; do not delete existing tokens in this track.

**Primitive components (`frontend/src/components/`)** — new files:
- `Avatar.jsx` — initials in a deterministic 2-color gradient circle. Build on the existing `utils/avatarColor.js` hash rather than duplicating it.
- `Chip.jsx` — `.gb-chip` pill with variants `default | accent | ok | warn | err | merged`.
- `Card.jsx` — solid surface, `1px solid var(--gb-line)`, 10px radius, no shadow/blur.
- `SectionHead.jsx` — mono-uppercase kicker + title + optional right slot.
`Dot` and `Icon` are inline / `lucide-react` direct per the handoff.

## Build sequence (branch `feat/today-ui-refresh`, one commit per chunk)

1. **Foundation** — `--gb-*` tokens + the four primitives. Nothing renders differently. Spec: `00-design-tokens.md`.
2. **Dashboard** (`pages/Dashboard.jsx`) — pinned + recently-visited via localStorage, "Waiting on you" panel (N+1 over `/pulls`), per-row open-PR count, client-side sort/filter; remove `.hoverable` lift. Spec: `01-dashboard.md`.
3. **Code tab** (`pages/Repository.jsx`, Code branch) — CODEOWNERS chips per tree row, file-mix sidebar card (client-side from tree), Branches + Tags rails. Spec: `02-code-tab.md`.
4. **Insights tab** (new `?tab=insights` in `pages/Repository.jsx`) — stat strip (commits/branches/tags/open PRs/collaborators/protection rules), recent commits, collaborators panel, branch-protection summary, CODEOWNERS map. Spec: `03-insights-tab.md`.
5. **PR detail** (`PullRequestDetail` in `pages/Repository.jsx`) — reviewer status rail, approvals progress, build status callout, commits list. Spec: `04-pr-detail.md`.
6. **Profile** (new `pages/Profile.jsx` + `/u/:username` route) — avatar, username, email, owner-filtered repo list, plus a "what we can't show yet" panel. Spec: `05-profile.md`.

## Data dependencies (sanity-checked against `06-api-contracts.md`)

Every visual element traces to an existing endpoint. Client-side derivation (file-mix from tree, open-PR counts via N+1 over `/pulls`, recently-visited via localStorage) is explicitly allowed and adds no backend work.

## Verification

After each surface (steps 2–6): run the Vite dev server with `DEV_MODE=true` mock auth, drive a browser, and capture a screenshot of the surface for the user to confirm *before* committing that chunk. Also `npm run lint` and `npm run build` must pass before each commit. The foundation step (1) is verified by build/lint only (nothing renders differently) plus a smoke check that existing pages are unaffected.

## Out of scope (explicitly NOT this track)

- Full visual rewrite of Tokens, Commits, BuildLogs, Apps pages — they keep current treatment.
- "Full vision" features needing backend work: stars, contributor stats, language detection, activity feed, profile heatmap.
- Routing rewrite. Only the additive `/u/:username` route and `?tab=insights` segment.
- Deprecating the old `--bg-*`/`--accent`/`--text-*` tokens (a later cleanup once all five land).

## Risks

- **Repository.jsx is the heavy file** — it hosts Code, Insights, and PR detail. Three of six commits touch it. Keep each surface's additions cohesive; consider extracting the new Insights tab body and PR-detail sub-panels into local components within the file to keep it readable.
- **N+1 over `/pulls`** for open-PR counts and "Waiting on you" — bounded by repo/PR count; acceptable for this track but keep calls parallel and tolerant of partial failure.
