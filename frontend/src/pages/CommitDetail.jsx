import { useState, useEffect } from 'react';
import { apiClient } from '../apiClient';
import { ArrowLeft, FileText } from 'lucide-react';
import Card from '../components/Card';

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
          color: 'var(--gb-fg-2)',
          display: 'flex',
          alignItems: 'center',
          gap: '0.25rem',
          cursor: 'pointer',
          fontSize: 13,
          fontWeight: 500,
          marginBottom: '1.25rem',
          padding: 0,
        }}
      >
        <ArrowLeft size={14} /> Back to commits
      </button>

      <div style={{ marginBottom: 24 }}>
        <h1 style={{ fontSize: 20, fontWeight: 600, letterSpacing: '-0.015em', color: 'var(--gb-fg)', margin: 0 }}>
          Commit{' '}
          <span style={{ color: 'var(--gb-accent)', fontFamily: 'var(--gb-mono)' }}>{sha.substring(0, 7)}</span>
        </h1>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: 13, color: 'var(--gb-fg-3)', marginTop: 6 }}>
          <span>Full SHA:</span>
          <span style={{
            fontFamily: 'var(--gb-mono)',
            background: 'var(--gb-accent-bg)',
            padding: '0.1rem 0.4rem',
            borderRadius: 6,
            color: 'var(--gb-accent)',
          }}>
            {sha}
          </span>
        </div>
      </div>

      {error ? (
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
      ) : (
        <Card style={{ padding: 0, overflow: 'hidden' }}>
          <div style={{
            background: 'var(--gb-surface-2)',
            borderBottom: '1px solid var(--gb-line)',
            padding: '0.75rem 1.25rem',
            display: 'flex',
            alignItems: 'center',
            gap: '0.5rem',
            color: 'var(--gb-fg-3)',
            fontSize: 13,
            fontWeight: 600,
          }}>
            <FileText size={16} style={{ color: 'var(--gb-accent)' }} />
            <span>Changeset details</span>
          </div>
          <div style={{ padding: '0.5rem 0' }}>
            {diff.split('\n').map((line, idx) => renderDiffLine(line, idx))}
          </div>
        </Card>
      )}
    </div>
  );
}
