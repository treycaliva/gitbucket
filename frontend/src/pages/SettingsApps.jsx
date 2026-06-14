import { useEffect, useState } from 'react';
import { apiClient } from '../apiClient';
import { Boxes, Package, PackageCheck } from 'lucide-react';
import Card from '../components/Card';
import PageHeader from '../components/PageHeader';

export default function SettingsApps({ onNavigate }) {
  const [owned, setOwned] = useState([]);
  const [installed, setInstalled] = useState([]);

  useEffect(() => {
    apiClient.listMyApps().then(setOwned).catch(() => setOwned([]));
    apiClient.listMyInstallations().then(setInstalled).catch(() => setInstalled([]));
  }, []);

  return (
    <div>
      <PageHeader icon={<Boxes size={18} style={{ color: 'var(--gb-accent)' }} />} title="GitHub Apps">
        Manage the GitHub Apps you own and the installations enabled on your account.
      </PageHeader>

      <div style={{ display: 'flex', flexDirection: 'column', gap: 22 }}>
        <Card style={{ padding: 16 }}>
          <h3 style={{ fontSize: 15, fontWeight: 600, color: 'var(--gb-fg)', marginBottom: 16, display: 'flex', alignItems: 'center', gap: 8 }}>
            <Package size={16} style={{ color: 'var(--gb-accent)' }} />
            Owned by me ({owned.length})
          </h3>
          {owned.length === 0 ? (
            <div style={{ textAlign: 'center', padding: '2rem 1rem', color: 'var(--gb-fg-4)', fontSize: 13 }}>
              No Apps yet.
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
              {owned.map(a => (
                <div
                  key={a.id}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    padding: 14,
                    background: 'var(--gb-surface-2)',
                    border: '1px solid var(--gb-line)',
                    borderRadius: 8,
                  }}
                >
                  <a
                    href={`/settings/apps/${a.slug}/installations/new`}
                    style={{ fontWeight: 600, fontSize: 13.5, color: 'var(--gb-accent)', textDecoration: 'none' }}
                  >
                    {a.name}
                  </a>
                  <span style={{ fontFamily: 'var(--gb-mono)', fontSize: 11.5, color: 'var(--gb-fg-4)' }}>
                    {a.slug}
                  </span>
                </div>
              ))}
            </div>
          )}
        </Card>

        <Card style={{ padding: 16 }}>
          <h3 style={{ fontSize: 15, fontWeight: 600, color: 'var(--gb-fg)', marginBottom: 16, display: 'flex', alignItems: 'center', gap: 8 }}>
            <PackageCheck size={16} style={{ color: 'var(--gb-accent)' }} />
            Installed on my account ({installed.length})
          </h3>
          {installed.length === 0 ? (
            <div style={{ textAlign: 'center', padding: '2rem 1rem', color: 'var(--gb-fg-4)', fontSize: 13 }}>
              No installations yet.
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
              {installed.map(i => (
                <div
                  key={i.id}
                  style={{
                    padding: 14,
                    background: 'var(--gb-surface-2)',
                    border: '1px solid var(--gb-line)',
                    borderRadius: 8,
                    fontSize: 13.5,
                    color: 'var(--gb-fg)',
                  }}
                >
                  {i.app_name || i.app_id}
                </div>
              ))}
            </div>
          )}
        </Card>
      </div>
    </div>
  );
}
