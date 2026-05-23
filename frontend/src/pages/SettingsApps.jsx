import { useEffect, useState } from 'react';
import { apiClient } from '../apiClient';

export default function SettingsApps({ onNavigate }) {
  const [owned, setOwned] = useState([]);
  const [installed, setInstalled] = useState([]);

  useEffect(() => {
    apiClient.listMyApps().then(setOwned).catch(() => setOwned([]));
    apiClient.listMyInstallations().then(setInstalled).catch(() => setInstalled([]));
  }, []);

  return (
    <div style={{ maxWidth: '720px', margin: '2rem auto', padding: '2rem', color: '#e2e8f0' }}>
      <h2>GitHub Apps</h2>

      <section>
        <h3>Owned by me</h3>
        {owned.length === 0 ? (
          <p style={{ color: '#94a3b8' }}>No Apps yet.</p>
        ) : (
          <ul>
            {owned.map(a => (
              <li key={a.id}>
                <a href={`/settings/apps/${a.slug}/installations/new`}>{a.name}</a>
                <span style={{ color: '#94a3b8' }}> ({a.slug})</span>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section style={{ marginTop: '2rem' }}>
        <h3>Installed on my account</h3>
        {installed.length === 0 ? (
          <p style={{ color: '#94a3b8' }}>No installations yet.</p>
        ) : (
          <ul>
            {installed.map(i => (
              <li key={i.id}>{i.app_name || i.app_id}</li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
