import { useEffect, useState } from 'react';
import { apiClient } from '../apiClient';
import { parseManifestFromURL } from '../utils/manifestUrl';

export default function SettingsAppsNew({ user, onNavigate }) {
  const [manifest, setManifest] = useState(null);
  const [error, setError] = useState(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    const m = parseManifestFromURL(window.location.search);
    if (!m) {
      setError('Missing or invalid manifest in URL.');
      return;
    }
    setManifest(m);
  }, []);

  const handleConfirm = async () => {
    setSubmitting(true);
    setError(null);
    try {
      const resp = await apiClient.submitManifestConversion(manifest);
      // Redirect the browser to the agent's redirect_url (which carries ?code=).
      window.location.assign(resp.redirect_url);
    } catch (e) {
      setError(e.message || 'Failed to create app');
      setSubmitting(false);
    }
  };

  if (error && !manifest) {
    return (
      <div style={{ padding: '2rem', color: '#fca5a5' }}>
        <h2>Invalid request</h2>
        <p>{error}</p>
        <button onClick={() => onNavigate('dashboard')}>Back to dashboard</button>
      </div>
    );
  }
  if (!manifest) {
    return <div style={{ padding: '2rem', color: '#94a3b8' }}>Loading manifest…</div>;
  }

  return (
    <div style={{ maxWidth: '640px', margin: '2rem auto', padding: '2rem',
                  background: '#0f172a', border: '1px solid #334155', borderRadius: '12px',
                  color: '#e2e8f0', fontFamily: 'system-ui' }}>
      <h2 style={{ marginTop: 0 }}>Register GitHub App</h2>
      <p>
        <strong>{manifest.name}</strong> is requesting to register an App on your account
        <span style={{ color: '#94a3b8' }}> ({user?.email || 'logged in user'})</span>.
      </p>

      <h3 style={{ fontSize: '1rem', color: '#cbd5e1' }}>App URL</h3>
      <code style={{ color: '#a5b4fc' }}>{manifest.url}</code>

      <h3 style={{ fontSize: '1rem', color: '#cbd5e1' }}>Webhook URL</h3>
      <code style={{ color: '#a5b4fc' }}>{manifest.hook_attributes?.url || '(none)'}</code>

      <h3 style={{ fontSize: '1rem', color: '#cbd5e1' }}>Permissions</h3>
      <ul style={{ paddingLeft: '1.25rem' }}>
        {Object.entries(manifest.default_permissions || {}).map(([scope, level]) => (
          <li key={scope}>
            <code>{scope}</code>: <span style={{ color: '#fde68a' }}>{level}</span>
          </li>
        ))}
      </ul>

      <h3 style={{ fontSize: '1rem', color: '#cbd5e1' }}>Events</h3>
      <ul style={{ paddingLeft: '1.25rem' }}>
        {(manifest.default_events || []).map(e => <li key={e}><code>{e}</code></li>)}
      </ul>

      {error && (
        <div style={{ marginTop: '1rem', padding: '0.75rem', background: '#7f1d1d',
                      color: '#fecaca', borderRadius: '6px' }}>
          {error}
        </div>
      )}

      <div style={{ marginTop: '1.5rem', display: 'flex', gap: '0.75rem' }}>
        <button
          onClick={handleConfirm}
          disabled={submitting}
          style={{ background: '#22c55e', color: 'black', border: 'none',
                   padding: '0.75rem 1.5rem', borderRadius: '6px', fontWeight: 600,
                   cursor: submitting ? 'wait' : 'pointer' }}>
          {submitting ? 'Creating…' : `Create App on behalf of ${user?.email || 'me'}`}
        </button>
        <button
          onClick={() => onNavigate('dashboard')}
          disabled={submitting}
          style={{ background: '#334155', color: '#e2e8f0', border: 'none',
                   padding: '0.75rem 1.5rem', borderRadius: '6px' }}>
          Cancel
        </button>
      </div>
    </div>
  );
}
