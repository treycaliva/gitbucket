import { useState } from 'react';
import { apiClient } from '../apiClient';
import { parseManifestFromURL } from '../utils/manifestUrl';
import { AppWindow } from 'lucide-react';
import Card from '../components/Card';

export default function SettingsAppsNew({ user, onNavigate }) {
  const [manifest] = useState(() => {
    const m = parseManifestFromURL(window.location.search);
    return m || null;
  });
  const [error, setError] = useState(() => {
    const m = parseManifestFromURL(window.location.search);
    return !m ? 'Missing or invalid manifest in URL.' : null;
  });
  const [submitting, setSubmitting] = useState(false);

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
      <div>
        <div style={{ marginBottom: 24 }}>
          <h1 style={{ fontSize: 20, fontWeight: 600, letterSpacing: '-0.015em', color: 'var(--gb-fg)', margin: 0 }}>
            Invalid request
          </h1>
          <p style={{ fontSize: 13, color: 'var(--gb-fg-3)', marginTop: 6, maxWidth: 760, lineHeight: 1.5 }}>
            {error}
          </p>
        </div>
        <button className="btn btn-secondary" onClick={() => onNavigate('dashboard')}>
          Back to dashboard
        </button>
      </div>
    );
  }
  if (!manifest) {
    return (
      <div className="loader-container" style={{ padding: '3rem 0' }}>
        <div className="loader"></div>
      </div>
    );
  }

  const heading = { fontSize: 15, fontWeight: 600, color: 'var(--gb-fg)', margin: '20px 0 8px' };
  const codeStyle = { fontFamily: 'var(--gb-mono)', fontSize: 13, color: 'var(--gb-accent)', wordBreak: 'break-all' };

  return (
    <div>
      <div style={{ marginBottom: 24 }}>
        <h1 style={{ fontSize: 20, fontWeight: 600, letterSpacing: '-0.015em', color: 'var(--gb-fg)', margin: 0, display: 'flex', alignItems: 'center', gap: 9 }}>
          <AppWindow size={18} style={{ color: 'var(--gb-accent)' }} /> Register GitHub App
        </h1>
        <p style={{ fontSize: 13, color: 'var(--gb-fg-3)', marginTop: 6, maxWidth: 760, lineHeight: 1.5 }}>
          <strong style={{ color: 'var(--gb-fg-2)' }}>{manifest.name}</strong> is requesting to register an App on your account ({user?.email || 'logged in user'}).
        </p>
      </div>

      <Card style={{ padding: 16, maxWidth: 640 }}>
        <h3 style={{ ...heading, marginTop: 0 }}>App URL</h3>
        <code style={codeStyle}>{manifest.url}</code>

        <h3 style={heading}>Webhook URL</h3>
        <code style={codeStyle}>{manifest.hook_attributes?.url || '(none)'}</code>

        <h3 style={heading}>Permissions</h3>
        <ul style={{ paddingLeft: '1.25rem', margin: 0, fontSize: 13, color: 'var(--gb-fg-2)' }}>
          {Object.entries(manifest.default_permissions || {}).map(([scope, level]) => (
            <li key={scope} style={{ marginBottom: 4 }}>
              <code style={{ fontFamily: 'var(--gb-mono)', color: 'var(--gb-fg)' }}>{scope}</code>: <span style={{ color: 'var(--gb-warn)' }}>{level}</span>
            </li>
          ))}
        </ul>

        <h3 style={heading}>Events</h3>
        <ul style={{ paddingLeft: '1.25rem', margin: 0, fontSize: 13, color: 'var(--gb-fg-2)' }}>
          {(manifest.default_events || []).map(e => (
            <li key={e} style={{ marginBottom: 4 }}>
              <code style={{ fontFamily: 'var(--gb-mono)', color: 'var(--gb-fg)' }}>{e}</code>
            </li>
          ))}
        </ul>

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
          <button
            className="btn btn-primary"
            onClick={handleConfirm}
            disabled={submitting}
          >
            {submitting ? (
              <span className="loader" style={{ width: '16px', height: '16px', borderWidth: '2px' }}></span>
            ) : (
              `Create App on behalf of ${user?.email || 'me'}`
            )}
          </button>
          <button
            className="btn btn-secondary"
            onClick={() => onNavigate('dashboard')}
            disabled={submitting}
          >
            Cancel
          </button>
        </div>
      </Card>
    </div>
  );
}
