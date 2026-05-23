# "Shippable Today" UI Refresh Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refresh five surfaces of the GitBucket React SPA (Dashboard, Code tab, Insights tab, PR detail, Profile) to the tightened dark/cyan editorial direction, using only data the current backend already returns.

**Architecture:** Foundation-first. Add a `--gb-*` design-token set to `index.css` alongside the existing tokens (they coexist) and build four reusable primitive components (`Avatar`, `Chip`, `Card`, `SectionHead`). Then implement each surface as its own commit, leaning on those primitives. The custom switch-based router in `App.jsx` gains one new route (`/u/:username`); Insights reuses the existing `?tab=` plumbing.

**Tech Stack:** React 19, `lucide-react` ^1.16, Vite 8. No test runner — verification per task is `npm run lint` + `npm run build` (both from `frontend/`) plus a browser screenshot of the surface under `DEV_MODE` mock auth. No new dependencies.

**Visual contract:** `design_handoff_today_v1/` per-mock specs (`00`–`06`) and `references/` mocks are the source of truth for layout, density, and JSX shape. This plan references them by section rather than reproducing every pixel.

**Conventions for every task:**
- All data fetching goes through `apiClient.get/post/delete/patch` (see `frontend/src/apiClient.js`). No raw `fetch`.
- Do not touch `authService` or break `DEV_MODE` `mock_<uid>` token support.
- Prefer the primitive components over re-implementing chips/cards inline.
- Run `npm run lint && npm run build` from `frontend/` before each commit; both must pass.
- Existing URLs must keep working.

---

### Task 1: Foundation — design tokens + primitive components

**Files:**
- Modify: `frontend/src/index.css` (`:root` block, ~line 3-37; append primitive classes after the existing primitives)
- Create: `frontend/src/components/Avatar.jsx`
- Create: `frontend/src/components/Chip.jsx`
- Create: `frontend/src/components/Card.jsx`
- Create: `frontend/src/components/SectionHead.jsx`

- [ ] **Step 1: Add `--gb-*` tokens to `index.css`**

Inside the existing `:root { … }` in `frontend/src/index.css`, after the existing `--warning` line and before `--font-sans`, add (these are additive — do NOT remove existing `--bg-*`/`--accent`/`--text-*` tokens). **Do NOT copy the `@import` font line from `references/tokens.css`** — `index.css:1` already imports Outfit + JetBrains Mono; a second import is redundant and uses a narrower weight range.

```css
  /* --- Refresh tokens (gb-*), coexist with the above during migration --- */
  --gb-page:        #0a0d14;
  --gb-surface:     #0f131c;
  --gb-surface-2:   #141926;
  --gb-surface-3:   #1b2030;
  --gb-hover:       rgba(255,255,255,0.035);

  --gb-line:        rgba(255,255,255,0.06);
  --gb-line-strong: rgba(255,255,255,0.11);
  --gb-line-accent: rgba(95,199,245,0.30);

  --gb-fg:          #e8edf4;
  --gb-fg-2:        #b6bec9;
  --gb-fg-3:        #8993a2;
  --gb-fg-4:        #5d6678;

  --gb-accent:      #5fc7f5;
  --gb-accent-2:    #8fd7f7;
  --gb-accent-dim:  rgba(95,199,245,0.14);
  --gb-accent-bg:   rgba(95,199,245,0.07);

  --gb-ok:          #4ade80;
  --gb-ok-dim:      rgba(74,222,128,0.13);
  --gb-warn:        #fbbf24;
  --gb-warn-dim:    rgba(251,191,36,0.13);
  --gb-err:         #f87171;
  --gb-err-dim:     rgba(248,113,113,0.13);
  --gb-merged:      #a78bfa;
  --gb-merged-dim:  rgba(167,139,250,0.13);

  --gb-sans: var(--font-sans);
  --gb-mono: var(--font-mono);
```

- [ ] **Step 2: Add primitive CSS classes to `index.css`**

Append to the end of `frontend/src/index.css` (these are the curated subset of the handoff's `gb-*` classes that the primitives use — not the whole canvas system):

```css
/* ===== Refresh primitives ===== */
.gb-card {
  background: var(--gb-surface);
  border: 1px solid var(--gb-line);
  border-radius: 10px;
}

.gb-chip {
  display: inline-flex; align-items: center; gap: 5px;
  font-size: 11.5px;
  padding: 2px 8px;
  border-radius: 999px;
  border: 1px solid var(--gb-line);
  color: var(--gb-fg-2);
  font-weight: 500;
  line-height: 1;
  height: 20px;
}
.gb-chip.dot::before { content: ''; width: 6px; height: 6px; border-radius: 50%; background: currentColor; }
.gb-chip.accent { color: var(--gb-accent); border-color: var(--gb-line-accent); background: var(--gb-accent-bg); }
.gb-chip.ok     { color: var(--gb-ok);     border-color: rgba(74,222,128,0.25);  background: var(--gb-ok-dim); }
.gb-chip.warn   { color: var(--gb-warn);   border-color: rgba(251,191,36,0.25);  background: var(--gb-warn-dim); }
.gb-chip.err    { color: var(--gb-err);    border-color: rgba(248,113,113,0.25); background: var(--gb-err-dim); }
.gb-chip.merged { color: var(--gb-merged); border-color: rgba(167,139,250,0.25); background: var(--gb-merged-dim); }

.gb-avatar {
  display: inline-grid; place-items: center;
  border-radius: 50%;
  font-weight: 600;
  font-family: var(--gb-mono);
  color: #0a0d14;
  flex-shrink: 0;
}

.gb-section-title {
  display: flex; align-items: center; gap: 9px;
  font-size: 13px; font-weight: 600; color: var(--gb-fg);
  margin-bottom: 12px;
}
.gb-section-title .kicker {
  font-family: var(--gb-mono);
  font-size: 10.5px; font-weight: 500;
  color: var(--gb-fg-4);
  letter-spacing: 0.04em; text-transform: uppercase;
}
.gb-section-title .right { margin-left: auto; font-size: 12px; color: var(--gb-fg-3); font-weight: 500; }
```

- [ ] **Step 3: Create `Card.jsx`**

```jsx
// frontend/src/components/Card.jsx
export default function Card({ children, className = '', style, ...rest }) {
  return (
    <div className={`gb-card ${className}`.trim()} style={style} {...rest}>
      {children}
    </div>
  );
}
```

- [ ] **Step 4: Create `Chip.jsx`**

`variant` is one of `default | accent | ok | warn | err | merged`. `icon` is an optional node (e.g. a `lucide-react` icon element) rendered before the children. `dot` renders the leading `::before` dot.

```jsx
// frontend/src/components/Chip.jsx
export default function Chip({ variant = 'default', icon = null, dot = false, children, className = '', ...rest }) {
  const cls = ['gb-chip', variant !== 'default' ? variant : '', dot ? 'dot' : '', className]
    .filter(Boolean)
    .join(' ');
  return (
    <span className={cls} {...rest}>
      {icon}
      {children}
    </span>
  );
}
```

- [ ] **Step 5: Create `SectionHead.jsx`**

```jsx
// frontend/src/components/SectionHead.jsx
export default function SectionHead({ kicker, title, right }) {
  return (
    <div className="gb-section-title">
      {kicker && <span className="kicker">{kicker}</span>}
      <span>{title}</span>
      {right && <span className="right">{right}</span>}
    </div>
  );
}
```

- [ ] **Step 6: Create `Avatar.jsx`** (reuses the existing `initials()` hash from `utils/avatarColor.js`; gradient palette matches the handoff `references/components.jsx`)

```jsx
// frontend/src/components/Avatar.jsx
import { initials } from '../utils/avatarColor';

const PALETTES = [
  ['#8fd7f7', '#b394ea'],
  ['#a7f3d0', '#67e8f9'],
  ['#fde68a', '#fca5a5'],
  ['#c4b5fd', '#fbcfe8'],
  ['#86efac', '#fde68a'],
  ['#fda4af', '#c4b5fd'],
  ['#7dd3fc', '#a78bfa'],
  ['#f9a8d4', '#fdba74'],
];

export default function Avatar({ name, size = 22, style, ...rest }) {
  const seed = name || '?';
  const idx = seed.split('').reduce((a, c) => a + c.charCodeAt(0), 0) % PALETTES.length;
  const [a, b] = PALETTES[idx];
  return (
    <span
      className="gb-avatar"
      style={{
        width: size,
        height: size,
        background: `linear-gradient(140deg, ${a}, ${b})`,
        fontSize: size <= 18 ? 8 : size <= 24 ? 10 : 11,
        ...style,
      }}
      {...rest}
    >
      {initials(name)}
    </span>
  );
}
```

- [ ] **Step 7: Lint and build**

Run from `frontend/`:
```bash
npm run lint && npm run build
```
Expected: both pass with no errors. Nothing in the running app renders differently yet (no existing component imports these primitives).

- [ ] **Step 8: Commit**

```bash
git add frontend/src/index.css frontend/src/components/Avatar.jsx frontend/src/components/Chip.jsx frontend/src/components/Card.jsx frontend/src/components/SectionHead.jsx
git commit -m "feat(ui): add gb-* design tokens and primitive components

Foundation for the 'shippable today' refresh. Adds --gb-* tokens to
index.css alongside existing tokens, plus Avatar/Chip/Card/SectionHead
primitives. No surface renders differently yet.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Dashboard refresh

**Files:**
- Modify: `frontend/src/pages/Dashboard.jsx` — full rewrite of the render body and data layer. Today's file is 305 lines: imports (L1-3), repo/modal state (L6-17), `loadRepos`/`useEffect` (L19-36), `handleCreateRepo` (L38-64), `filteredRepos` filter (L66-74), the `page-header` hero (L78-89), the top full-width search bar (L91-113), error box (L115-126), the `loading / empty / glass-card grid` block (L128-214), and the Create Repository modal (L216-302). **Keep `handleCreateRepo` and the entire modal (L216-302) verbatim** — out of scope per spec §"What's removed". Replace L78-214 (hero + search + grid) with the new layout. Add the new data layer (open-PR counts, waiting-on-you, pinned/recent helpers) around the existing `loadRepos`.
- Add (top of `Dashboard.jsx`, module scope, not a new file): `localStorage` helpers `readSlugs`, `writePins` and a `slugOf(repo)` / `relTime(repo)` helper. No new files; this task only *reads* `gitbucket.recent` (recent-list maintenance lives in `Repository.jsx`, a later concern).

- [ ] **Step 1: Swap imports.** Replace the lucide import line (L3) and add primitive imports. New top block:
  ```jsx
  import { useState, useEffect, useMemo, useCallback } from 'react';
  import { apiClient } from '../apiClient';
  import { Plus, Globe, Lock, Search, History, GitPullRequest, Pin, Folder } from 'lucide-react';
  import Card from '../components/Card';
  import Chip from '../components/Chip';
  import SectionHead from '../components/SectionHead';
  ```
  (`Chip` may be unused after the rewrite — drop it from the import if lint flags it. `Folder` stays for the no-repos empty state.)

- [ ] **Step 2: Add module-scope helpers** above `export default function Dashboard`. These centralize the `localStorage` contract (`gitbucket.pinned` / `gitbucket.recent` = JSON arrays of `"owner/repo"` slugs) and a relative-time formatter for the Firestore `{seconds}` timestamp shape this codebase uses:
  ```jsx
  const PIN_KEY = 'gitbucket.pinned';
  const RECENT_KEY = 'gitbucket.recent';
  const MAX_PINS = 6;

  const slugOf = (r) => `${r.owner}/${r.name}`;

  function readSlugs(key) {
    try {
      const v = JSON.parse(localStorage.getItem(key) || '[]');
      return Array.isArray(v) ? v.filter((s) => typeof s === 'string') : [];
    } catch {
      return [];
    }
  }
  function writePins(slugs) {
    localStorage.setItem(PIN_KEY, JSON.stringify(slugs.slice(0, MAX_PINS)));
  }

  // Firestore timestamps arrive as { seconds } (see existing updatedAt usage).
  function tsOf(r) {
    const u = r.updatedAt;
    if (u && typeof u.seconds === 'number') return u.seconds * 1000;
    if (typeof u === 'string') { const t = Date.parse(u); return Number.isNaN(t) ? 0 : t; }
    return 0;
  }
  function relTime(r) {
    const ms = tsOf(r);
    if (!ms) return 'recently';
    const s = Math.floor((Date.now() - ms) / 1000);
    if (s < 60) return 'just now';
    const m = Math.floor(s / 60); if (m < 60) return `${m}m ago`;
    const h = Math.floor(m / 60); if (h < 24) return `${h}h ago`;
    const d = Math.floor(h / 24); if (d < 7) return `${d}d ago`;
    const w = Math.floor(d / 7); if (w < 5) return `${w}w ago`;
    const mo = Math.floor(d / 30); if (mo < 12) return `${mo}mo ago`;
    return `${Math.floor(d / 365)}y ago`;
  }
  ```

- [ ] **Step 3: Add the new state.** Below the existing `repos`/`loading`/`error` state (after L9), add (keep `search` from L9):
  ```jsx
  const [openPRCount, setOpenPRCount] = useState({}); // { "owner/repo": number | '?' }
  const [waitingOnMe, setWaitingOnMe] = useState([]);
  const [pinned, setPinned] = useState(() => readSlugs(PIN_KEY));
  const [recent] = useState(() => readSlugs(RECENT_KEY));
  const [typeFilter, setTypeFilter] = useState('all');
  const [sort, setSort] = useState('updated');
  ```

- [ ] **Step 4: Add the N+1 `/pulls` loader** as a `useCallback`, placed after `loadRepos` (after L30). One `?status=open` call per repo, fully parallel, partial-failure tolerant (failed repo → `'?'` count, excluded from waiting set). It fills the per-repo open-PR count map and derives the waiting-on-you list in one pass:
  ```jsx
  const loadPullsAndWaiting = useCallback(async (repoList) => {
    if (!repoList.length) { setOpenPRCount({}); setWaitingOnMe([]); return; }
    const results = await Promise.all(
      repoList.map((r) =>
        apiClient
          .get(`/api/repos/${r.owner}/${r.name}/pulls`)
          .then((pulls) => ({ r, pulls: Array.isArray(pulls) ? pulls : [], ok: true }))
          .catch(() => ({ r, pulls: [], ok: false }))
      )
    );
    const counts = {};
    const waiting = [];
    const me = user?.username;
    for (const { r, pulls, ok } of results) {
      const slug = slugOf(r);
      // The /pulls list returns ALL pull requests (the backend ignores ?status),
      // so narrow to open ones client-side.
      const openPulls = pulls.filter((p) => p.status === 'open');
      counts[slug] = ok ? openPulls.length : '?';
      if (!ok || !me) continue;
      for (const p of openPulls) {
        // "Waiting on you" = open PRs where you're a requested reviewer.
        // (No failed-check branch: the PR list object has no build/check status
        // field — overallStatus lives only on commit objects, not PRs.)
        if ((p.requestedReviewers || []).includes(me)) {
          waiting.push({ ...p, owner: r.owner, repo: r.name });
        }
      }
    }
    setOpenPRCount(counts);
    setWaitingOnMe(waiting);
  }, [user]);
  ```
  Validation confirmed: `/pulls` items carry `status` (`open|merged|closed`), `requestedReviewers`, and `authorUsername`; there is **no** `checkStatus`/build field on PR list items, so the failed-author idea was dropped.

- [ ] **Step 5: Trigger the pulls loader after repos load.** In `loadRepos` (L19-30), after `setRepos(data)`:
  ```jsx
  const data = await apiClient.get('/api/repos');
  setRepos(data);
  loadPullsAndWaiting(data); // fire-and-forget; tolerates partial failure internally
  ```
  Leave the rest of `loadRepos` and its `useEffect` (L32-36) unchanged. Counts are cached in state and never refetched on filter/sort change.

- [ ] **Step 6: Add the pin toggle handler.** Place after `loadPullsAndWaiting`:
  ```jsx
  const togglePin = useCallback((slug, e) => {
    e?.stopPropagation();
    e?.preventDefault();
    setPinned((prev) => {
      const next = prev.includes(slug)
        ? prev.filter((s) => s !== slug)
        : [...prev, slug].slice(0, MAX_PINS);
      writePins(next);
      return next;
    });
  }, []);
  ```

- [ ] **Step 7: Derive the pinned/recent repo objects** (intersect persisted slugs with the live repo list so deleted pins never render). Add as `useMemo`s:
  ```jsx
  const repoBySlug = useMemo(() => {
    const m = new Map();
    for (const r of repos) m.set(slugOf(r), r);
    return m;
  }, [repos]);

  const pinnedRepos = useMemo(
    () => pinned.map((s) => repoBySlug.get(s)).filter(Boolean),
    [pinned, repoBySlug]
  );

  const recentRepos = useMemo(
    () => recent.map((s) => repoBySlug.get(s)).filter(Boolean).slice(0, 5),
    [recent, repoBySlug]
  );
  ```

- [ ] **Step 8: Replace the old `filteredRepos` filter (L66-74)** with the client-side filter+sort `useMemo`. Uses `toSorted` (no new deps), reads counts from cached `openPRCount`, treats `'?'` as 0 for sort:
  ```jsx
  const filtered = useMemo(() => {
    const q = search.toLowerCase().trim();
    let rows = repos.filter(
      (r) =>
        (typeFilter === 'all' || r.visibility === typeFilter) &&
        (!q ||
          r.name.toLowerCase().includes(q) ||
          r.owner.toLowerCase().includes(q) ||
          (r.description || '').toLowerCase().includes(q))
    );
    const prCount = (r) => {
      const n = openPRCount[slugOf(r)];
      return typeof n === 'number' ? n : 0;
    };
    switch (sort) {
      case 'updated':   rows = rows.toSorted((a, b) => tsOf(b) - tsOf(a)); break;
      case 'name-asc':  rows = rows.toSorted((a, b) => a.name.localeCompare(b.name)); break;
      case 'name-desc': rows = rows.toSorted((a, b) => b.name.localeCompare(a.name)); break;
      case 'open-prs':  rows = rows.toSorted((a, b) => prCount(b) - prCount(a)); break;
      default: break;
    }
    return rows;
  }, [repos, search, typeFilter, sort, openPRCount]);
  ```

- [ ] **Step 9: Replace the hero block (L78-89)** with the tight kicker + greeting + status line, built from real state. Insert at the top of the returned JSX (the outer `<div>` and the trailing modal stay):
  ```jsx
  const today = new Date().toLocaleDateString('en-US', {
    weekday: 'short', month: 'short', day: 'numeric', year: 'numeric',
  }).toUpperCase();
  const hour = new Date().getHours();
  const greet = hour < 12 ? 'Morning' : hour < 18 ? 'Afternoon' : 'Evening';
  // waitingOnMe already contains only open PRs where you're a requested reviewer.
  const reviewCount = waitingOnMe.length;
  ```
  ```jsx
  <header style={{ display: 'flex', alignItems: 'flex-end', justifyContent: 'space-between', marginBottom: 22 }}>
    <div>
      <div style={{ fontFamily: 'var(--gb-mono)', fontSize: 10.5, color: 'var(--gb-fg-4)', letterSpacing: '0.05em' }}>{today}</div>
      <h1 style={{ fontSize: 24, fontWeight: 500, letterSpacing: '-0.02em', margin: '2px 0 0' }}>
        {greet}, <span style={{ color: 'var(--gb-accent)' }}>{user?.username}</span>.
      </h1>
      <p style={{ color: 'var(--gb-fg-3)', fontSize: 13, marginTop: 4 }}>
        {reviewCount > 0
          ? <><span style={{ color: 'var(--gb-fg)', fontWeight: 600 }}>{reviewCount} review{reviewCount === 1 ? '' : 's'}</span> waiting on you.</>
          : <>Nothing waiting on you.</>}
      </p>
    </div>
    <button className="btn btn-primary" onClick={() => setShowModal(true)}>
      <Plus size={16} /> New repository
    </button>
  </header>
  ```

- [ ] **Step 10: Replace the top search bar (L91-113), error box (L115-126), and the loading/empty/grid block (L128-214)** with the two-column grid shell. Keep the `loading` spinner and the no-repos centered CTA; the error box moves above the filter row. Outer shape:
  ```jsx
  {loading ? (
    <div className="loader-container"><div className="loader" /></div>
  ) : repos.length === 0 && !error ? (
    <Card style={{ textAlign: 'center', padding: '4rem 2rem', borderStyle: 'dashed' }}>
      <Folder size={48} style={{ color: 'var(--gb-fg-4)', marginBottom: '1rem' }} />
      <h3 style={{ color: 'var(--gb-fg)', marginBottom: '0.5rem' }}>No repositories yet</h3>
      <p style={{ marginBottom: '1.5rem', fontSize: '0.95rem', color: 'var(--gb-fg-3)' }}>Get started by creating your first repository.</p>
      <button className="btn btn-primary" onClick={() => setShowModal(true)}><Plus size={18} /> Create Repository</button>
    </Card>
  ) : (
    <div style={{ display: 'grid', gridTemplateColumns: '1fr 320px', gap: 22 }}>
      <div>{/* LEFT: Step 11 pinned + Step 12 list */}</div>
      <aside style={{ display: 'flex', flexDirection: 'column', gap: 22 }}>{/* RIGHT: Step 13 waiting + Step 14 recent */}</aside>
    </div>
  )}
  ```
  The error box (rendered above the grid, inside the non-loading branch):
  ```jsx
  {error && (
    <Card style={{ borderColor: 'var(--gb-err-dim)', color: 'var(--gb-err)', padding: '1rem', marginBottom: 16 }}>{error}</Card>
  )}
  ```

- [ ] **Step 11: Build the Pinned grid** (left column, first child). Empty state = single dashed Card. Each card: 14px padding, 8px child gap; title `owner / repo` (owner+slash `--gb-fg-3`, repo `--gb-accent` weight 600); the `Pin` icon is the un-pin click target; footer split by a `--gb-line` hairline with `<GitPullRequest size={11}/> N open` left (show `?` when count is `'?'`) and `relTime(r)` right. The card (except the pin target) links to the repo:
  ```jsx
  <SectionHead kicker="PINNED" title="Repositories you keep coming back to" />
  {pinnedRepos.length === 0 ? (
    <Card style={{ borderStyle: 'dashed', padding: 18, textAlign: 'center', color: 'var(--gb-fg-3)', fontSize: 12.5, marginBottom: 26 }}>
      <Pin size={14} style={{ verticalAlign: '-2px', marginRight: 6, color: 'var(--gb-fg-4)' }} />
      Pin up to {MAX_PINS} repositories to keep them at the top.
    </Card>
  ) : (
    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 26 }}>
      {pinnedRepos.map((r) => {
        const slug = slugOf(r);
        const c = openPRCount[slug];
        const count = c === '?' ? '?' : (c ?? 0);
        return (
          <Card key={slug} style={{ padding: 14, display: 'flex', flexDirection: 'column', gap: 8, cursor: 'pointer' }}
                onClick={() => onNavigate('repository', { owner: r.owner, repo: r.name })}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              {r.visibility === 'private' ? <Lock size={13} color="var(--gb-fg-3)" /> : <Globe size={13} color="var(--gb-fg-3)" />}
              <span style={{ fontSize: 13.5, fontWeight: 600 }}>
                <span style={{ color: 'var(--gb-fg-3)' }}>{r.owner} / </span>
                <span style={{ color: 'var(--gb-accent)' }}>{r.name}</span>
              </span>
              <span title="Unpin" onClick={(e) => togglePin(slug, e)} style={{ marginLeft: 'auto', cursor: 'pointer', display: 'inline-flex' }}>
                <Pin size={12} color="var(--gb-accent)" fill="var(--gb-accent)" />
              </span>
            </div>
            <div style={{ fontSize: 12, color: 'var(--gb-fg-3)', lineHeight: 1.45, minHeight: 30, display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical', overflow: 'hidden' }}>
              {r.description || 'No description provided.'}
            </div>
            <div style={{ display: 'flex', alignItems: 'center', fontSize: 11.5, color: 'var(--gb-fg-3)', borderTop: '1px solid var(--gb-line)', paddingTop: 8 }}>
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
                <GitPullRequest size={11} /> <span style={{ fontFamily: 'var(--gb-mono)' }}>{count}</span> open
              </span>
              <span style={{ marginLeft: 'auto', color: 'var(--gb-fg-4)' }}>{relTime(r)}</span>
            </div>
          </Card>
        );
      })}
    </div>
  )}
  ```

- [ ] **Step 12: Build the All-repositories section** (left column, second child): `SectionHead` + filter row (controlled `search`/`typeFilter`/`sort`) + a single padding-0 `Card` of rows.
  ```jsx
  <SectionHead kicker="ALL" title="All repositories" right={<span style={{ color: 'var(--gb-fg-3)', fontSize: 12 }}><span style={{ fontFamily: 'var(--gb-mono)' }}>{repos.length}</span> total</span>} />
  <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10 }}>
    <div style={{ flex: 1, display: 'inline-flex', alignItems: 'center', gap: 8, height: 32, padding: '0 11px', borderRadius: 7, background: 'var(--gb-surface)', border: '1px solid var(--gb-line)' }}>
      <Search size={13} color="var(--gb-fg-3)" />
      <input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Filter repositories…"
             style={{ flex: 1, border: 'none', background: 'transparent', outline: 'none', color: 'var(--gb-fg-2)', fontSize: 12.5, fontFamily: 'inherit' }} />
    </div>
    <select value={typeFilter} onChange={(e) => setTypeFilter(e.target.value)} className="btn" style={{ fontFamily: 'inherit', color: 'var(--gb-fg-2)' }}>
      <option value="all">Type: all</option>
      <option value="public">Public</option>
      <option value="private">Private</option>
    </select>
    <select value={sort} onChange={(e) => setSort(e.target.value)} className="btn" style={{ fontFamily: 'inherit', color: 'var(--gb-fg-2)' }}>
      <option value="updated">Sort: recently updated</option>
      <option value="name-asc">Sort: name (A→Z)</option>
      <option value="name-desc">Sort: name (Z→A)</option>
      <option value="open-prs">Sort: most open PRs</option>
    </select>
  </div>
  ```
  Then the rows Card. Grid per row `20px 1fr 100px 130px`, 12×16 padding, 14 gap, `--gb-line` top border except first, hover `--gb-hover` via inline `onMouseEnter/Leave` (no `.hoverable`, no translateY lift), whole row links:
  ```jsx
  <Card style={{ padding: 0 }}>
    {filtered.length === 0 ? (
      <div style={{ padding: '20px 16px', color: 'var(--gb-fg-3)', fontSize: 12.5 }}>No repositories match your filters.</div>
    ) : filtered.map((r, i) => {
      const slug = slugOf(r);
      const c = openPRCount[slug];
      const count = c === '?' ? '?' : (c ?? 0);
      const hasPRs = typeof c === 'number' && c > 0;
      return (
        <div key={slug}
             onClick={() => onNavigate('repository', { owner: r.owner, repo: r.name })}
             onMouseEnter={(e) => (e.currentTarget.style.background = 'var(--gb-hover)')}
             onMouseLeave={(e) => (e.currentTarget.style.background = 'transparent')}
             style={{ display: 'grid', gridTemplateColumns: '20px 1fr 100px 130px', gap: 14, alignItems: 'center', padding: '12px 16px', cursor: 'pointer', borderTop: i === 0 ? 'none' : '1px solid var(--gb-line)' }}>
          {r.visibility === 'private' ? <Lock size={14} color="var(--gb-fg-3)" /> : <Globe size={14} color="var(--gb-fg-3)" />}
          <div style={{ minWidth: 0 }}>
            <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--gb-accent)' }}>{r.name}</div>
            {r.description && <div style={{ fontSize: 11.5, color: 'var(--gb-fg-3)', marginTop: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{r.description}</div>}
          </div>
          <span style={{ fontSize: 11.5, color: hasPRs ? 'var(--gb-fg-2)' : 'var(--gb-fg-4)', display: 'inline-flex', alignItems: 'center', gap: 4 }}>
            <GitPullRequest size={11} /> <span style={{ fontFamily: 'var(--gb-mono)' }}>{count}</span> open
          </span>
          <span style={{ fontSize: 11.5, color: 'var(--gb-fg-3)', textAlign: 'right' }}>{relTime(r)}</span>
        </div>
      );
    })}
  </Card>
  ```

- [ ] **Step 13: Build the right-rail "Waiting on you" panel** (first `aside` child). Empty state is a quiet one-liner inside the Card. Each row → `GitPullRequest` ok-green icon, PR title (1 line ellipsized), `owner/repo · #number` underneath; click navigates to the PR detail route (`pull_detail` — the key `App.jsx` `renderView`/`navigate` use, params `{ owner, repo, number }`):
  ```jsx
  <div>
    <SectionHead kicker="WAITING" title="Waiting on you" />
    <Card style={{ padding: waitingOnMe.length ? 0 : 12 }}>
      {waitingOnMe.length === 0 ? (
        <div style={{ fontSize: 12, color: 'var(--gb-fg-3)' }}>Nothing waiting — clear inbox.</div>
      ) : waitingOnMe.map((p, i) => (
        <div key={`${p.owner}/${p.repo}#${p.number}`}
             onClick={() => onNavigate('pull_detail', { owner: p.owner, repo: p.repo, number: p.number })}
             style={{ display: 'flex', gap: 10, padding: '11px 13px', cursor: 'pointer', borderTop: i === 0 ? 'none' : '1px solid var(--gb-line)' }}>
          <GitPullRequest size={14} color="var(--gb-ok)" style={{ flexShrink: 0, marginTop: 2 }} />
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontSize: 12, color: 'var(--gb-fg-2)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{p.title}</div>
            <div style={{ fontSize: 10.5, color: 'var(--gb-fg-4)', marginTop: 2 }}>
              <span style={{ fontFamily: 'var(--gb-mono)' }}>{p.owner}/{p.repo}</span> · #{p.number}
            </div>
          </div>
        </div>
      ))}
    </Card>
  </div>
  ```

- [ ] **Step 14: Build the right-rail "Recently visited" panel** (second `aside` child). 5 rows from `recentRepos`. `History` icon + `owner / repo` (repo in `--gb-accent`); click navigates to repo. Empty → "No recent visits.":
  ```jsx
  <div>
    <SectionHead kicker="RECENT" title="Recently visited" />
    <Card style={{ padding: 6 }}>
      {recentRepos.length === 0 ? (
        <div style={{ fontSize: 12, color: 'var(--gb-fg-3)', padding: '7px 10px' }}>No recent visits.</div>
      ) : recentRepos.map((r) => (
        <div key={slugOf(r)} onClick={() => onNavigate('repository', { owner: r.owner, repo: r.name })}
             onMouseEnter={(e) => (e.currentTarget.style.background = 'var(--gb-hover)')}
             onMouseLeave={(e) => (e.currentTarget.style.background = 'transparent')}
             style={{ display: 'flex', alignItems: 'center', gap: 9, padding: '7px 10px', borderRadius: 5, fontSize: 12.5, cursor: 'pointer' }}>
          <History size={12} color="var(--gb-fg-4)" />
          <span><span style={{ color: 'var(--gb-fg-3)' }}>{r.owner} / </span><span style={{ color: 'var(--gb-accent)' }}>{r.name}</span></span>
        </div>
      ))}
    </Card>
  </div>
  ```

- [ ] **Step 15: Delete dead code / confirm no regressions.** Verify the old `.gradient-text` H1, the top full-width search bar, the `.glass-card hoverable` grid, and the old `filteredRepos` const are all gone. Confirm `handleCreateRepo` (L38-64) and the Create Repository modal (L216-302) are untouched and still wired to `showModal`/`setShowModal`. Confirm no raw `fetch` was introduced and no new npm deps.

- [ ] **Step 16: Lint + build.** Run: `cd frontend && npm run lint && npm run build` — Expected: PASS. If lint flags `useCallback`/`useMemo` deps, satisfy them honestly (`user`, `repos`, `openPRCount` as shown) rather than disabling the rule.

- [ ] **Step 17: Screenshot verification under DEV_MODE.** `cd frontend && npm run dev`, sign in as a mock user (`Bearer mock_<uid>` via `authService`), navigate to `/`, take a browser screenshot. Compare against `design_handoff_today_v1/references/mock-dashboard.jsx`: (a) tight kicker+greeting hero with accent username, (b) 2-up pinned grid (or dashed empty card), (c) filter row with working search/type/sort, (d) flat row list with per-row open-PR count + relative time and `--gb-hover` on hover with NO translateY lift, (e) right rail Waiting-on-you (or "clear inbox") and Recently visited. Toggle a pin, refresh, confirm it persists.

- [ ] **Step 18: Commit** (on the existing `feat/today-ui-refresh` branch — do NOT create a new branch):
  ```bash
  git add frontend/src/pages/Dashboard.jsx
  git commit -m "feat(ui): refresh dashboard with pinned, waiting-on-you, and recent panels

  Replace the flat glass-card grid with a two-column layout: pinned repos
  (localStorage), a client-side filtered/sorted repo list with per-row
  open-PR counts, a 'Waiting on you' N+1 over /pulls, and a recently-visited
  rail. Removes the gradient hero and .hoverable lift per the design tokens.

  Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
  ```

---

### Task 3: Repository / Code tab refresh

**Files:**
- Modify: `frontend/src/pages/Repository.jsx`
  - Imports block (lines ~1–28): add primitives + new lucide icons.
  - State block (lines ~142–177): add `tags` / `tagsError` / `collaboratorsCount` state.
  - Content loader effect (lines ~314–385): lift tags + collaborators fetch onto the Code tab.
  - Repo header (lines ~491–554): remove the 320px inline clone-URL `page-header-actions` block; restyle title + add stats row.
  - Code-tab render branch (lines ~612–808): wrap in `1fr 280px` grid, restyle branch row + commit bar, keep existing tree/CODEOWNERS rows, add right-rail cards.
  - Add **local helper components inside this file** (above the `Repository` component, near `QuickstartCard` at line ~77): `fileMix(treeItems)` pure function, `FileMixCard`, `BranchesRailCard`, `TagsRailCard`, `AboutRailCard`. Keep them in-file (no new files) since they are page-local.
  - `QuickstartCard` (lines ~77–122): restyle to solid surface (drop `glass-card`).

> **CODEOWNERS (load-bearing correction):** The page already fetches `/api/repos/:o/:r/codeowners?path=&ref=` (lines ~341–355) and stores `codeowners` as a `{ childName: ["@owner", …] }` map — the Go backend parses CODEOWNERS server-side and returns the matched map directly (confirmed in `06-api-contracts.md`). **Do NOT add client-side CODEOWNERS file fetching or glob parsing.** Reuse the existing `codeowners` state; this task only restyles its per-row chip rendering. Missing/empty CODEOWNERS already yields `{}` → no chips, no error (existing `try/catch` at lines ~347–352 swallows failures into `{}`).

- [ ] **Step 1: Add imports.** In the lucide-react import block (lines ~7–28) add only `Tag`, `Hash`, `Users`. Validation confirmed `Copy`, `Check`, `Clock`, `GitBranch`, `Lock`, `Globe` are **already imported** — do NOT re-add them (duplicate imports fail lint). Add the Task 1 primitive imports below the existing component imports (after line 6):
  ```jsx
  import Card from '../components/Card';
  import Chip from '../components/Chip';
  import SectionHead from '../components/SectionHead';
  import Avatar from '../components/Avatar';
  ```
  (Keep existing `BranchTagPicker`, `LatestCommitBar`, `BranchProtectionModal` imports — they are reused.)

- [ ] **Step 2: Add the `fileMix` pure helper** above the `Repository` component (near `QuickstartCard`, ~line 76):
  ```jsx
  const FILE_MIX_PALETTE = {
    '.go': '#5fc7f5',
    '.jsx': '#fbbf24', '.tsx': '#fbbf24',
    '.js': '#fde68a', '.ts': '#fde68a', '.mjs': '#fde68a',
    '.md': '#a78bfa',
    '.css': '#f9a8d4', '.scss': '#f9a8d4',
    '.sh': '#4ade80',
    other: '#5d6678', config: '#5d6678',
  };
  const mixColor = (name) => FILE_MIX_PALETTE[name] || FILE_MIX_PALETTE.other;

  // Derive a top-5 + "other" file-mix from the current tree listing. No backend.
  function fileMix(treeItems) {
    const blobs = (treeItems || []).filter((t) => t.type === 'blob');
    if (blobs.length === 0) return { rows: [], total: 0 };
    const byExt = {};
    for (const it of blobs) {
      const dot = it.name.lastIndexOf('.');
      const ext = dot <= 0 ? 'config' : it.name.slice(dot).toLowerCase();
      byExt[ext] = (byExt[ext] || 0) + 1;
    }
    const sorted = Object.entries(byExt).sort((a, b) => b[1] - a[1]);
    const top = sorted.slice(0, 5);
    const otherCount = sorted.slice(5).reduce((s, [, n]) => s + n, 0);
    if (otherCount) top.push(['other', otherCount]);
    return {
      rows: top.map(([name, count]) => ({ name, count, color: mixColor(name) })),
      total: blobs.length,
    };
  }
  ```

- [ ] **Step 3: Add new page state + derived mix.** In the Code-tab state cluster (after `const [codeowners, setCodeowners] = useState({});`, ~line 143) add:
  ```jsx
  const [tags, setTags] = useState(null);          // null = not loaded yet; [] = loaded empty
  const [tagsError, setTagsError] = useState('');
  const [collaboratorsCount, setCollaboratorsCount] = useState(null);
  ```
  And near other derived values (ensure `useMemo` is imported):
  ```jsx
  const mix = useMemo(() => fileMix(treeItems), [treeItems]);
  ```

- [ ] **Step 4: Lift Tags + Collaborators fetch onto the Code tab.** Add a new effect after the content loader effect (after line ~385). Both come from existing endpoints (`/tags`, `/collaborators`). Reset tags on branch change:
  ```jsx
  useEffect(() => {
    if (activeTab !== 'code' || !currentBranch) return;
    let cancelled = false;
    setTags(null);
    setTagsError('');
    apiClient.get(`/api/repos/${owner}/${repo}/tags`)
      .then((data) => { if (!cancelled) setTags(Array.isArray(data) ? data : []); })
      .catch((err) => {
        if (!cancelled) { setTagsError(err.message || 'Failed to load tags'); setTags([]); }
      });
    return () => { cancelled = true; };
  }, [activeTab, owner, repo, currentBranch]);

  useEffect(() => {
    if (activeTab !== 'code') return;
    let cancelled = false;
    apiClient.get(`/api/repos/${owner}/${repo}/collaborators`)
      .then((data) => { if (!cancelled) setCollaboratorsCount(Array.isArray(data) ? data.length : 0); })
      .catch(() => { if (!cancelled) setCollaboratorsCount(null); }); // count just hides on failure
    return () => { cancelled = true; };
  }, [activeTab, owner, repo]);
  ```
  The existing Settings-tab collaborators effect (lines ~215–220) stays untouched; this is a separate count-only read.

- [ ] **Step 5: Refine the repo header — remove the inline clone block, add the stats row.** In the header (lines ~491–554):
  - Delete the entire `{/* HTTPS Clone Link */}` `<div className="page-header-actions">…</div>` block (lines ~511–553). The clone URL moves to the Code-tab branch row (Step 7).
  - Change the `<span style={{ color: '#38bdf8' }}>{meta.name}</span>` to `font-weight: 500` per spec, owner/slash to `--gb-fg-3`. Keep `meta.description` as the `<p>` above the new stats row.
  - Add a stats row directly under the description `<p>`:
    ```jsx
    <div style={{ display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: 14, marginTop: 10, fontSize: 12.5, color: 'var(--gb-fg-3)' }}>
      <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
        <GitBranch size={13} /> <span className="mono">{meta.defaultBranch || 'main'}</span>
      </span>
      <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
        <GitBranch size={13} /> {(meta.branches || []).length} branches
      </span>
      {tags != null && (
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
          <Hash size={13} /> {tags.length} tags
        </span>
      )}
      {collaboratorsCount != null && (
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
          <Users size={13} /> {collaboratorsCount + 1} collaborators
        </span>
      )}
    </div>
    ```
    (`+1` synthesizes the owner, who is not in the `/collaborators` array — see `06-api-contracts.md`.) The `copyCloneUrl`/`copied` state stays; it's now used by the branch-row chip.

- [ ] **Step 6: Wrap the Code-tab body in the two-column grid.** In the `activeTab === 'code'` branch (line ~612), the branch/breadcrumb row stays full-width on top. Wrap the **folder (Tree)** view (the `else` branch ~line 691, containing LatestCommitBar + file-list + readme) so its inner content becomes the main column of a grid, and add a right rail. The **file (Blob) view** (lines ~675–689) stays full-width and unchanged:
  ```jsx
  <div style={{ display: 'grid', gridTemplateColumns: '1fr 280px', gap: 18, alignItems: 'start' }}>
    <div>{/* MAIN COLUMN: existing LatestCommitBar + file-list + quickstart + readme */}</div>
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>{/* RIGHT RAIL — Step 9 */}</div>
  </div>
  ```
  Empty repo (`treeItems.length === 0 && !currentPath`): render the right rail with only About + Branches cards (FileMix and Tags hide on no data — handled inside those cards).

- [ ] **Step 7: Restyle the branch + clone-URL row.** Restyle the branch-selector header `<div>` (lines ~615–665) so the picker, a `N branches · M tags` mono caption, and a right-aligned clone chip sit on one flex row (`display:flex; align-items:center; gap:10px; margin-bottom:14px`). Keep `<BranchTagPicker {...}>` with its existing props/`onChange` verbatim and the breadcrumb block. Add after the picker:
  ```jsx
  <span className="mono" style={{ fontSize: 12, color: 'var(--gb-fg-4)' }}>
    {(meta.branches || []).length} branches{tags != null ? ` · ${tags.length} tags` : ''}
  </span>
  <button
    type="button"
    onClick={copyCloneUrl}
    title="Copy clone URL"
    style={{
      marginLeft: 'auto', display: 'inline-flex', alignItems: 'center', gap: 8,
      padding: '6px 10px', borderRadius: 7, background: 'var(--gb-surface)',
      border: '1px solid var(--gb-line)', fontFamily: 'var(--gb-mono)',
      fontSize: 12, color: 'var(--gb-fg-4)', cursor: 'pointer',
    }}
  >
    {cloneUrl}
    {copied ? <Check size={13} style={{ color: 'var(--gb-accent)' }} /> : <Copy size={13} />}
  </button>
  ```
  (Reuses existing `copyCloneUrl`/`copied`/`cloneUrl`.) Keep the existing "Back to Folder" button for the file view.

- [ ] **Step 8: Restyle the LatestCommitBar (no build chip).**
  - **Do NOT add a build-status chip here.** Validation confirmed `/refs/{branch}/head` (`BranchHeadInfo`) returns only `sha/authorName/authorEmail/date/message/totalCommits` — there is no `overallStatus` on the head endpoint (build status is decorated only onto `/commits/:branch` and `/pulls/:n/commits`). A chip keyed on `head.overallStatus` would never render and the unused `Chip`/`Check` import would fail lint. Build status is surfaced on PR detail (Task 5), not here.
  - Restyle only: if `.latest-commit-bar` still shows `glass-card`, change that to a solid surface (`background: var(--gb-surface); border: 1px solid var(--gb-line); border-radius: 8px`) — preferably by editing the `.latest-commit-bar` rule in `index.css` rather than the component. No JSX/import changes to `LatestCommitBar.jsx`. Verify via screenshot in Step 12.

- [ ] **Step 9: Build the right rail cards.** Inside the rail cell from Step 6, render in order: **About** (always), **FileMix** (only if `mix.total > 0`), **Branches** (always), **Tags** (only if `tags && tags.length > 0`):
  ```jsx
  <AboutRailCard meta={meta} collaboratorsCount={collaboratorsCount} commits={commits} />
  {mix.total > 0 && <FileMixCard mix={mix} />}
  <BranchesRailCard
    branches={meta.branches || []}
    defaultBranch={meta.defaultBranch || 'main'}
    currentBranch={currentBranch}
    onSelect={(b) => { setCurrentBranch(b); setViewingFile(null); setFileContent(''); }}
  />
  {tags && tags.length > 0 && <TagsRailCard tags={tags} />}
  {tagsError && (!tags || tags.length === 0) && (
    <Card style={{ padding: 16 }}>
      <SectionHead kicker="TAGS" title="" />
      <div style={{ fontSize: 12, color: 'var(--gb-err)' }}>{tagsError}</div>
    </Card>
  )}
  ```
  Define these helper components above `Repository` (in-file), each using the Task 1 primitives. The RDY badge in the SectionHead right slot uses `Chip variant="ok"` (there is no `rdy` variant):
  ```jsx
  function AboutRailCard({ meta, collaboratorsCount, commits }) {
    const VisIcon = meta.visibility === 'private' ? Lock : Globe;
    return (
      <Card style={{ padding: 16 }}>
        <SectionHead kicker="ABOUT" title="" right={<Chip variant="ok" dot>RDY</Chip>} />
        {meta.description && (
          <p style={{ fontSize: 13, color: 'var(--gb-fg-2)', lineHeight: 1.5, margin: '0 0 12px' }}>
            {meta.description}
          </p>
        )}
        <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 8, fontSize: 12.5, color: 'var(--gb-fg-3)' }}>
          <li style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <VisIcon size={12} /> {meta.visibility}
          </li>
          <li style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <GitBranch size={12} /> {(meta.branches || []).length} branches
            <span style={{ color: 'var(--gb-fg-4)' }}>· default</span>
            <span className="mono" style={{ fontSize: 11.5 }}>{meta.defaultBranch || 'main'}</span>
          </li>
          {collaboratorsCount != null && (
            <li style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <Users size={12} /> {collaboratorsCount + 1} collaborators
            </li>
          )}
          {commits && commits.length > 0 && (
            <li style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <Clock size={12} /> {commits.length}{commits.length >= 50 ? '+' : ''} commits loaded
            </li>
          )}
        </ul>
      </Card>
    );
  }

  function FileMixCard({ mix }) {
    return (
      <Card style={{ padding: 16 }}>
        <SectionHead kicker="FILE MIX" title="" right={<Chip variant="ok" dot>RDY</Chip>} />
        <div style={{ display: 'flex', height: 8, borderRadius: 4, overflow: 'hidden', marginBottom: 10, background: 'var(--gb-surface-2)' }}>
          {mix.rows.map((r) => (
            <div key={r.name} style={{ width: `${(r.count / mix.total) * 100}%`, background: r.color }} />
          ))}
        </div>
        <ul style={{ listStyle: 'none', margin: '0 0 8px', padding: 0, display: 'flex', flexWrap: 'wrap', gap: '4px 12px', fontSize: 11.5 }}>
          {mix.rows.map((r) => (
            <li key={r.name} style={{ display: 'inline-flex', alignItems: 'center', gap: 6, color: 'var(--gb-fg-2)' }}>
              <span style={{ width: 7, height: 7, borderRadius: 999, background: r.color, display: 'inline-block' }} />
              <span className="mono" style={{ fontWeight: 500 }}>{r.name}</span>
              <span className="mono" style={{ color: 'var(--gb-fg-4)' }}>{r.count}</span>
            </li>
          ))}
        </ul>
        <div style={{ fontSize: 11, color: 'var(--gb-fg-4)' }}>
          by file count — {mix.total} files at this ref
        </div>
      </Card>
    );
  }

  function BranchesRailCard({ branches, defaultBranch, currentBranch, onSelect }) {
    return (
      <Card style={{ padding: 16 }}>
        <SectionHead kicker="BRANCHES" title="" right={<Chip variant="ok" dot>RDY</Chip>} />
        <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 2 }}>
          {branches.map((b) => (
            <li key={b}>
              <button
                type="button"
                onClick={() => onSelect(b)}
                style={{
                  width: '100%', display: 'flex', alignItems: 'center', gap: 8,
                  padding: '6px 8px', borderRadius: 6, border: 'none', cursor: 'pointer',
                  background: b === currentBranch ? 'var(--gb-hover)' : 'transparent',
                  color: 'var(--gb-fg-2)', fontSize: 12.5, textAlign: 'left',
                }}
              >
                <GitBranch size={12} style={{ color: 'var(--gb-fg-3)', flexShrink: 0 }} />
                <span className="mono" style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{b}</span>
                {b === defaultBranch && (
                  <span style={{ marginLeft: 'auto' }}><Chip variant="accent">default</Chip></span>
                )}
              </button>
            </li>
          ))}
        </ul>
      </Card>
    );
  }

  function TagsRailCard({ tags }) {
    const top = tags.slice(0, 5);
    return (
      <Card style={{ padding: 16 }}>
        <SectionHead kicker="TAGS" title="" right={<Chip variant="ok" dot>RDY</Chip>} />
        <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 6 }}>
          {top.map((t, i) => (
            <li key={t.name} style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12.5 }}>
              <Hash size={12} style={{ color: 'var(--gb-fg-3)', flexShrink: 0 }} />
              <span className="mono" style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{t.name}</span>
              {i === 0 && <span style={{ marginLeft: 'auto' }}><Chip variant="ok">latest</Chip></span>}
            </li>
          ))}
        </ul>
        <div style={{ fontSize: 11, color: 'var(--gb-fg-4)', marginTop: 10 }}>
          Promoting tags to Releases (with body + assets) needs new backend work.
        </div>
      </Card>
    );
  }
  ```
  Confirm `tags` items expose a `.name` field against `06-api-contracts.md` `/tags`; if they are bare strings, use the string directly as the key/label.

- [ ] **Step 10: Keep & restyle the per-row CODEOWNERS chips.** The tree rows (lines ~725–765) already render `codeowners[item.name]`. Keep the existing folders-first / blobs-second logic and click handlers verbatim. Replace the inline `<span>{codeowners[item.name].join(' ')}</span>` with mapped ghost chips (small mono pills) in both folder and blob rows:
  ```jsx
  {codeowners[item.name] && codeowners[item.name].length > 0 && (
    <span style={{ display: 'inline-flex', gap: 5, flexWrap: 'wrap', marginRight: 12 }}
          title={`CODEOWNERS: ${codeowners[item.name].join(', ')}`}>
      {codeowners[item.name].map((o) => (
        <span key={o} style={{
          height: 18, lineHeight: '18px', padding: '0 7px', borderRadius: 999,
          border: '1px solid var(--gb-line)', fontSize: 10.5, color: 'var(--gb-fg-3)',
          fontFamily: 'var(--gb-mono)', whiteSpace: 'nowrap',
        }}>{o}</span>
      ))}
    </span>
  )}
  ```
  Empty/missing CODEOWNERS → falsy guard → nothing renders. Leave the `.file-list` wrapper as a solid surface (drop any `glass-card`).

- [ ] **Step 11: Lint + build.** From `frontend/`: `npm run lint && npm run build`. Resolve any unused-import warnings; build must emit `frontend/dist` cleanly.

- [ ] **Step 12: Screenshot verification under DEV_MODE.** `cd frontend && npm run dev`, open a populated repo's Code tab under mock auth, screenshot. Compare to `design_handoff_today_v1/references/mock-code.jsx`: 2-column grid (`1fr 280px`), solid-surface cards; CODEOWNERS chips on matched rows (none where unmatched); file-mix card with stacked bar + ext list + `by file count — N files at this ref` caption; Branches and Tags rails render without opening the picker; default-branch + `latest` chips; clone chip right-aligned on the branch row, header has no inline clone block. Verify the empty-repo path: Quickstart prominent, About + Branches rail only.

- [ ] **Step 13: Commit** (on `feat/today-ui-refresh`):
  ```bash
  # stage index.css too if you restyled .latest-commit-bar there
  git add frontend/src/pages/Repository.jsx frontend/src/index.css
  git commit -m "$(cat <<'EOF'
  feat(ui): refresh repository code tab with CODEOWNERS chips, file-mix + branch/tag rails

  Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
  EOF
  )"
  ```

---

### Task 4: Repository / Insights tab (new)

**Files:**
- Modify: `frontend/src/pages/Repository.jsx`
  - Imports (lines 1–28): add Task-1 primitives + `formatRelative` + new lucide icons.
  - State block (lines ~157–177): add Insights-only state (`insightsLoaded`, `insightsLoading`, `tags`, `insightsPulls`, `codeownersRoot`). Reuse existing `collaborators` and `protectionRules` state (already declared lines 169 & 174). **If Task 3 already added `tags` state, reuse it — do not redeclare.**
  - New lazy fetch effect: insert after the existing protection-rules effect (after line 251).
  - Tab nav (lines 558–589 `tabs-container`): insert an "Insights" `<button>` with a NEW marker between Pull Requests (ends line 579) and the `isOwner && Settings` block (line 580).
  - Render switch: add `{activeTab === 'insights' && (...)}` branch inside the `<>...</>` fragment, after the Settings branch.
  - Local component: define `function InsightsTab({...})` (plus `RecentCommitsPanel`, `CollaboratorsPanel`, `ProtectionPanel`, `CodeownersPanel`) near the bottom of the file alongside `PullRequestList` — module-level, NOT nested inside the default export.

Verified facts (bake in):
- `onNavigate('repository', { owner, repo, tab: 'insights' })` already round-trips: `App.jsx` `navigate()` emits `?tab=insights`, `parseLocation()` reads `searchParams.get('tab')` → `initialTab`. **No `App.jsx` change required.** Unknown tabs already default to `code` (there is no `validTabs` array).
- The relativeTime util exports **`formatRelative`** — import that name.
- `collaborators` and `protectionRules` state already exist (lines 169, 174), populated by Settings-gated effects. Add a separate Insights-gated effect that populates the same state; both are idempotent and gated by different `activeTab` values, so they never race.

- [ ] **Step 1: Add imports.**
  - After the existing component imports (after line 6), add (skip any already added by Tasks 2/3):
    ```jsx
    import Card from '../components/Card';
    import Chip from '../components/Chip';
    import SectionHead from '../components/SectionHead';
    import Avatar from '../components/Avatar';
    import { formatRelative } from '../utils/relativeTime';
    ```
  - Extend the `lucide-react` import block (lines 7–28) with: `Activity, GitCommit, Tag, Users, ShieldCheck` (skip duplicates already imported by Task 3).

- [ ] **Step 2: Add Insights-only state.** After the branch-protection state block (after line 176), add (omit `tags` if Task 3 already declared it):
  ```jsx
  const [insightsLoaded, setInsightsLoaded] = useState(false);
  const [insightsLoading, setInsightsLoading] = useState(false);
  const [insightsPulls, setInsightsPulls] = useState([]);
  const [codeownersRoot, setCodeownersRoot] = useState({}); // { childName: ["@a", ...] }
  ```
  Reuse existing `commits`, `collaborators`, `protectionRules`, `meta`, and (from Task 3) `tags`.

- [ ] **Step 3: Add the lazy Insights fetch effect.** Insert after the branch-protection `useEffect` (after line 251). Fires once per repo when Insights first opens; fetches all panels in parallel; tolerant of partial failure; skips owner-only branch-protection for non-owners:
  ```jsx
  useEffect(() => {
    if (activeTab !== 'insights' || insightsLoaded) return;
    const branch = currentBranch || meta?.defaultBranch || 'main';
    if (!branch) return;

    let cancelled = false;
    setInsightsLoading(true);
    const safe = (p, fallback) => p.then((d) => d).catch(() => fallback);
    const coParams = new URLSearchParams({ path: '', ref: branch });

    Promise.all([
      safe(apiClient.get(`/api/repos/${owner}/${repo}/commits/${branch}?limit=${COMMITS_PAGE_SIZE}&offset=0`), []),
      safe(apiClient.get(`/api/repos/${owner}/${repo}/tags`), []),
      safe(apiClient.get(`/api/repos/${owner}/${repo}/pulls`), []),
      safe(apiClient.get(`/api/repos/${owner}/${repo}/collaborators`), []),
      isOwner
        ? safe(apiClient.get(`/api/repos/${owner}/${repo}/branch-protection`), [])
        : Promise.resolve([]),
      safe(apiClient.get(`/api/repos/${owner}/${repo}/codeowners?${coParams.toString()}`), { entries: {} }),
    ]).then(([commitList, tagList, pullList, collabList, rules, co]) => {
      if (cancelled) return;
      setCommits(Array.isArray(commitList) ? commitList : []);
      setTags(Array.isArray(tagList) ? tagList : []);
      setInsightsPulls(Array.isArray(pullList) ? pullList : []);
      setCollaborators(Array.isArray(collabList) ? collabList : []);
      setProtectionRules(Array.isArray(rules) ? rules : []);
      setCodeownersRoot((co && co.entries) || {});
      setInsightsLoaded(true);
      setInsightsLoading(false);
    });

    return () => { cancelled = true; };
  }, [activeTab, insightsLoaded, owner, repo, currentBranch, meta, isOwner]);
  ```
  Implementer note: confirm `COMMITS_PAGE_SIZE` is the actual constant name used by the existing commits effect; if it differs, match it. The codeowners shape `{ entries }` must match Task 3's understanding of the endpoint — both read `co.entries`.

- [ ] **Step 4: Add the Insights tab-nav button (with NEW marker).** Insert between the Pull Requests button (closes line 579) and the `{isOwner && (` Settings block (line 580):
  ```jsx
  <button
    className={`tab ${activeTab === 'insights' ? 'active' : ''}`}
    onClick={() => onNavigate('repository', { owner, repo, tab: 'insights' })}
  >
    <Activity size={18} />
    Insights
    <span
      style={{
        marginLeft: '0.4rem', fontFamily: 'var(--gb-mono)', fontSize: '9.5px',
        letterSpacing: '0.06em', textTransform: 'uppercase', color: 'var(--gb-accent)',
        background: 'var(--gb-accent-bg)', border: '1px solid var(--gb-line-accent)',
        borderRadius: '4px', padding: '0.05rem 0.3rem', lineHeight: 1.4,
      }}
    >
      New
    </span>
  </button>
  ```

- [ ] **Step 5: Add the render branch.** Inside the tab-content fragment, after the Settings branch closes and before the Pull Requests branch:
  ```jsx
  {activeTab === 'insights' && (
    <InsightsTab
      meta={meta}
      commits={commits}
      tags={tags}
      pulls={insightsPulls}
      collaborators={collaborators}
      protectionRules={protectionRules}
      codeownersRoot={codeownersRoot}
      isOwner={isOwner}
      loading={insightsLoading || !insightsLoaded}
      onNavigate={onNavigate}
      owner={owner}
      repo={repo}
    />
  )}
  ```

- [ ] **Step 6: Define `InsightsTab` with the stat strip + derivations.** Add at the bottom of the file, near `PullRequestList` (outside the default export):
  ```jsx
  function InsightsTab({
    meta, commits, tags, pulls, collaborators, protectionRules,
    codeownersRoot, isOwner, loading, owner, repo, onNavigate,
  }) {
    const branchCount = (meta?.branches || []).length;
    const tagCount = (tags || []).length;
    const openPrCount = (pulls || []).filter((p) => p.status === 'open').length;
    const collabCount = (collaborators || []).length;
    const ruleCount = (protectionRules || []).length;
    const commitCount = (commits || []).length;
    const commitLabel = commitCount >= COMMITS_PAGE_SIZE ? `${commitCount}+` : `${commitCount}`;

    const stats = [
      { kicker: 'COMMITS', value: commitLabel, icon: <GitCommit size={13} /> },
      { kicker: 'BRANCHES', value: `${branchCount}`, icon: <GitBranch size={13} /> },
      { kicker: 'TAGS', value: `${tagCount}`, icon: <Tag size={13} /> },
      { kicker: 'OPEN PRs', value: `${openPrCount}`, icon: <GitPullRequest size={13} /> },
      { kicker: 'COLLABS', value: `${collabCount}`, icon: <Users size={13} /> },
      { kicker: 'PROT. RULES', value: isOwner ? `${ruleCount}` : '—', icon: <ShieldCheck size={13} /> },
    ];

    if (loading) {
      return <div className="loader-container"><div className="loader"></div></div>;
    }

    const coEntries = Object.entries(codeownersRoot || {});
    const recentCommits = (commits || []).slice(0, 8);
    const defaultBranch = meta?.defaultBranch || 'main';

    return (
      <div style={{ fontFamily: 'var(--gb-sans)' }}>
        <SectionHead kicker="OVERVIEW" title="Repository at a glance" />
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(6, 1fr)', gap: 12, marginTop: 12, marginBottom: 22 }}>
          {stats.map((s) => (
            <div key={s.kicker} style={{ padding: 14, background: 'var(--gb-surface)', border: '1px solid var(--gb-line)', borderRadius: 10 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 5, fontFamily: 'var(--gb-mono)', fontSize: 10, textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--gb-fg-4)', marginBottom: 6 }}>
                {s.icon}{s.kicker}
              </div>
              <div style={{ fontSize: 22, fontWeight: 600, letterSpacing: '-0.01em', color: 'var(--gb-fg)' }}>{s.value}</div>
            </div>
          ))}
        </div>

        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 18, alignItems: 'start' }}>
          <RecentCommitsPanel commits={recentCommits} owner={owner} repo={repo} onNavigate={onNavigate} />
          <div style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
            <CollaboratorsPanel meta={meta} collaborators={collaborators} />
            <ProtectionPanel rules={protectionRules} isOwner={isOwner} />
            <CodeownersPanel entries={coEntries} branch={defaultBranch} />
          </div>
        </div>
      </div>
    );
  }
  ```

- [ ] **Step 7: Define `RecentCommitsPanel`** (below `InsightsTab`). Empty-state for zero-commit repos:
  ```jsx
  function RecentCommitsPanel({ commits, owner, repo, onNavigate }) {
    return (
      <Card style={{ padding: 16 }}>
        <SectionHead kicker="RECENT" title="Last 8 commits" />
        {commits.length === 0 ? (
          <div style={{ color: 'var(--gb-fg-4)', fontSize: 12.5, padding: '8px 0' }}>No commits yet.</div>
        ) : (
          <ul style={{ listStyle: 'none', margin: 0, padding: 0 }}>
            {commits.map((c) => (
              <li key={c.sha}
                  onClick={() => onNavigate('commit', { owner, repo, sha: c.sha })}
                  style={{ display: 'grid', gridTemplateColumns: '22px 1fr auto auto', gap: 10, alignItems: 'center', padding: '10px 0', cursor: 'pointer' }}
                  onMouseEnter={(e) => (e.currentTarget.style.background = 'var(--gb-hover)')}
                  onMouseLeave={(e) => (e.currentTarget.style.background = 'transparent')}>
                <Avatar name={c.authorName} size={22} />
                <span style={{ minWidth: 0, display: 'flex', gap: 7, alignItems: 'baseline' }}>
                  <strong style={{ fontSize: 12.5, color: 'var(--gb-fg)', whiteSpace: 'nowrap' }}>{c.authorName}</strong>
                  <span style={{ fontSize: 12, color: 'var(--gb-fg-3)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{c.message}</span>
                </span>
                <span style={{ fontFamily: 'var(--gb-mono)', fontSize: 11.5, color: 'var(--gb-fg-4)' }}>{c.sha.slice(0, 7)}</span>
                <span style={{ fontSize: 11, color: 'var(--gb-fg-4)', whiteSpace: 'nowrap' }}>{formatRelative(c.date)}</span>
              </li>
            ))}
          </ul>
        )}
      </Card>
    );
  }
  ```
  Confirm commit field names (`authorName`, `message`, `sha`, `date`) against the existing Commits-tab rendering and match them.

- [ ] **Step 8: Define `CollaboratorsPanel`** (synthesizes the implicit owner row, per `06-api-contracts.md`):
  ```jsx
  function CollaboratorsPanel({ meta, collaborators }) {
    const ownerName = meta?.owner || '';
    return (
      <Card style={{ padding: 16 }}>
        <SectionHead kicker="COLLABORATORS" title="" />
        <ul style={{ listStyle: 'none', margin: 0, padding: 0 }}>
          {ownerName && (
            <li key="__owner" style={{ display: 'grid', gridTemplateColumns: '24px 1fr auto', gap: 10, alignItems: 'center', padding: '9px 0' }}>
              <Avatar name={ownerName} size={24} />
              <span style={{ fontSize: 12.5, fontWeight: 600, color: 'var(--gb-fg)' }}>{ownerName}</span>
              <Chip variant="accent">owner</Chip>
            </li>
          )}
          {(collaborators || []).map((c) => (
            <li key={c.uid || c.username} style={{ display: 'grid', gridTemplateColumns: '24px 1fr auto auto', gap: 10, alignItems: 'center', padding: '9px 0', borderTop: '1px solid var(--gb-line)' }}>
              <Avatar name={c.username} size={24} />
              <span style={{ fontSize: 12.5, fontWeight: 500, color: 'var(--gb-fg-2)' }}>{c.username}</span>
              <span style={{ fontSize: 11, color: 'var(--gb-fg-4)', whiteSpace: 'nowrap' }}>{c.addedAt ? formatRelative(c.addedAt) : ''}</span>
              <Chip variant="default">collaborator</Chip>
            </li>
          ))}
          {ownerName && (collaborators || []).length === 0 && (
            <li style={{ fontSize: 12, color: 'var(--gb-fg-4)', padding: '9px 0', borderTop: '1px solid var(--gb-line)' }}>No additional collaborators.</li>
          )}
        </ul>
      </Card>
    );
  }
  ```

- [ ] **Step 9: Define `ProtectionPanel`** (owner-only; non-owner sees the quiet line; empty list → quiet line):
  ```jsx
  function ProtectionPanel({ rules, isOwner }) {
    return (
      <Card style={{ padding: 16 }}>
        <SectionHead kicker="BRANCH PROTECTION" title="" />
        {!isOwner ? (
          <div style={{ fontSize: 12, color: 'var(--gb-fg-4)' }}>Branch protection settings are visible to repo owners only.</div>
        ) : (rules || []).length === 0 ? (
          <div style={{ fontSize: 12, color: 'var(--gb-fg-4)' }}>No protection rules defined.</div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
            {rules.map((rule) => (
              <div key={rule.id} style={{ display: 'flex', flexDirection: 'column', gap: 6, paddingBottom: 10 }}>
                <div style={{ display: 'flex', alignItems: 'baseline', gap: 8, flexWrap: 'wrap' }}>
                  <span style={{ fontFamily: 'var(--gb-mono)', fontSize: 12, color: 'var(--gb-fg)' }}>{rule.pattern}</span>
                  <span style={{ fontFamily: 'var(--gb-mono)', fontSize: 11, color: 'var(--gb-fg-4)' }}>
                    push:{(rule.pushAllowlist || []).length} · merge:{(rule.mergeAllowlist || []).length}
                  </span>
                </div>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                  {rule.requirePullRequest && <Chip variant="accent">PR required</Chip>}
                  {rule.requireApprovals > 0 && <Chip variant="accent">{rule.requireApprovals} approval{rule.requireApprovals === 1 ? '' : 's'}</Chip>}
                  {rule.requireCodeownerApproval && <Chip variant="accent">CODEOWNERS</Chip>}
                  {rule.blockForcePush && <Chip variant="accent">no force-push</Chip>}
                  {rule.blockDeletion && <Chip variant="accent">no delete</Chip>}
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>
    );
  }
  ```

- [ ] **Step 10: Define `CodeownersPanel`** (maps root `entries`; empty → quiet line):
  ```jsx
  function CodeownersPanel({ entries, branch }) {
    return (
      <Card style={{ padding: 16 }}>
        <SectionHead kicker="CODEOWNERS" title="" right={<span style={{ fontFamily: 'var(--gb-mono)', fontSize: 10.5, color: 'var(--gb-fg-4)' }}>{branch}</span>} />
        {(entries || []).length === 0 ? (
          <div style={{ fontSize: 12, color: 'var(--gb-fg-4)' }}>No CODEOWNERS rules on default branch.</div>
        ) : (
          <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 7 }}>
            {entries.map(([name, ownersList]) => (
              <li key={name} style={{ display: 'grid', gridTemplateColumns: '1fr auto', gap: 12, alignItems: 'baseline', fontFamily: 'var(--gb-mono)', fontSize: 11.5 }}>
                <span style={{ color: 'var(--gb-fg-2)' }}>{name}/</span>
                <span style={{ color: 'var(--gb-fg-3)', textAlign: 'right' }}>{(ownersList || []).join(' ')}</span>
              </li>
            ))}
          </ul>
        )}
      </Card>
    );
  }
  ```

- [ ] **Step 11: Lint + build.** From `frontend/`: `npm run lint && npm run build`. Fix unused imports; build must emit `frontend/dist`.

- [ ] **Step 12: Screenshot verification under DEV_MODE.** Build, run the Go server with `DEV_MODE=true`, open `/{owner}/{repo}?tab=insights`, screenshot. Verify against `design_handoff_today_v1/references/mock-insights.jsx`: "Insights" tab with **New** marker between Pull Requests and Settings and `active`; 6 stat cards populate; recent-commits panel (≤8 rows or "No commits yet."); right column Collaborators (synthesized owner row + accent chip), Branch protection (rules / quiet empty / permission line), CODEOWNERS (mapped rows or quiet line). Open DevTools Network: confirm **no new endpoints**, lazy fetch only on tab open, and other tabs still render after switching back.

- [ ] **Step 13: Commit** (on `feat/today-ui-refresh`):
  ```bash
  git add frontend/src/pages/Repository.jsx
  git commit -m "$(cat <<'EOF'
  feat(ui): add repository insights tab

  New ?tab=insights surface: a 6-card stat strip (commits/branches/tags/open
  PRs/collaborators/protection rules), recent commits, collaborators (with
  synthesized owner row), branch-protection summary, and a root CODEOWNERS
  map. All values derive from existing endpoints; data fetches lazily on first
  tab activation and tolerates partial failure. No router or backend changes.

  Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
  EOF
  )"
  ```

---

### Task 5: Repository / PR detail refresh

**Files:**
- Modify: `frontend/src/pages/Repository.jsx`
  - `PullRequestDetail` component spans **~1573–2530** (`function PullRequestDetail(...)` at line 1573, rendered when `activeTab === 'pull_detail'`).
  - Imports block (lines 7–28) — add new icons.
  - State + `Promise.all` data load at **~1761–1792**.
  - Existing reviewer-list IIFE at **~2300–2365** (replace with `ReviewerRail`), existing "Review Status" card at **~2413–2431** (replace with `RuleMatchedCard`), Conversation column body at **~2010–2293** (insert callouts), Commits sub-tab at **~2436–2466** (replace inner list with `CommitsList`).
- Add module-level sub-components (same file): `ReviewerRail`, `ApprovalsCallout`, `BuildCallout`, `CommitsList`, `RuleMatchedCard`. Add pure helpers `globMatch`, `getMatchingRule`, `buildReviewerList`, `approvalsProgress`, `reviewMeta`, `buildStatusMeta` near the top of the file (next to `renderReadme`).

All panels trace to existing endpoints (`06-api-contracts.md`): PR (`/pulls/:n`, carries `requestedReviewers`), commits (`/pulls/:n/commits`, each carries `overallStatus` + `statuses[]`), reviews (`/pulls/:n/reviews`), branch-protection (`/branch-protection`). Only the branch-protection fetch is new.

- [ ] **Step 1: Imports.** In the lucide import block (lines 7–28) add `CheckCircle2`, `XCircle`, `MessageSquareText`, `GitCommit` (keep existing `Clock`, `AlertTriangle`, `GitMerge`, `ArrowLeft`). Add the foundation imports below line 6 (skip duplicates from earlier tasks):
  ```jsx
  import Card from '../components/Card';
  import Chip from '../components/Chip';
  import SectionHead from '../components/SectionHead';
  import Avatar from '../components/Avatar';
  ```

- [ ] **Step 2: Pure helpers** (near `renderReadme`, line ~30). Glob match approximates `filepath.Match` (patterns use `*`/`?`), anchored so `main` doesn't match `release/main`:
  ```jsx
  function globMatch(pattern, value) {
    if (!pattern) return false;
    const re = new RegExp('^' + pattern
      .replace(/[.+^${}()|[\]\\]/g, '\\$&')
      .replace(/\*/g, '[^/]*')
      .replace(/\?/g, '[^/]') + '$');
    return re.test(value);
  }
  function getMatchingRule(rules, targetBranch) {
    if (!Array.isArray(rules) || !targetBranch) return null;
    return rules.find(r => globMatch(r.pattern, targetBranch)) || null;
  }
  function buildReviewerList(requestedReviewers, reviews) {
    const byUser = new Map();
    for (const username of (requestedReviewers || [])) {
      byUser.set(username.toLowerCase(), { username, status: 'pending', role: 'Requested · CODEOWNERS' });
    }
    for (const r of (reviews || [])) {
      const key = r.username.toLowerCase();
      byUser.set(key, { username: r.username, status: r.state, submittedAt: r.submittedAt, body: r.body, role: byUser.get(key)?.role || 'Reviewer' });
    }
    return [...byUser.values()];
  }
  function approvalsProgress(rule, reviews) {
    if (!rule) return null;
    const distinct = new Map();
    for (const r of (reviews || [])) distinct.set(r.username.toLowerCase(), r);
    const vals = [...distinct.values()];
    const given = vals.filter(r => r.state === 'approved').length;
    const changesRequested = vals.filter(r => r.state === 'changes_requested').map(r => r.username);
    return {
      required: rule.requireApprovals || 0,
      given,
      changesRequested,
      codeownersRequired: !!rule.requireCodeownerApproval,
      satisfied: given >= (rule.requireApprovals || 0) && changesRequested.length === 0,
    };
  }
  function reviewMeta(status) {
    switch (status) {
      case 'approved':          return { variant: 'ok',  Icon: CheckCircle2,      color: 'var(--gb-ok)',   label: 'Approved' };
      case 'changes_requested': return { variant: 'err', Icon: XCircle,           color: 'var(--gb-err)',  label: 'Changes requested' };
      case 'commented':         return { variant: 'default', Icon: MessageSquareText, color: 'var(--gb-fg-3)', label: 'Commented' };
      default:                  return { variant: 'warn', Icon: Clock,            color: 'var(--gb-warn)', label: 'Pending' };
    }
  }
  function buildStatusMeta(overallStatus) {
    const s = (overallStatus || '').toUpperCase();
    if (s === 'SUCCESS')
      return { variant: 'ok',  Icon: CheckCircle2,  color: 'var(--gb-ok)',   bg: 'var(--gb-ok-dim)',   chip: 'SUCCESS', title: 'Build passing on head commit' };
    if (s === 'FAILURE' || s === 'TIMEOUT' || s === 'CANCELLED')
      return { variant: 'err', Icon: AlertTriangle, color: 'var(--gb-err)',  bg: 'var(--gb-err-dim)',  chip: s,        title: `Build ${s.toLowerCase()} on head commit` };
    return { variant: 'warn', Icon: Clock, color: 'var(--gb-warn)', bg: 'var(--gb-warn-dim)', chip: s || 'PENDING', title: 'Build pending — no status reported' };
  }
  ```

- [ ] **Step 3: New state + branch-protection fetch.** Add `const [protectionRules, setProtectionRules] = useState([]);` beside `reviews` (line ~1729). In the load `Promise.all` (lines 1766–1771) add a fifth fetch (defaults to `[]`; `.catch` covers a non-owner 403):
  ```jsx
  const [prData, commitsData, diffData, reviewsData, rulesData] = await Promise.all([
    apiClient.get(`/api/repos/${owner}/${repo}/pulls/${prNumber}`),
    apiClient.get(`/api/repos/${owner}/${repo}/pulls/${prNumber}/commits`).catch(() => []),
    apiClient.get(`/api/repos/${owner}/${repo}/pulls/${prNumber}/diff`).catch(() => ({ rawDiff: '' })),
    apiClient.get(`/api/repos/${owner}/${repo}/pulls/${prNumber}/reviews`).catch(() => []),
    apiClient.get(`/api/repos/${owner}/${repo}/branch-protection`).catch(() => []),
  ]);
  ```
  Then `setProtectionRules(Array.isArray(rulesData) ? rulesData : []);` alongside the other setters (~line 1776).

- [ ] **Step 4: Derive once per render** (inside `PullRequestDetail`, after `pr` is non-null, before the `return` ~1933). Head commit is `commits[0]` (most recent first):
  ```jsx
  const matchingRule = useMemo(() => getMatchingRule(protectionRules, pr?.targetBranch), [protectionRules, pr?.targetBranch]);
  const approvals = useMemo(() => approvalsProgress(matchingRule, reviews), [matchingRule, reviews]);
  const reviewerList = useMemo(() => buildReviewerList(pr?.requestedReviewers, reviews), [pr?.requestedReviewers, reviews]);
  const headCommit = commits[0] || null;
  const buildMeta = buildStatusMeta(headCommit?.overallStatus);
  const headBuildId = headCommit?.statuses?.[0]?.buildId || null;
  // Merge requires BOTH no git conflict (pr.mergeable — the existing guard) AND
  // approval rules satisfied (or no rule). Never drop the pr.mergeable check.
  const approvalsOk = !matchingRule || (approvals && approvals.satisfied);
  const mergeAllowed = pr.mergeable === true && approvalsOk;
  const isOpen = pr.status === 'open';
  ```

- [ ] **Step 5: `ApprovalsCallout` sub-component.** Renders only on open PRs. Wire existing `handleMerge`/`actionLoading` through props:
  ```jsx
  function ApprovalsCallout({ approvals, matchingRule, mergeAllowed, onMerge, merging }) {
    const meta = !matchingRule
      ? { Icon: CheckCircle2, color: 'var(--gb-ok)', bg: 'var(--gb-ok-dim)' }
      : approvals.satisfied
        ? { Icon: CheckCircle2, color: 'var(--gb-ok)', bg: 'var(--gb-ok-dim)' }
        : approvals.changesRequested.length
          ? { Icon: XCircle, color: 'var(--gb-err)', bg: 'var(--gb-err-dim)' }
          : { Icon: Clock, color: 'var(--gb-warn)', bg: 'var(--gb-warn-dim)' };
    const Bubble = meta.Icon;
    const headline = !matchingRule ? 'No approvals required' : `Approvals · ${approvals.given} of ${approvals.required}`;
    const subhead = !matchingRule
      ? 'No protection rule applies — anyone with write access can merge.'
      : [
          `Rule for ${matchingRule.pattern} requires ${approvals.required} approval${approvals.required === 1 ? '' : 's'}${approvals.codeownersRequired ? ' + CODEOWNERS' : ''}`,
          approvals.changesRequested.length ? `changes requested by ${approvals.changesRequested.map(u => '@' + u).join(', ')}` : null,
        ].filter(Boolean).join(' · ');
    return (
      <Card style={{ padding: 0, marginTop: 16 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 14, padding: '14px 18px' }}>
          <div style={{ width: 36, height: 36, borderRadius: '50%', background: meta.bg, color: meta.color, display: 'grid', placeItems: 'center', flexShrink: 0 }}>
            <Bubble size={18} />
          </div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontSize: 14, fontWeight: 600, color: 'var(--gb-fg)' }}>{headline}</div>
            <div style={{ fontSize: 12, color: 'var(--gb-fg-3)' }}>{subhead}</div>
          </div>
          <button type="button" onClick={onMerge} disabled={!mergeAllowed || merging}
            style={{ display: 'inline-flex', alignItems: 'center', gap: 6, padding: '7px 12px', borderRadius: 6, fontSize: 13, border: '1px solid var(--gb-line)', background: 'var(--gb-surface-2)', color: 'var(--gb-fg)', cursor: mergeAllowed && !merging ? 'pointer' : 'not-allowed', opacity: mergeAllowed && !merging ? 1 : 0.6, flexShrink: 0 }}>
            <GitMerge size={13} /> {merging ? 'Merging…' : 'Merge pull request'}
          </button>
        </div>
      </Card>
    );
  }
  ```

- [ ] **Step 6: `BuildCallout` sub-component.** On failure-class status with a `buildId`, render a "view build logs →" link via `onNavigate('build_logs', { owner, repo, sha, buildId })` — the key `App.jsx` `navigate`/`renderView` use for `/:owner/:repo/commit/:sha/builds/:buildId`:
  ```jsx
  function BuildCallout({ meta, headCommit, buildId, owner, repo, onNavigate }) {
    const short = headCommit.sha.substring(0, 7);
    const isFail = meta.variant === 'err';
    const Bubble = meta.Icon;
    return (
      <Card style={{ padding: 0, marginTop: 16 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 14, padding: '14px 18px' }}>
          <div style={{ width: 36, height: 36, borderRadius: '50%', background: meta.bg, color: meta.color, display: 'grid', placeItems: 'center', flexShrink: 0 }}>
            <Bubble size={18} />
          </div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontSize: 14, fontWeight: 600, color: 'var(--gb-fg)' }}>{meta.title}</div>
            <div style={{ fontSize: 12, color: 'var(--gb-fg-3)' }}>
              <span style={{ fontFamily: 'var(--gb-mono)' }}>{short}</span> · status: {meta.chip}
              {isFail && buildId && (
                <>
                  {' · '}
                  <a role="button" tabIndex={0}
                     onClick={() => onNavigate('build_logs', { owner, repo, sha: headCommit.sha, buildId })}
                     style={{ color: 'var(--gb-err)', textDecoration: 'underline', cursor: 'pointer' }}>view build logs →</a>
                </>
              )}
            </div>
          </div>
          <Chip variant={meta.variant}>{meta.chip}</Chip>
        </div>
      </Card>
    );
  }
  ```

- [ ] **Step 7: `ReviewerRail` and `RuleMatchedCard` (right rail).** `ReviewerRail` replaces the IIFE at ~2300–2365 (keep the review-action form below it). Empty case: "No reviewers yet."
  ```jsx
  function ReviewerRail({ reviewers }) {
    return (
      <Card style={{ padding: 16 }}>
        <SectionHead kicker="REVIEWERS" title="" />
        {reviewers.length === 0 ? (
          <div style={{ fontSize: 12.5, color: 'var(--gb-fg-3)' }}>No reviewers yet.</div>
        ) : (
          <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 9 }}>
            {reviewers.map(r => {
              const m = reviewMeta(r.status);
              const RowIcon = m.Icon;
              return (
                <li key={r.username} style={{ display: 'grid', gridTemplateColumns: '22px 1fr 14px', gap: 9, alignItems: 'center' }}>
                  <Avatar name={r.username} size={22} />
                  <div style={{ minWidth: 0 }}>
                    <div style={{ fontSize: 12.5, fontWeight: 500, display: 'flex', alignItems: 'center', gap: 6 }}>
                      <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{r.username}</span>
                      <span style={{ fontSize: 10.5, color: m.color, fontWeight: 500, flexShrink: 0 }}>· {m.label}</span>
                    </div>
                    <div style={{ fontSize: 10.5, color: 'var(--gb-fg-4)' }}>{r.role}</div>
                  </div>
                  <RowIcon size={13} color={m.color} strokeWidth={2.4} />
                </li>
              );
            })}
          </ul>
        )}
      </Card>
    );
  }

  function RuleMatchedCard({ rule, targetBranch }) {
    if (!rule) return null;
    const lines = [
      rule.requirePullRequest ? 'PR required' : null,
      rule.requireApprovals > 0 ? `${rule.requireApprovals} approval${rule.requireApprovals === 1 ? '' : 's'} required` : null,
      rule.requireCodeownerApproval ? 'CODEOWNERS review required' : null,
      (rule.blockForcePush || rule.blockDeletion)
        ? `No ${[rule.blockForcePush && 'force-push', rule.blockDeletion && 'delete'].filter(Boolean).join(', no ')}`
        : null,
    ].filter(Boolean);
    return (
      <Card style={{ padding: 16 }}>
        <SectionHead kicker="RULE MATCHED" title="" />
        <div style={{ fontSize: 12, color: 'var(--gb-fg-3)', marginBottom: 8 }}>
          Target <span style={{ fontFamily: 'var(--gb-mono)' }}>{targetBranch}</span> matches protection rule
          {' '}<span style={{ fontFamily: 'var(--gb-mono)' }}>{rule.pattern}</span>:
        </div>
        <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 4 }}>
          {lines.map((l, i) => (
            <li key={i} style={{ fontSize: 12.5, color: 'var(--gb-fg-2)' }}>
              <span style={{ color: 'var(--gb-fg-4)', marginRight: 6 }}>·</span>{l}
            </li>
          ))}
        </ul>
      </Card>
    );
  }
  ```

- [ ] **Step 8: Wire the panels into the existing JSX (callouts are ADDITIVE — keep the existing merge box).**
  - In the Conversation column (`prTab === 'conversation'`, after the description card ~2028, immediately ABOVE the existing merge box at ~2102), insert the two callouts, only when open:
    ```jsx
    {isOpen && (
      <ApprovalsCallout approvals={approvals} matchingRule={matchingRule} mergeAllowed={mergeAllowed} onMerge={handleMerge} merging={actionLoading} />
    )}
    {isOpen && headCommit && (
      <BuildCallout meta={buildMeta} headCommit={headCommit} buildId={headBuildId} owner={owner} repo={repo} onNavigate={onNavigate} />
    )}
    ```
    **Do NOT remove or replace the existing merge box (lines ~2102–2206).** It carries the git-level conflict guard (`pr.mergeable !== true`) and the "Update via Merge/Rebase" conflict-resolution controls — these must survive. The callouts sit above it as an at-a-glance summary; the merge box stays the authoritative merge/close/update surface. Because `ApprovalsCallout`'s merge button also requires `pr.mergeable === true` (Step 4), it never offers a merge the box would block. Do NOT delete `handleMerge`/`handleClose`/`handleUpdateBranch`/`handleDeleteBranch` or the review-submit form.
  - In the right rail (`<div>` at ~2296): replace the reviewer IIFE with `<ReviewerRail reviewers={reviewerList} />` (keep the review-action form that follows) and replace the "Review Status" card with `<RuleMatchedCard rule={matchingRule} targetBranch={pr.targetBranch} />`.
  - In the `prTab === 'commits'` block (~2436): replace the inner list with `<CommitsList commits={commits} />`.

- [ ] **Step 9: `CommitsList` sub-component.** Zero-commit case handled:
  ```jsx
  function CommitsList({ commits }) {
    return (
      <Card style={{ padding: 0, overflow: 'hidden' }}>
        <div style={{ padding: '10px 14px', borderBottom: '1px solid var(--gb-line)', background: 'var(--gb-surface-2)' }}>
          <SectionHead kicker="COMMITS" title="" right={<span style={{ fontSize: 11, color: 'var(--gb-fg-4)' }}>{commits.length}</span>} />
        </div>
        {commits.length === 0 ? (
          <div style={{ padding: '24px', textAlign: 'center', fontSize: 12.5, color: 'var(--gb-fg-3)' }}>No commits in this pull request.</div>
        ) : commits.map((c, i) => (
          <div key={c.sha} style={{ display: 'grid', gridTemplateColumns: '22px 1fr auto auto', gap: 10, alignItems: 'center', padding: '10px 14px', borderTop: i === 0 ? 'none' : '1px solid var(--gb-line)' }}>
            <Avatar name={c.authorName} size={22} />
            <span style={{ fontSize: 13, color: 'var(--gb-fg)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{(c.message || '').split('\n')[0]}</span>
            <code style={{ fontFamily: 'var(--gb-mono)', fontSize: 11.5, color: 'var(--gb-accent)', background: 'var(--gb-accent-bg)', padding: '1px 6px', borderRadius: 3 }}>{c.sha.substring(0, 7)}</code>
            <span style={{ fontSize: 11, color: 'var(--gb-fg-4)' }}>{new Date(c.date).toLocaleDateString()}</span>
          </div>
        ))}
      </Card>
    );
  }
  ```

- [ ] **Step 10: Lint + build.** From `frontend/`: `npm run lint && npm run build`. Fix unused-import / hooks-deps warnings (new `useMemo` deps exhaustive). Build must emit `frontend/dist`.

- [ ] **Step 11: Screenshot verification.** Build, run the Go server under `DEV_MODE=true`, open `/:owner/:repo/pulls/:number`, screenshot, compare to `design_handoff_today_v1/references/mock-pr.jsx`: (a) reviewers card with approved/changes-requested/pending/commented icon+color, (b) approvals callout "given of required" with Merge disabled below threshold / on changes-requested, enabled when satisfied or unprotected, (c) build callout color matches head-commit `overallStatus`, failure link points at the build-logs route, (d) rule-matched card lists the matched rule (hidden when none), (e) zero-commit / no-reviewers / no-rule / closed-or-merged edge cases render quiet/hidden. Confirm existing merge, comments, and Files-changed diff still work.

- [ ] **Step 12: Commit** (on `feat/today-ui-refresh`):
  ```bash
  git add frontend/src/pages/Repository.jsx
  git commit -m "feat(ui): refresh pull request detail with reviewer rail, approvals + build callouts, commits panel

  Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
  ```

---

### Task 6: User Profile route (new)

**Files:**
- **Create:** `frontend/src/pages/Profile.jsx`
- **Modify:** `frontend/src/App.jsx` — `parseLocation` (~line 21-138), `navigate` (~line 175-226), `renderView` (~line 251-332), and the import block (~line 10-19)

The **bare honest** profile: avatar + username + email (own profile only) + an owner-filtered repo list + a dashed-border panel naming the gaps that need backend work. Values trace to existing endpoints: the `user` prop (already loaded from `/api/user/me`) and `GET /api/repos` (filtered client-side to `r.owner === username`).

**Routing collision note (load-bearing):** `/u/:username` is 2 segments, so it would be shadowed by the 2-segment `/:owner/:repo` branch (~line 111). Insert the `profile` branch BEFORE that branch, gated on `segments[0] === 'u'`. Safe to reserve `u`: the username regex is `^[a-zA-Z0-9-]{3,20}$` (min 3 chars), so a single-char first segment can never be a real owner.

- [ ] **Step 1: Add the `parseLocation` branch for `/u/:username`.** Insert **immediately before** the `// 6. Repository Details & Tabs` / `if (segments.length === 2)` branch (~line 109):
  ```js
  // 5b. User Profile (new)
  // Pattern: /u/:username — MUST appear BEFORE the 2-segment /:owner/:repo branch.
  // Safe to reserve "u": username regex ^[a-zA-Z0-9-]{3,20}$ (min 3 chars) means a
  // single-char first segment can never be a real owner.
  if (segments.length === 2 && segments[0] === 'u') {
    return { page: 'profile', params: { username: segments[1] } };
  }
  ```

- [ ] **Step 2: Add the `navigate` URL builder branch.** In the `navigate` `if/else if` chain (~line 211-216), add:
  ```js
    } else if (page === 'profile') {
      url = `/u/${params.username}`;
  ```

- [ ] **Step 3: Add the import.** In the import block (~line 10-19):
  ```js
  import Profile from './pages/Profile';
  ```

- [ ] **Step 4: Add the `renderView` case.** In the `switch (navigation.page)` block (~line 254-332), after `case 'security':`:
  ```jsx
      case 'profile':
        return (
          <Profile user={user} username={navigation.params.username} onNavigate={navigate} />
        );
  ```
  `renderView` already short-circuits to `<Login />` when `!user` (line 252), satisfying "Not signed in → Login" — no extra guard needed.

- [ ] **Step 5: Create `frontend/src/pages/Profile.jsx`.** Props `{ user, username, onNavigate }`. Fetches `/api/repos`, filters to `r.owner === username` (exact, case-sensitive). Email shows only on own profile; other users get a "public profiles pending" notice. The 200px avatar reuses `Avatar` so generation is identical to the small one:
  ```jsx
  import { useState, useEffect } from 'react';
  import { apiClient } from '../apiClient';
  import { AlertTriangle, Folder, Lock, Globe } from 'lucide-react';
  import Card from '../components/Card';
  import Chip from '../components/Chip';
  import SectionHead from '../components/SectionHead';
  import Avatar from '../components/Avatar';

  function relativeTime(ts) {
    if (!ts) return 'recently';
    const ms = ts.seconds ? ts.seconds * 1000 : Date.parse(ts);
    if (!ms || Number.isNaN(ms)) return 'recently';
    const diff = Date.now() - ms;
    const day = 86400000;
    if (diff < day) return 'today';
    if (diff < 2 * day) return 'yesterday';
    if (diff < 30 * day) return `${Math.floor(diff / day)}d ago`;
    return new Date(ms).toLocaleDateString();
  }

  const CANT_SHOW = [
    'Bio · location · links', 'Followers · following', 'Pinned repos',
    'Contribution heatmap', 'Activity timeline', 'Starred repos', 'Cross-repo events feed',
  ];

  export default function Profile({ user, username, onNavigate }) {
    const [repos, setRepos] = useState([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState('');
    const isSelf = !!(user && user.username && user.username === username);

    useEffect(() => {
      let cancelled = false;
      const load = async () => {
        try {
          setLoading(true);
          setError('');
          const data = await apiClient.get('/api/repos');
          const mine = (Array.isArray(data) ? data : []).filter((r) => r.owner === username);
          if (!cancelled) setRepos(mine);
        } catch (err) {
          if (!cancelled) setError(err.message || 'Failed to load repositories.');
        } finally {
          if (!cancelled) setLoading(false);
        }
      };
      load();
      return () => { cancelled = true; };
    }, [username]);

    return (
      <div style={{ maxWidth: 1080, margin: '0 auto', padding: '24px 0' }}>
        <div style={{ display: 'grid', gridTemplateColumns: '260px 1fr', gap: 28, alignItems: 'start' }}>
          <aside style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
            <div>
              <div style={{ marginBottom: 16 }}><Avatar name={username} size={200} /></div>
              <h1 style={{ fontSize: 20, fontWeight: 600, letterSpacing: '-0.015em', color: 'var(--gb-fg)', margin: 0 }}>{username}</h1>
              {isSelf && user.email && (
                <div style={{ fontSize: 12.5, color: 'var(--gb-fg-3)', marginTop: 4, fontFamily: 'var(--gb-mono)' }}>{user.email}</div>
              )}
            </div>
            <div style={{ background: 'var(--gb-surface)', border: '1px dashed var(--gb-line)', borderRadius: 8, padding: 10 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.04em', color: 'var(--gb-warn)', marginBottom: 6 }}>
                <AlertTriangle size={13} /> Today: profiles are paper-thin
              </div>
              <p style={{ fontSize: 12, color: 'var(--gb-fg-3)', lineHeight: 1.5, margin: 0 }}>
                We have <code style={{ fontFamily: 'var(--gb-mono)' }}>/api/user/me</code> (uid · email · username)
                and can filter <code style={{ fontFamily: 'var(--gb-mono)' }}>/api/repos</code> by owner.
                That's it. No bio, heatmap, followers, or activity without new endpoints.
              </p>
            </div>
          </aside>

          <div style={{ display: 'flex', flexDirection: 'column', gap: 24 }}>
            {!isSelf && (
              <div style={{ display: 'flex', alignItems: 'center', gap: 10, background: 'var(--gb-surface)', border: '1px dashed var(--gb-line)', borderRadius: 8, padding: '10px 14px', fontSize: 12.5, color: 'var(--gb-fg-3)' }}>
                <AlertTriangle size={14} style={{ color: 'var(--gb-warn)', flexShrink: 0 }} />
                <span>Public profiles aren't fully available yet — backend support is pending. You're seeing repositories owned by <strong style={{ color: 'var(--gb-fg-2)' }}>{username}</strong> that are visible to you.</span>
              </div>
            )}

            <div>
              <SectionHead kicker={`/api/repos · owner == "${username}"`} title="Repositories" />
              {error ? (
                <Card style={{ padding: 16 }}><div style={{ color: 'var(--gb-err)', fontSize: 13 }}>{error}</div></Card>
              ) : loading ? (
                <Card style={{ padding: 16 }}><div style={{ color: 'var(--gb-fg-3)', fontSize: 13 }}>Loading repositories…</div></Card>
              ) : repos.length === 0 ? (
                <Card style={{ padding: 16 }}><div style={{ color: 'var(--gb-fg-3)', fontSize: 13, textAlign: 'center', padding: '24px 0' }}>No public repositories yet.</div></Card>
              ) : (
                <Card style={{ padding: 0 }}>
                  {repos.map((r, i) => (
                    <div key={`${r.owner}/${r.name}`}
                         onClick={() => onNavigate('repository', { owner: r.owner, repo: r.name })}
                         style={{ display: 'grid', gridTemplateColumns: '20px 1fr 120px', gap: 14, alignItems: 'center', padding: '13px 16px', cursor: 'pointer', borderTop: i === 0 ? 'none' : '1px solid var(--gb-line)' }}>
                      {r.visibility === 'private' ? <Lock size={15} style={{ color: 'var(--gb-fg-3)' }} /> : <Globe size={15} style={{ color: 'var(--gb-fg-3)' }} />}
                      <div style={{ minWidth: 0 }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                          <Folder size={13} style={{ color: 'var(--gb-fg-3)', flexShrink: 0 }} />
                          <span style={{ fontSize: 13, fontWeight: 600, color: 'var(--gb-accent)' }}>{r.name}</span>
                        </div>
                        <div style={{ fontSize: 12, color: 'var(--gb-fg-3)', marginTop: 2, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                          {r.description || 'No description provided.'}
                        </div>
                      </div>
                      <div style={{ fontSize: 11.5, color: 'var(--gb-fg-4)', textAlign: 'right' }}>{relativeTime(r.updatedAt)}</div>
                    </div>
                  ))}
                </Card>
              )}
            </div>

            <div style={{ background: 'var(--gb-surface)', border: '1px dashed var(--gb-line)', borderRadius: 8, padding: 14 }}>
              <div style={{ fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.04em', color: 'var(--gb-fg-3)', marginBottom: 10 }}>What today can't show</div>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                {CANT_SHOW.map((label) => (<Chip key={label} variant="warn">{label}</Chip>))}
              </div>
            </div>
          </div>
        </div>
      </div>
    );
  }
  ```
  Trace check: `username` from the route; `email` from `user.email` (own profile only); every repo field (`name`, `owner`, `description`, `visibility`, `updatedAt` as Firestore `{seconds}`) is the exact shape `Dashboard.jsx` consumes from `/api/repos`. Confirm the logged-in `user` object exposes `.username` and `.email` (it's the same object `App.jsx` passes everywhere).

- [ ] **Step 6: Lint and build.** From `frontend/`: `npm run lint && npm run build`. Fix lint errors; build must emit `frontend/dist`.

- [ ] **Step 7: Screenshot verification under DEV_MODE.** Build, run the Go server with `DEV_MODE=true`, sign in as a mock user, navigate to `/u/<your-username>` (email + repos render) and `/u/aria-chen` (other-user pending notice). Screenshot each, compare to `design_handoff_today_v1/references/mock-profile.jsx`: 260px sidebar + 1fr, 28px gap; 200px avatar; dashed notice cards; amber warn chips. Confirm own profile shows email; other-user hides email + shows pending notice; empty-owner shows "No public repositories yet."; and existing routes (`/`, `/tokens`, `/{owner}/{repo}`) still resolve (route precedence intact).

- [ ] **Step 8: Commit** (on `feat/today-ui-refresh`):
  ```bash
  git add frontend/src/pages/Profile.jsx frontend/src/App.jsx
  git commit -m "$(cat <<'EOF'
  feat(ui): add user profile route /u/:username

  Adds a bare-honest Profile page: deterministic avatar, username, email
  (own profile only), and an owner-filtered repo list from /api/repos, plus a
  dashed "what today can't show" panel naming the backend gaps. Routes via
  /u/:username, inserted before the 2-segment /:owner/:repo branch ("u" is a
  safe reservation since usernames are >=3 chars). Other users see a "public
  profiles pending" notice. No backend changes.

  Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
  EOF
  )"
  ```

---

## Self-review notes (author)

- **Single-branch discipline:** Every commit step targets the existing `feat/today-ui-refresh` branch. No task creates its own branch.
- **Primitive API consistency:** All tasks import `Card`/`Chip`/`SectionHead`/`Avatar` from `../components/*` with the Task 1 signatures (`Chip variant=default|accent|ok|warn|err|merged`, `icon`, `dot`, children; `SectionHead kicker/title/right` with `title=""` allowed; `Avatar name/size`). There is no `rdy` Chip variant — RDY badges use `variant="ok"`.
- **Shared-file ordering:** Tasks 3, 4, and 5 all modify `Repository.jsx`. Execute them in order; `tags` state is introduced in Task 3 and reused (not redeclared) in Task 4. Re-read the file before each of these tasks since line numbers shift.
- **Cross-file nav keys verified against `App.jsx`:** PR detail links use `pull_detail` ({owner,repo,number}); build-logs links use `build_logs` ({owner,repo,sha,buildId}); repo links use `repository`; commit links use `commit`.
- **Open verification items for the implementer** (flagged inline, resolve during execution): `COMMITS_PAGE_SIZE` constant name (Task 4 — validation confirmed it exists at line 124, value 50).

## Validation pass (resolved against the Go backend)

Two validation subagents checked every anchor, endpoint, and field against the real `frontend/` and `internal/api/` sources. Confirmed accurate: all line anchors, nav keys (`pull_detail`/`build_logs`/`commit`/`repository`), the `/u/` routing-precedence reservation, `--gb-*` token/Avatar-palette values, and field shapes for repos, tags (`{name,sha}`), commits (`authorName/message/sha/date/overallStatus/statuses[].buildId`), reviews (`username/state∈{approved,changes_requested,commented}/submittedAt/body`), PRs (`status/requestedReviewers/authorUsername/targetBranch/mergeable`), branch-protection rules, collaborators (`uid/username/addedAt`, owner excluded), and codeowners (`{entries}`). Four blockers found and **fixed in this plan**:
1. Task 2: `/pulls` ignores `?status` server-side → now filters `status === 'open'` client-side.
2. Task 2: PR objects have no `checkStatus` → failed-author "Waiting on you" branch removed.
3. Task 3: `/refs/{branch}/head` has no `overallStatus` → LatestCommitBar build chip removed (restyle only).
4. Task 5: merge gate now ANDs `pr.mergeable === true`; the existing conflict/update merge box is preserved (callouts are additive).
