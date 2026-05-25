import { useState, useEffect } from 'react';
import { apiClient } from '../apiClient';
import { Key, Plus, Trash2, ShieldAlert, Check, Copy } from 'lucide-react';
import Card from '../components/Card';

export default function Tokens() {
  const [tokens, setTokens] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  // New Token State
  const [tokenName, setTokenName] = useState('');
  const [generating, setGenerating] = useState(false);
  const [newlyCreatedToken, setNewlyCreatedToken] = useState(null);
  const [copied, setCopied] = useState(false);

  const loadTokens = async () => {
    try {
      setLoading(true);
      const data = await apiClient.get('/api/tokens');
      setTokens(data);
    } catch (err) {
      console.error(err);
      setError(err.message || 'Failed to load tokens.');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    Promise.resolve().then(() => {
      loadTokens();
    });
  }, []);

  const handleCreateToken = async (e) => {
    e.preventDefault();
    if (!tokenName.trim()) return;

    setGenerating(true);
    setError('');
    setNewlyCreatedToken(null);

    try {
      const data = await apiClient.post('/api/tokens', { name: tokenName.trim() });
      setNewlyCreatedToken(data.token);
      setTokenName('');
      await loadTokens();
    } catch (err) {
      setError(err.message || 'Failed to generate token.');
    } finally {
      setGenerating(false);
    }
  };

  const handleRevokeToken = async (tokenId) => {
    if (!window.confirm('Are you sure you want to revoke this Personal Access Token? Any git client using this token will lose access immediately.')) {
      return;
    }

    try {
      await apiClient.delete(`/api/tokens/${tokenId}`);
      await loadTokens();
    } catch (err) {
      setError(err.message || 'Failed to revoke token.');
    }
  };

  const copyToken = () => {
    if (!newlyCreatedToken) return;
    navigator.clipboard.writeText(newlyCreatedToken);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div>
      <div style={{ marginBottom: 24 }}>
        <h1 style={{ fontSize: 20, fontWeight: 600, letterSpacing: '-0.015em', color: 'var(--gb-fg)', margin: 0, display: 'flex', alignItems: 'center', gap: 9 }}>
          <Key size={18} style={{ color: 'var(--gb-accent)' }} /> Personal access tokens
        </h1>
        <p style={{ fontSize: 13, color: 'var(--gb-fg-3)', marginTop: 6, maxWidth: 760, lineHeight: 1.5 }}>
          Generate secure tokens to authenticate git CLI clone, push and pull operations over HTTPS.
        </p>
      </div>

      {error && (
        <div style={{
          background: 'var(--gb-err-dim)',
          border: '1px solid rgba(248,113,113,0.25)',
          color: 'var(--gb-err)',
          padding: '12px 14px',
          borderRadius: 8,
          marginBottom: 22,
          fontSize: 13,
        }}>
          {error}
        </div>
      )}

      {/* Newly Created Token Presentation */}
      {newlyCreatedToken && (
        <Card style={{
          borderColor: 'rgba(74,222,128,0.25)',
          background: 'var(--gb-ok-dim)',
          marginBottom: 22,
          padding: 16,
          display: 'flex',
          flexDirection: 'column',
          gap: 12,
        }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, color: 'var(--gb-ok)', fontWeight: 600, fontSize: 14 }}>
            <ShieldAlert size={18} />
            <span>Token generated successfully</span>
          </div>

          <p style={{ color: 'var(--gb-fg-3)', fontSize: 12.5, lineHeight: 1.5, margin: 0 }}>
            Make sure to copy your Personal Access Token now. You will <strong style={{ color: 'var(--gb-fg-2)' }}>not</strong> be able to see it again once you refresh or navigate away.
          </p>

          <div style={{
            display: 'flex',
            alignItems: 'center',
            gap: 12,
            background: 'var(--gb-page)',
            padding: '10px 14px',
            borderRadius: 6,
            border: '1px solid rgba(74,222,128,0.2)',
          }}>
            <span style={{
              fontFamily: 'var(--gb-mono)',
              color: 'var(--gb-fg)',
              fontSize: 13.5,
              flex: 1,
              wordBreak: 'break-all',
            }}>
              {newlyCreatedToken}
            </span>
            <button
              className="btn btn-secondary btn-icon"
              onClick={copyToken}
              style={{ borderColor: copied ? 'var(--gb-ok)' : 'var(--gb-line)', color: copied ? 'var(--gb-ok)' : 'var(--gb-accent)' }}
            >
              {copied ? <Check size={18} /> : <Copy size={18} />}
            </button>
          </div>
        </Card>
      )}

      <div style={{
        display: 'grid',
        gridTemplateColumns: '1fr 2fr',
        gap: 22,
        alignItems: 'start',
        flexWrap: 'wrap',
      }}>
        {/* Create Token Box */}
        <Card style={{ padding: 16 }}>
          <h3 style={{ fontSize: 15, fontWeight: 600, color: 'var(--gb-fg)', marginBottom: 16, display: 'flex', alignItems: 'center', gap: 8 }}>
            <Plus size={16} style={{ color: 'var(--gb-accent)' }} />
            Generate new token
          </h3>
          <form onSubmit={handleCreateToken}>
            <div className="form-group" style={{ marginBottom: 18 }}>
              <label className="form-label">Token description</label>
              <input
                type="text"
                className="text-input"
                placeholder="e.g. Work MacBook Pro"
                value={tokenName}
                onChange={e => setTokenName(e.target.value)}
                disabled={generating}
                required
              />
            </div>
            <button type="submit" className="btn btn-primary" style={{ width: '100%' }} disabled={generating}>
              {generating ? (
                <span className="loader" style={{ width: '16px', height: '16px', borderWidth: '2px' }}></span>
              ) : (
                'Generate token'
              )}
            </button>
          </form>
        </Card>

        {/* Tokens List Box */}
        <Card style={{ padding: 16 }}>
          <h3 style={{ fontSize: 15, fontWeight: 600, color: 'var(--gb-fg)', marginBottom: 16, display: 'flex', alignItems: 'center', gap: 8 }}>
            <Key size={16} style={{ color: 'var(--gb-accent)' }} />
            Active tokens ({tokens.length})
          </h3>

          {loading ? (
            <div className="loader-container" style={{ padding: '2rem 0' }}>
              <div className="loader"></div>
            </div>
          ) : tokens.length === 0 ? (
            <div style={{ textAlign: 'center', padding: '3rem 1rem', color: 'var(--gb-fg-4)', fontSize: 13 }}>
              You don't have any Personal Access Tokens yet. Generate one to use the Git CLI client.
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
              {tokens.map(token => (
                <div
                  key={token.id}
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
                  <div>
                    <h4 style={{ fontWeight: 600, fontSize: 13.5, color: 'var(--gb-fg)', marginBottom: 4 }}>
                      {token.name}
                    </h4>
                    <div style={{ display: 'flex', gap: 14, fontSize: 11.5, color: 'var(--gb-fg-4)' }}>
                      <span>Created: {token.createdAt ? new Date(token.createdAt).toLocaleDateString() : 'N/A'}</span>
                      <span>Last used: {token.lastUsedAt ? new Date(token.lastUsedAt).toLocaleDateString() : 'Never'}</span>
                    </div>
                  </div>
                  <button
                    className="btn btn-danger btn-icon"
                    onClick={() => handleRevokeToken(token.id)}
                    title="Revoke Token"
                  >
                    <Trash2 size={16} />
                  </button>
                </div>
              ))}
            </div>
          )}
        </Card>
      </div>
    </div>
  );
}
