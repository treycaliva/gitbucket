import { useState, useEffect } from 'react';
import { apiClient } from '../apiClient';
import { Plus, Folder, Globe, Lock, Search, Clock } from 'lucide-react';

export default function Dashboard({ user, onNavigate }) {
  const [repos, setRepos] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [search, setSearch] = useState('');
  
  // Create Repo Modal State
  const [showModal, setShowModal] = useState(false);
  const [newRepoName, setNewRepoName] = useState('');
  const [newRepoDesc, setNewRepoDesc] = useState('');
  const [newRepoVisibility, setNewRepoVisibility] = useState('public');
  const [modalLoading, setModalLoading] = useState(false);
  const [modalError, setModalError] = useState('');

  const loadRepos = async () => {
    try {
      setLoading(true);
      const data = await apiClient.get('/api/repos');
      setRepos(data);
    } catch (err) {
      console.error(err);
      setError(err.message || 'Failed to load repositories.');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    Promise.resolve().then(() => {
      loadRepos();
    });
  }, []);

  const handleCreateRepo = async (e) => {
    e.preventDefault();
    setModalError('');
    setModalLoading(true);

    try {
      await apiClient.post('/api/repos', {
        name: newRepoName,
        description: newRepoDesc,
        visibility: newRepoVisibility
      });
      
      // Reset & Close Modal
      setNewRepoName('');
      setNewRepoDesc('');
      setNewRepoVisibility('public');
      setShowModal(false);
      
      // Reload and Navigate
      await loadRepos();
      onNavigate('repository', { owner: user.username, repo: newRepoName });
    } catch (err) {
      setModalError(err.message || 'Failed to create repository.');
    } finally {
      setModalLoading(false);
    }
  };

  // Filtered repositories based on search
  const filteredRepos = repos.filter(repo => {
    const term = search.toLowerCase();
    return (
      repo.name.toLowerCase().includes(term) ||
      repo.owner.toLowerCase().includes(term) ||
      (repo.description && repo.description.toLowerCase().includes(term))
    );
  });

  return (
    <div>
      <div className="page-header">
        <div className="page-header-title">
          <h1 className="gradient-text">Your Repositories</h1>
          <p>Manage and browse your cloud-hosted Git repositories</p>
        </div>
        <div className="page-header-actions">
          <button className="btn btn-primary" onClick={() => setShowModal(true)}>
            <Plus size={18} />
            New Repository
          </button>
        </div>
      </div>

      <div style={{
        display: 'flex',
        gap: '1rem',
        marginBottom: '2rem'
      }}>
        <div style={{ position: 'relative', flex: 1 }}>
          <Search size={18} style={{
            position: 'absolute',
            left: '1rem',
            top: '50%',
            transform: 'translateY(-50%)',
            color: '#64748b'
          }} />
          <input
            type="text"
            className="text-input"
            style={{ paddingLeft: '2.75rem' }}
            placeholder="Search repositories..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
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

      {loading ? (
        <div className="loader-container">
          <div className="loader"></div>
        </div>
      ) : filteredRepos.length === 0 ? (
        <div className="glass-card" style={{
          textAlign: 'center',
          padding: '4rem 2rem',
          color: '#94a3b8',
          borderStyle: 'dashed'
        }}>
          <Folder size={48} style={{ color: '#475569', marginBottom: '1rem' }} />
          <h3 style={{ color: '#f8fafc', marginBottom: '0.5rem' }}>No repositories found</h3>
          <p style={{ marginBottom: '1.5rem', fontSize: '0.95rem' }}>
            {search ? 'No repositories match your search criteria.' : 'Get started by creating your first repository.'}
          </p>
          {!search && (
            <button className="btn btn-primary" onClick={() => setShowModal(true)}>
              <Plus size={18} />
              Create Repository
            </button>
          )}
        </div>
      ) : (
        <div style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fill, minmax(360px, 1fr))',
          gap: '1.5rem'
        }}>
          {filteredRepos.map(repo => {
            const isOwner = user && user.username && user.username.toLowerCase() === repo.owner.toLowerCase();
            return (
              <div 
                key={`${repo.owner}/${repo.name}`}
                className="glass-card hoverable"
                style={{ cursor: 'pointer', display: 'flex', flexDirection: 'column' }}
                onClick={() => onNavigate('repository', { owner: repo.owner, repo: repo.name })}
              >
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '0.75rem' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontWeight: 600, fontSize: '1.1rem' }}>
                    <span style={{ color: '#94a3b8' }}>{repo.owner}</span>
                    <span style={{ color: '#64748b' }}>/</span>
                    <span style={{ color: '#38bdf8' }}>{repo.name}</span>
                  </div>
                  <span className={`badge badge-${repo.visibility}`}>
                    {repo.visibility === 'private' ? <Lock size={12} style={{ marginRight: '0.25rem' }} /> : <Globe size={12} style={{ marginRight: '0.25rem' }} />}
                    {repo.visibility}
                  </span>
                </div>
                
                <p style={{
                  color: '#94a3b8',
                  fontSize: '0.9rem',
                  lineHeight: '1.5',
                  marginBottom: '1.5rem',
                  flex: 1,
                  display: '-webkit-box',
                  WebkitLineClamp: 2,
                  WebkitBoxOrient: 'vertical',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis'
                }}>
                  {repo.description || 'No description provided.'}
                </p>

                <div style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  fontSize: '0.8rem',
                  color: '#64748b',
                  borderTop: '1px solid rgba(255, 255, 255, 0.05)',
                  paddingTop: '0.75rem'
                }}>
                  <span style={{ display: 'flex', alignItems: 'center', gap: '0.25rem' }}>
                    <Clock size={12} />
                    Updated {repo.updatedAt ? new Date(repo.updatedAt.seconds * 1000).toLocaleDateString() : 'recently'}
                  </span>
                  {isOwner && (
                    <span style={{ color: 'rgba(56, 189, 248, 0.6)', fontWeight: 600 }}>Owner</span>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}

      {/* Create Repository Modal */}
      {showModal && (
        <div className="modal-overlay" onClick={() => !modalLoading && setShowModal(false)}>
          <div className="glass-card modal-content" onClick={e => e.stopPropagation()} style={{ animation: 'none' }}>
            <h2 style={{ fontSize: '1.5rem', marginBottom: '1.5rem' }}>Create New Repository</h2>
            
            {modalError && (
              <div style={{
                background: 'rgba(244, 63, 94, 0.1)',
                border: '1px solid rgba(244, 63, 94, 0.2)',
                color: '#fb7185',
                padding: '0.75rem 1rem',
                borderRadius: '8px',
                fontSize: '0.85rem',
                marginBottom: '1rem'
              }}>
                {modalError}
              </div>
            )}

            <form onSubmit={handleCreateRepo}>
              <div className="form-group">
                <label className="form-label">Repository Name</label>
                <input
                  type="text"
                  className="text-input"
                  placeholder="my-cool-project"
                  value={newRepoName}
                  onChange={e => setNewRepoName(e.target.value)}
                  disabled={modalLoading}
                  required
                />
                <span style={{ fontSize: '0.75rem', color: '#64748b', display: 'block', marginTop: '0.25rem' }}>
                  Alphanumeric, hyphens and underscores only (3-30 chars).
                </span>
              </div>

              <div className="form-group">
                <label className="form-label">Description (optional)</label>
                <textarea
                  className="text-input"
                  style={{ resize: 'vertical', minHeight: '80px', fontFamily: 'inherit' }}
                  placeholder="Brief summary of your project..."
                  value={newRepoDesc}
                  onChange={e => setNewRepoDesc(e.target.value)}
                  disabled={modalLoading}
                />
              </div>

              <div className="form-group" style={{ marginBottom: '2rem' }}>
                <label className="form-label">Visibility</label>
                <select
                  className="text-input"
                  value={newRepoVisibility}
                  onChange={e => setNewRepoVisibility(e.target.value)}
                  disabled={modalLoading}
                >
                  <option value="public">Public (Anyone can see this repository. Only you can push.)</option>
                  <option value="private">Private (Only you can see or push to this repository.)</option>
                </select>
              </div>

              <div style={{ display: 'flex', gap: '1rem', justifyContent: 'flex-end' }}>
                <button
                  type="button"
                  className="btn btn-secondary"
                  onClick={() => setShowModal(false)}
                  disabled={modalLoading}
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="btn btn-primary"
                  disabled={modalLoading}
                >
                  {modalLoading ? (
                    <span className="loader" style={{ width: '16px', height: '16px', borderWidth: '2px' }}></span>
                  ) : (
                    'Create Repository'
                  )}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
