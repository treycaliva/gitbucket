import { useEffect, useState } from 'react';
import { apiClient } from '../apiClient';

export default function SettingsAppsInstall({ slug, onNavigate }) {
  const [app, setApp] = useState(null);
  const [repos, setRepos] = useState([]);
  const [selection, setSelection] = useState('all');
  const [pickedRepos, setPickedRepos] = useState(new Set());
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState(null);

  useEffect(() => {
    apiClient.getAppBySlug(slug).then(setApp).catch(e => setError(e.message));
    apiClient.listMyRepos().then(r => setRepos(r || [])).catch(() => setRepos([]));
  }, [slug]);

  const toggleRepo = (repoId) => {
    const next = new Set(pickedRepos);
    if (next.has(repoId)) next.delete(repoId);
    else next.add(repoId);
    setPickedRepos(next);
  };

  const handleInstall = async () => {
    if (!app) return;
    if (selection === 'selected' && pickedRepos.size === 0) {
      setError('Pick at least one repository.');
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await apiClient.installApp({
        appId: app.id,
        repositorySelection: selection,
        repositoryIds: selection === 'selected' ? Array.from(pickedRepos) : [],
      });
      onNavigate('apps_list');
    } catch (e) {
      setError(e.message || 'Failed to install app');
      setSubmitting(false);
    }
  };

  if (!app && !error) {
    return <div style={{ padding: '2rem', color: '#94a3b8' }}>Loading…</div>;
  }
  if (error && !app) {
    return <div style={{ padding: '2rem', color: '#fca5a5' }}>{error}</div>;
  }

  return (
    <div style={{ maxWidth: '640px', margin: '2rem auto', padding: '2rem',
                  background: '#0f172a', border: '1px solid #334155', borderRadius: '12px',
                  color: '#e2e8f0' }}>
      <h2 style={{ marginTop: 0 }}>Install {app.name}</h2>
      <p>Repository access:</p>

      <label style={{ display: 'block', marginBottom: '0.5rem' }}>
        <input type="radio" name="sel" value="all"
          checked={selection === 'all'}
          onChange={() => setSelection('all')} /> All repositories
      </label>
      <label style={{ display: 'block', marginBottom: '0.5rem' }}>
        <input type="radio" name="sel" value="selected"
          checked={selection === 'selected'}
          onChange={() => setSelection('selected')} /> Only select repositories
      </label>

      {selection === 'selected' && (
        <div style={{ marginTop: '1rem', maxHeight: '300px', overflowY: 'auto',
                      border: '1px solid #334155', padding: '0.5rem', borderRadius: '6px' }}>
          {repos.map(r => {
            const repoId = `${r.owner}_${r.name}`;
            return (
              <label key={repoId} style={{ display: 'block', padding: '0.25rem 0' }}>
                <input type="checkbox" checked={pickedRepos.has(repoId)}
                  onChange={() => toggleRepo(repoId)} /> {r.owner}/{r.name}
              </label>
            );
          })}
          {repos.length === 0 && <span style={{ color: '#94a3b8' }}>No repositories.</span>}
        </div>
      )}

      {error && (
        <div style={{ marginTop: '1rem', padding: '0.75rem', background: '#7f1d1d',
                      color: '#fecaca', borderRadius: '6px' }}>
          {error}
        </div>
      )}

      <div style={{ marginTop: '1.5rem', display: 'flex', gap: '0.75rem' }}>
        <button onClick={handleInstall} disabled={submitting}
          style={{ background: '#22c55e', color: 'black', border: 'none',
                   padding: '0.75rem 1.5rem', borderRadius: '6px', fontWeight: 600,
                   cursor: submitting ? 'wait' : 'pointer' }}>
          {submitting ? 'Installing…' : 'Install'}
        </button>
        <button onClick={() => onNavigate('dashboard')}
          style={{ background: '#334155', color: '#e2e8f0', border: 'none',
                   padding: '0.75rem 1.5rem', borderRadius: '6px' }}>
          Cancel
        </button>
      </div>
    </div>
  );
}
