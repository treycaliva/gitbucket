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
              <span>Public profiles aren&apos;t fully available yet — backend support is pending. You&apos;re seeing repositories owned by <strong style={{ color: 'var(--gb-fg-2)' }}>{username}</strong> that are visible to you.</span>
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
            <div style={{ fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.04em', color: 'var(--gb-fg-3)', marginBottom: 10 }}>What today can&apos;t show</div>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
              {CANT_SHOW.map((label) => (<Chip key={label} variant="warn">{label}</Chip>))}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
