import { useState, useEffect } from 'react';
import { apiClient } from '../apiClient';
import { ArrowLeft, Clock, FileText } from 'lucide-react';

export default function CommitDetail({ owner, repo, sha, onNavigate }) {
  const [diff, setDiff] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    const loadDiff = async () => {
      try {
        setLoading(true);
        const data = await apiClient.get(`/api/repos/${owner}/${repo}/commit/${sha}`);
        setDiff(data.rawDiff);
      } catch (err) {
        console.error(err);
        setError(err.message || 'Failed to load commit diff details.');
      } finally {
        setLoading(false);
      }
    };

    loadDiff();
  }, [owner, repo, sha]);

  const renderDiffLine = (line, idx) => {
    let className = 'diff-line';
    if (line.startsWith('+++') || line.startsWith('---')) {
      className += ' diff-line-meta';
    } else if (line.startsWith('+')) {
      className += ' diff-line-add';
    } else if (line.startsWith('-')) {
      className += ' diff-line-del';
    } else if (line.startsWith('@@')) {
      className += ' diff-line-header';
    } else if (line.startsWith('diff --git') || line.startsWith('index ')) {
      className += ' diff-line-meta';
    }
    
    return (
      <div key={idx} className={className}>
        {line}
      </div>
    );
  };

  if (loading) {
    return (
      <div className="loader-container">
        <div className="loader"></div>
      </div>
    );
  }

  return (
    <div>
      <button 
        onClick={() => onNavigate('repository', { owner, repo, tab: 'commits' })} 
        style={{ 
          background: 'none', 
          border: 'none', 
          color: '#94a3b8', 
          display: 'flex', 
          alignItems: 'center', 
          gap: '0.25rem',
          cursor: 'pointer',
          fontSize: '0.9rem',
          fontWeight: 500,
          marginBottom: '1.25rem'
        }}
      >
        <ArrowLeft size={14} /> Back to commits
      </button>

      <div className="page-header">
        <div className="page-header-title">
          <h1>
            Commit <span style={{ color: '#38bdf8', fontFamily: 'var(--font-mono)' }}>{sha.substring(0, 7)}</span>
          </h1>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.9rem', color: '#94a3b8', marginTop: '0.5rem' }}>
            <Clock size={16} />
            <span>Full SHA: </span>
            <span style={{ fontFamily: 'var(--font-mono)', background: 'rgba(255,255,255,0.06)', padding: '0.1rem 0.3rem', borderRadius: '4px', color: '#e2e8f0' }}>
              {sha}
            </span>
          </div>
        </div>
      </div>

      {error ? (
        <div className="glass-card" style={{ color: '#fb7185', borderColor: 'var(--error)' }}>
          {error}
        </div>
      ) : (
        <div className="diff-container">
          <div style={{
            background: 'rgba(255, 255, 255, 0.02)',
            borderBottom: '1px solid var(--border-color)',
            padding: '0.75rem 1.25rem',
            display: 'flex',
            alignItems: 'center',
            gap: '0.5rem',
            color: '#94a3b8',
            fontWeight: 500
          }}>
            <FileText size={16} />
            <span>Changeset details</span>
          </div>
          <div style={{ padding: '0.5rem 0' }}>
            {diff.split('\n').map((line, idx) => renderDiffLine(line, idx))}
          </div>
        </div>
      )}
    </div>
  );
}
