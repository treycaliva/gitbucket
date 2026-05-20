import { useState, useEffect } from 'react';
import { apiClient } from '../apiClient';
import { Key, Plus, Trash2, ShieldAlert, Check, Copy } from 'lucide-react';

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
      <div className="page-header">
        <div className="page-header-title">
          <h1 className="gradient-text">Personal Access Tokens</h1>
          <p>Generate secure tokens to authenticate git CLI clone, push and pull operations over HTTPS</p>
        </div>
      </div>

      {error && (
        <div style={{
          background: 'rgba(244, 63, 94, 0.1)',
          border: '1px solid rgba(244, 63, 94, 0.2)',
          color: '#fb7185',
          padding: '1rem',
          borderRadius: '8px',
          marginBottom: '2rem'
        }}>
          {error}
        </div>
      )}

      {/* Newly Created Token Presentation */}
      {newlyCreatedToken && (
        <div className="glass-card" style={{ 
          borderColor: '#10b981', 
          background: 'rgba(16, 185, 129, 0.08)',
          marginBottom: '2rem',
          display: 'flex',
          flexDirection: 'column',
          gap: '1rem'
        }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', color: '#10b981', fontWeight: 700 }}>
            <ShieldAlert size={20} />
            <span>Token Generated Successfully!</span>
          </div>
          
          <p style={{ color: '#94a3b8', fontSize: '0.9rem', lineHeight: '1.4' }}>
            Make sure to copy your Personal Access Token now. You will <strong>not</strong> be able to see it again once you refresh or navigate away!
          </p>

          <div style={{
            display: 'flex',
            alignItems: 'center',
            gap: '1rem',
            background: 'rgba(0,0,0,0.3)',
            padding: '0.75rem 1.25rem',
            borderRadius: '6px',
            border: '1px solid rgba(16, 185, 129, 0.2)'
          }}>
            <span style={{ 
              fontFamily: 'var(--font-mono)', 
              color: '#f8fafc',
              fontSize: '1rem',
              flex: 1,
              wordBreak: 'break-all'
            }}>
              {newlyCreatedToken}
            </span>
            <button 
              className="btn btn-secondary btn-icon" 
              onClick={copyToken}
              style={{ borderColor: copied ? '#10b981' : 'var(--border-color)', color: copied ? '#10b981' : '#38bdf8' }}
            >
              {copied ? <Check size={18} /> : <Copy size={18} />}
            </button>
          </div>
        </div>
      )}

      <div style={{
        display: 'grid',
        gridTemplateColumns: '1fr 2fr',
        gap: '2rem',
        alignItems: 'start',
        flexWrap: 'wrap'
      }}>
        {/* Create Token Box */}
        <div className="glass-card">
          <h3 style={{ fontSize: '1.25rem', marginBottom: '1.25rem', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <Plus size={18} style={{ color: '#38bdf8' }} />
            Generate New Token
          </h3>
          <form onSubmit={handleCreateToken}>
            <div className="form-group" style={{ marginBottom: '1.5rem' }}>
              <label className="form-label">Token Description</label>
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
                'Generate Token'
              )}
            </button>
          </form>
        </div>

        {/* Tokens List Box */}
        <div className="glass-card">
          <h3 style={{ fontSize: '1.25rem', marginBottom: '1.25rem', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <Key size={18} style={{ color: '#38bdf8' }} />
            Active Tokens ({tokens.length})
          </h3>

          {loading ? (
            <div className="loader-container" style={{ padding: '2rem 0' }}>
              <div className="loader"></div>
            </div>
          ) : tokens.length === 0 ? (
            <div style={{ textAlign: 'center', padding: '3rem 1rem', color: '#64748b', fontSize: '0.95rem' }}>
              You don't have any Personal Access Tokens yet. Generate one to use the Git CLI client.
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              {tokens.map(token => (
                <div 
                  key={token.id}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    padding: '1rem',
                    background: 'rgba(15, 23, 42, 0.4)',
                    border: '1px solid var(--border-color)',
                    borderRadius: '8px'
                  }}
                >
                  <div>
                    <h4 style={{ fontWeight: 600, fontSize: '0.95rem', color: '#f8fafc', marginBottom: '0.25rem' }}>
                      {token.name}
                    </h4>
                    <div style={{ display: 'flex', gap: '1rem', fontSize: '0.75rem', color: '#64748b' }}>
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
        </div>
      </div>
    </div>
  );
}
