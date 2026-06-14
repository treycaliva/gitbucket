import { useEffect, useState } from 'react';
import { apiClient } from '../apiClient';
import { parseManifestFromURL } from '../utils/manifestUrl';
import { AppWindow } from 'lucide-react';
import Card from '../components/Card';
import PageHeader from '../components/PageHeader';

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
      <div>
        <PageHeader title="Invalid request">{error}</PageHeader>
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
      <PageHeader icon={<AppWindow size={18} style={{ color: 'var(--gb-accent)' }} />} title="Register GitHub App">
        <strong style={{ color: 'var(--gb-fg-2)' }}>{manifest.name}</strong> is requesting to register an App on your account ({user?.email || 'logged in user'}).
      </PageHeader>

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
          <div className="gb-error-banner" style={{ marginTop: 16 }}>
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
