# UI Wins: Commits Pagination, PR Filters, Branch Popover, Breadcrumb Cleanup, Latest Commit Bar

**Status:** Approved
**Date:** 2026-05-20
**Owner:** Trey Caliva

## Motivation

Five focused improvements to the repository view that bring it closer to GitHub-grade ergonomics without scope creep:

1. Commits tab loads only the first 50 commits — long histories are inaccessible.
2. Pull request list shows everything mixed together with no way to focus on open/closed/merged.
3. The branch switcher is a bare native `<select>` and feels visually inconsistent with the rest of the app.
4. The breadcrumb redundantly leads with the repo name, which is already in the page header and the URL.
5. There is no "latest commit on this branch" affordance above the file tree, which is the single most-glanced piece of metadata on a Git host home page.

## Scope

In scope:
- All five items above on `frontend/src/pages/Repository.jsx`.
- Two small backend additions: `?offset=` on `GET /api/repos/{owner}/{repo}/commits/{branch}`, and a new `GET /api/repos/{owner}/{repo}/tags` endpoint.
- A new "head commit" endpoint: `GET /api/repos/{owner}/{repo}/refs/{branch}/head`.

Out of scope:
- Creating branches/tags from the popover.
- A dedicated "All branches" page (popover scrolls, search filters; the "View all branches" link is omitted in v1).
- Server-side PR state filtering (purely client-side — counts come from the existing list response).
- Numbered pagination on commits (Load more only).

## Components

### 1. Commits pagination ("Load more")

**Backend** — `internal/api/api.go : GetCommitHistory`:
- Accept `?offset=` query param (int, default 0). Pass to `git log --skip <offset> -n <limit>`.
- Keep existing `?limit=` (default 50). Cap `limit` at e.g. 200 to prevent abuse.
- Response shape unchanged (`[]CommitInfo`).

**Frontend** — Repository.jsx Commits tab:
- State: `commits`, `commitsLoading`, `commitsHasMore` (default true), `commitsOffset` (default 0).
- On branch/tab change: reset to `offset=0`, fetch first 50.
- "Load more" button at the bottom of the list when `commitsHasMore`:
  - Fetches `?offset=commits.length&limit=50`.
  - Appends results.
  - If response length < 50, sets `commitsHasMore = false`.
- Disabled state shows a spinner during fetch.

### 2. PR filter pills (Open / Closed / Merged)

**Frontend only** — `PullRequestList` at Repository.jsx:1210:
- New `prFilter` state, default `'open'`.
- Compute counts in one pass: `counts = { open, closed, merged }`.
- Render three pills above the list:
  - `Open · n` · `Closed · n` · `Merged · n`
  - Active pill: filled accent (`#38bdf8` family, matching existing PR badges).
  - Inactive pill: glass border, hover lightens background.
- Filtered list: `pulls.filter(p => p.status === prFilter)`.
- Empty-state copy adapts: "No open pull requests", "No closed pull requests", "No merged pull requests".
- The total count chip next to the heading stays as `pulls.length` (overall total).

### 3. Branch / tag popover

**Backend** — new endpoint in `internal/api/git.go` (or `api.go`, matching where similar list helpers live):
- `GET /api/repos/{owner}/{repo}/tags` →
  - Auth: same `authorizeGitRead` flow as `GetCommitHistory`.
  - Runs `git --git-dir <path> for-each-ref --sort=-creatordate --format=%(refname:short)|%(objectname) refs/tags`.
  - Returns `[]TagInfo{ Name string, SHA string }`. Empty array if none.
  - Returns `[]` when repo has no commits yet (handle the "fatal: bad default revision" case like commits).
- Register the route alongside the commits route in `internal/api/api.go` under the same auth group.

**Frontend** — new component `BranchTagPicker` (defined inside Repository.jsx, near other helpers, since the rest of the file uses inline components):
- Props: `branches: string[]`, `defaultBranch: string`, `currentBranch: string`, `owner`, `repo`, `onChange(ref, type)` where `type` is `'branch' | 'tag'`.
- Trigger: re-uses existing pill chrome from lines 591–624 (icon + ref name + chevron). Add a small caret to indicate it's interactive.
- Popover (absolute-positioned, opens below trigger, ~360px wide):
  - Header row: `Switch branches/tags` title + close (X) button.
  - Search input ("Find a branch or tag…"). Filters the active list as you type, case-insensitive substring match.
  - Two tabs: `Branches` (default) | `Tags`.
  - List body, scrollable, max-height ~320px:
    - Branches: each row shows the name; current branch gets a leading check icon; default branch gets a trailing `default` pill.
    - Tags: same shape, no `default` pill; loaded lazily on first Tags tab open (cached in component state thereafter).
  - Empty states: "No branches match" / "No tags" / "No tags match".
- Close on: outside click, Escape, or selecting a row.
- Replaces the native `<select>` block at Repository.jsx:601–623. Trigger reuses the existing pill styling so the surrounding layout doesn't shift.

### 4. Breadcrumb cleanup

In `Repository.jsx` around lines 626–656:
- Remove the leading repo-name span (lines 627–633).
- The breadcrumb container should only render when `currentPath` is non-empty or `viewingFile` is set. When at repo root with no file open, render nothing in that slot.
- Path parts remain clickable and styled as today.
- The first path segment should no longer share a `/` separator with a now-removed root.

### 5. Latest commit bar

**Backend** — new endpoint:
- `GET /api/repos/{owner}/{repo}/refs/{branch}/head` →
  - Auth: `authorizeGitRead`.
  - Runs `git --git-dir <path> log -n 1 --format=%H|%an|%ae|%ad|%s <branch>` for the head commit.
  - Runs `git --git-dir <path> rev-list --count <branch>` for the total.
  - Response:
    ```json
    {
      "sha": "dbcbc14…",
      "authorName": "trey",
      "authorEmail": "t@example.com",
      "date": "Mon May 19 …",
      "message": "feat(value): …",
      "totalCommits": 834
    }
    ```
  - Empty repo / missing branch → `{ "sha": "", "totalCommits": 0 }` with HTTP 200, frontend renders nothing.

**Frontend** — new `LatestCommitBar` component on the Code tab only:
- Rendered above the file tree, below the branch picker row.
- Fetches once per branch change, cached in state on the Code tab.
- Layout (left-aligned group, right-aligned commit count):
  - Avatar: 24px circle. URL = `https://www.gravatar.com/avatar/<md5(authorEmail)>?d=identicon&s=48`. Fallback (if image errors) to a circle with first letter of `authorName`.
  - `authorName` (bold).
  - Commit subject (single line, ellipsis on overflow).
  - Short SHA (7 chars, mono).
  - `· <relative time>`.
  - Far right: `<TotalCommits> Commits` as a clickable link that switches `activeTab` to `commits` via the existing `onNavigate('repository', { ..., tab: 'commits' })`.
- Whole bar is a `glass-card` style container matching the file-tree card below it.

**Shared util** — `frontend/src/utils/relativeTime.js`:
- `formatRelative(input: string | number | Date): string`.
- Returns: `"just now"`, `"5 minutes ago"`, `"3 hours ago"`, `"yesterday"`, `"3 days ago"`, `"2 weeks ago"`, `"3 months ago"`, `"2 years ago"`.
- Used by `LatestCommitBar`. (Commits tab keeps its current absolute-date rendering — not in scope.)
- MD5 helper for gravatar lives in the same file or a sibling util.

## Data flow

```
Code tab mount / branch change
  ├─ fetch /api/repos/.../refs/<branch>/head   → LatestCommitBar
  └─ fetch /api/repos/.../tree/<branch>/<path> → file tree (existing)

Commits tab mount / branch change
  └─ fetch /api/repos/.../commits/<branch>?limit=50&offset=0
     └─ on "Load more": refetch with offset=commits.length

Pulls tab mount
  └─ fetch /api/repos/.../pulls                → client-side filter by prFilter

Branch picker open
  ├─ Branches tab: use in-memory meta.branches (already loaded)
  └─ Tags tab (lazy, first open only): fetch /api/repos/.../tags
```

## Error handling

- Each fetch surfaces failures into a local `error` state, rendered in the existing `error-box` style. None of the new endpoints should crash the page on 4xx/5xx.
- `LatestCommitBar` silently hides on empty response (`sha === ""`) — empty repos shouldn't show a placeholder row.
- Tags endpoint returning `[]` shows the "No tags" empty state, not an error.
- "Load more" failure leaves already-loaded commits intact and shows the error above the button.

## Testing

Backend (Go):
- `internal/api/api_test.go` (or new `git_test.go`) — `TestGetCommitHistory_Offset` verifying `?offset=` returns the expected slice from a seeded local repo.
- `TestListTags` — empty case + populated case.
- `TestGetBranchHead` — populated branch + empty repo.

Frontend (manual, since the project has no React test harness today):
- Smoke matrix documented in PR description:
  - Repo with >100 commits: pagination loads next pages, hides button at the end.
  - Repo with mixed PR statuses: each pill filters correctly; counts match.
  - Branch popover: search filters live, Tags tab loads, current+default badges render.
  - Breadcrumb: root view shows none; nested paths render as before minus the leading repo segment.
  - Latest commit bar: shows on Code tab, hidden on empty repo, "N Commits" jumps to Commits tab.

## File touch list

New:
- `internal/api/` — handlers for `ListTags` and `GetBranchHead` (likely in `git.go` for proximity to `GetCommitHistory`).
- `frontend/src/utils/relativeTime.js`.
- `docs/superpowers/specs/2026-05-20-ui-wins-design.md` (this file).

Modified:
- `internal/api/api.go` — `GetCommitHistory` (offset param), route registrations for new endpoints.
- `frontend/src/pages/Repository.jsx` — Code tab branch row, breadcrumb, Commits tab body, `PullRequestList`, new `BranchTagPicker` + `LatestCommitBar` components.
- `frontend/src/index.css` — popover and pill styles if not expressible inline.

## Open follow-ups (explicitly deferred)

- "View all branches" linking to a dedicated `/branches` page.
- Create-branch UX inside the picker.
- Server-side PR filter once the dataset gets large enough that client-side filtering wastes payload.
- Numbered/cursor pagination on commits if "Load more" becomes a friction point.
