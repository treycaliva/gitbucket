import { useState, useEffect, useMemo, useCallback } from 'react';
import { apiClient } from '../apiClient';
import { Plus, Globe, Lock, Search, History, GitPullRequest, Pin, Folder } from 'lucide-react';
import Card from '../components/Card';
import SectionHead from '../components/SectionHead';

// ── Module-scope helpers ──────────────────────────────────────────────────────

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

// Concurrency-limited async mapper (no new deps). Keeps the /pulls fan-out
// bounded so hundreds of repos don't fire hundreds of parallel requests.
async function mapLimit(items, limit, fn) {
  const results = new Array(items.length);
  let i = 0;
  async function worker() {
    while (i < items.length) {
      const idx = i++;
      results[idx] = await fn(items[idx], idx);
    }
  }
  await Promise.all(Array.from({ length: Math.min(limit, items.length) }, worker));
  return results;
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

// ── Component ─────────────────────────────────────────────────────────────────

export default function Dashboard({ user, onNavigate }) {
  const [repos, setRepos] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [search, setSearch] = useState('');

  // Create Repo Modal State
  const [showModal, setShowModal] = useState(false);
  const [newRepoName, setNewRepoName] = useState('');
  const [newRepoDesc, setNewRepoDesc] = useState('');
  const [newRepoVisibility, setNewRepoVisibility] = useState('public');
  const [modalLoading, setModalLoading] = useState(false);
  const [modalError, setModalError] = useState('');

  // New state for refresh
  const [openPRCount, setOpenPRCount] = useState({}); // { "owner/repo": number | '?' }
  const [waitingOnMe, setWaitingOnMe] = useState([]);
  const [pinned, setPinned] = useState(() => readSlugs(PIN_KEY));
  const [recent] = useState(() => readSlugs(RECENT_KEY));
  const [typeFilter, setTypeFilter] = useState('all');
  const [sort, setSort] = useState('updated');

  // Capture username as a stable string so the effect chain doesn't re-fire on
  // a new `user` object reference (which would trigger a second N+1 burst).
  const username = user?.username;

  const loadPullsAndWaiting = useCallback(async (repoList) => {
    if (!repoList.length) { setOpenPRCount({}); setWaitingOnMe([]); return; }
    const results = await mapLimit(repoList, 8, (r) =>
      apiClient
        .get(`/api/repos/${r.owner}/${r.name}/pulls`)
        .then((pulls) => ({ r, pulls: Array.isArray(pulls) ? pulls : [], ok: true }))
        .catch(() => ({ r, pulls: [], ok: false }))
    );
    const counts = {};
    const waiting = [];
    const me = username;
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
  }, [username]);

  const loadRepos = useCallback(async () => {
    try {
      setLoading(true);
      const data = await apiClient.get('/api/repos');
      setRepos(data);
      loadPullsAndWaiting(data); // fire-and-forget; tolerates partial failure internally
    } catch (err) {
      console.error(err);
      setError(err.message || 'Failed to load repositories.');
    } finally {
      setLoading(false);
    }
  }, [loadPullsAndWaiting]);

  useEffect(() => {
    Promise.resolve().then(() => {
      loadRepos();
    });
  }, [loadRepos]);

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

  const handleCreateRepo = async (e) => {
    e.preventDefault();
    setModalError('');
    setModalLoading(true);

    try {
      await apiClient.post('/api/repos', {
        name: newRepoName,
        description: newRepoDesc,
        visibility: newRepoVisibility
      });

      // Reset & Close Modal
      setNewRepoName('');
      setNewRepoDesc('');
      setNewRepoVisibility('public');
      setShowModal(false);

      // Reload and Navigate
      await loadRepos();
      onNavigate('repository', { owner: user.username, repo: newRepoName });
    } catch (err) {
      setModalError(err.message || 'Failed to create repository.');
    } finally {
      setModalLoading(false);
    }
  };

  // Derived values for header
  const today = new Date().toLocaleDateString('en-US', {
    weekday: 'short', month: 'short', day: 'numeric', year: 'numeric',
  }).toUpperCase();
  const hour = new Date().getHours();
  const greet = hour < 12 ? 'Morning' : hour < 18 ? 'Afternoon' : 'Evening';
  // waitingOnMe already contains only open PRs where you're a requested reviewer.
  const reviewCount = waitingOnMe.length;

  return (
    <div>
      <header style={{ display: 'flex', alignItems: 'flex-end', justifyContent: 'space-between', marginBottom: 22 }}>
        <div>
          <div style={{ fontFamily: 'var(--gb-mono)', fontSize: 10.5, color: 'var(--gb-fg-4)', letterSpacing: '0.05em' }}>{today}</div>
          <h1 style={{ fontSize: 24, fontWeight: 500, letterSpacing: '-0.02em', margin: '2px 0 0' }}>
            {greet}, <span style={{ color: 'var(--gb-accent)' }}>{username}</span>.
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

      {loading ? (
        <div className="loader-container"><div className="loader" /></div>
      ) : (
        <>
          {error && (
            <Card style={{ borderColor: 'var(--gb-err-dim)', color: 'var(--gb-err)', padding: '1rem', marginBottom: 16 }}>{error}</Card>
          )}
          {repos.length === 0 ? (
        <Card style={{ textAlign: 'center', padding: '4rem 2rem', borderStyle: 'dashed' }}>
          <Folder size={48} style={{ color: 'var(--gb-fg-4)', marginBottom: '1rem' }} />
          <h3 style={{ color: 'var(--gb-fg)', marginBottom: '0.5rem' }}>No repositories yet</h3>
          <p style={{ marginBottom: '1.5rem', fontSize: '0.95rem', color: 'var(--gb-fg-3)' }}>Get started by creating your first repository.</p>
          <button className="btn btn-primary" onClick={() => setShowModal(true)}><Plus size={18} /> Create Repository</button>
        </Card>
          ) : (
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 320px', gap: 22 }}>
            {/* LEFT COLUMN */}
            <div>
              {/* Step 11: Pinned grid */}
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

              {/* Step 12: All repositories section */}
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
            </div>

            {/* RIGHT ASIDE */}
            <aside style={{ display: 'flex', flexDirection: 'column', gap: 22 }}>
              {/* Step 13: Waiting on you */}
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

              {/* Step 14: Recently visited */}
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
            </aside>
          </div>
          )}
        </>
      )}

      {/* Create Repository Modal */}
      {showModal && (
        <div className="modal-overlay" onClick={() => !modalLoading && setShowModal(false)}>
          <div className="glass-card modal-content" onClick={e => e.stopPropagation()} style={{ animation: 'none' }}>
            <h2 style={{ fontSize: '1.5rem', marginBottom: '1.5rem' }}>Create New Repository</h2>

            {modalError && (
              <div style={{
                background: 'rgba(244, 63, 94, 0.1)',
                border: '1px solid rgba(244, 63, 94, 0.2)',
                color: '#fb7185',
                padding: '0.75rem 1rem',
                borderRadius: '8px',
                fontSize: '0.85rem',
                marginBottom: '1rem'
              }}>
                {modalError}
              </div>
            )}

            <form onSubmit={handleCreateRepo}>
              <div className="form-group">
                <label className="form-label">Repository Name</label>
                <input
                  type="text"
                  className="text-input"
                  placeholder="my-cool-project"
                  value={newRepoName}
                  onChange={e => setNewRepoName(e.target.value)}
                  disabled={modalLoading}
                  required
                />
                <span style={{ fontSize: '0.75rem', color: '#64748b', display: 'block', marginTop: '0.25rem' }}>
                  Alphanumeric, hyphens and underscores only (3-30 chars).
                </span>
              </div>

              <div className="form-group">
                <label className="form-label">Description (optional)</label>
                <textarea
                  className="text-input"
                  style={{ resize: 'vertical', minHeight: '80px', fontFamily: 'inherit' }}
                  placeholder="Brief summary of your project..."
                  value={newRepoDesc}
                  onChange={e => setNewRepoDesc(e.target.value)}
                  disabled={modalLoading}
                />
              </div>

              <div className="form-group" style={{ marginBottom: '2rem' }}>
                <label className="form-label">Visibility</label>
                <select
                  className="text-input"
                  value={newRepoVisibility}
                  onChange={e => setNewRepoVisibility(e.target.value)}
                  disabled={modalLoading}
                >
                  <option value="public">Public (Anyone can see this repository. Only you can push.)</option>
                  <option value="private">Private (Only you can see or push to this repository.)</option>
                </select>
              </div>

              <div style={{ display: 'flex', gap: '1rem', justifyContent: 'flex-end' }}>
                <button
                  type="button"
                  className="btn btn-secondary"
                  onClick={() => setShowModal(false)}
                  disabled={modalLoading}
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="btn btn-primary"
                  disabled={modalLoading}
                >
                  {modalLoading ? (
                    <span className="loader" style={{ width: '16px', height: '16px', borderWidth: '2px' }}></span>
                  ) : (
                    'Create Repository'
                  )}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
