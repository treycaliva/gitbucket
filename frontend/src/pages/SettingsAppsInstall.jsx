import { useEffect, useState } from 'react';
import { apiClient } from '../apiClient';
import { Download } from 'lucide-react';
import Card from '../components/Card';

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
    return (
      <div className="loader-container" style={{ padding: '3rem 0' }}>
        <div className="loader"></div>
      </div>
    );
  }
  if (error && !app) {
    return (
      <div style={{
        background: 'var(--gb-err-dim)',
        border: '1px solid rgba(248,113,113,0.25)',
        color: 'var(--gb-err)',
        padding: '12px 14px',
        borderRadius: 8,
        fontSize: 13,
      }}>
        {error}
      </div>
    );
  }

  const radioLabel = { display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10, fontSize: 13.5, color: 'var(--gb-fg-2)', cursor: 'pointer' };

  return (
    <div>
      <div style={{ marginBottom: 24 }}>
        <h1 style={{ fontSize: 20, fontWeight: 600, letterSpacing: '-0.015em', color: 'var(--gb-fg)', margin: 0, display: 'flex', alignItems: 'center', gap: 9 }}>
          <Download size={18} style={{ color: 'var(--gb-accent)' }} /> Install {app.name}
        </h1>
        <p style={{ fontSize: 13, color: 'var(--gb-fg-3)', marginTop: 6, maxWidth: 760, lineHeight: 1.5 }}>
          Choose which repositories this App can access on your account.
        </p>
      </div>

      <Card style={{ padding: 16, maxWidth: 640 }}>
        <h3 style={{ fontSize: 15, fontWeight: 600, color: 'var(--gb-fg)', marginTop: 0, marginBottom: 16 }}>
          Repository access
        </h3>

        <label style={radioLabel}>
          <input type="radio" name="sel" value="all"
            checked={selection === 'all'}
            onChange={() => setSelection('all')} /> All repositories
        </label>
        <label style={radioLabel}>
          <input type="radio" name="sel" value="selected"
            checked={selection === 'selected'}
            onChange={() => setSelection('selected')} /> Only select repositories
        </label>

        {selection === 'selected' && (
          <div style={{
            marginTop: 12,
            maxHeight: 300,
            overflowY: 'auto',
            background: 'var(--gb-surface-2)',
            border: '1px solid var(--gb-line)',
            padding: 12,
            borderRadius: 8,
          }}>
            {repos.map(r => {
              const repoId = `${r.owner}_${r.name}`;
              return (
                <label key={repoId} style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '5px 0', fontSize: 13, color: 'var(--gb-fg-2)', cursor: 'pointer' }}>
                  <input type="checkbox" checked={pickedRepos.has(repoId)}
                    onChange={() => toggleRepo(repoId)} /> {r.owner}/{r.name}
                </label>
              );
            })}
            {repos.length === 0 && <span style={{ color: 'var(--gb-fg-4)', fontSize: 13 }}>No repositories.</span>}
          </div>
        )}

        {error && (
          <div style={{
            marginTop: 16,
            background: 'var(--gb-err-dim)',
            border: '1px solid rgba(248,113,113,0.25)',
            color: 'var(--gb-err)',
            padding: '12px 14px',
            borderRadius: 8,
            fontSize: 13,
          }}>
            {error}
          </div>
        )}

        <div style={{ marginTop: 22, display: 'flex', gap: 12 }}>
          <button className="btn btn-primary" onClick={handleInstall} disabled={submitting}>
            {submitting ? (
              <span className="loader" style={{ width: '16px', height: '16px', borderWidth: '2px' }}></span>
            ) : (
              'Install'
            )}
          </button>
          <button className="btn btn-secondary" onClick={() => onNavigate('dashboard')} disabled={submitting}>
            Cancel
          </button>
        </div>
      </Card>
    </div>
  );
}
