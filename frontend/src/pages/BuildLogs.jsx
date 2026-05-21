import { useState, useEffect, useRef } from 'react';
import { apiClient } from '../apiClient';
import { ArrowLeft, ExternalLink, Terminal, Shield, FileText, CheckCircle2, XCircle, AlertCircle, RefreshCw } from 'lucide-react';

export default function BuildLogs({ owner, repo, sha, buildId, onNavigate }) {
  const [status, setStatus] = useState('LOADING');
  const [logs, setLogs] = useState([]);
  const [error, setError] = useState('');
  const [signedUrl, setSignedUrl] = useState('');
  const logEndRef = useRef(null);
  const wsRef = useRef(null);

  const fetchSignedUrl = async () => {
    try {
      const data = await apiClient.get(`/api/repos/${owner}/${repo}/builds/${buildId}/logs`);
      setSignedUrl(data.signedUrl);
    } catch (err) {
      console.error("Failed to fetch GCS log signed URL:", err);
    }
  };

  useEffect(() => {
    let active = true;

    const connectLogsStream = async () => {
      try {
        setStatus('CONNECTING');
        // 1. Get HMAC signed ticket for WebSocket
        const ticketData = await apiClient.post(`/api/repos/${owner}/${repo}/builds/ticket`, { buildId });
        const ticket = ticketData.ticket;

        // 2. Establish WebSocket connection
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const host = window.location.host;
        const wsUrl = `${protocol}//${host}/api/repos/${owner}/${repo}/builds/${buildId}/stream?ticket=${encodeURIComponent(ticket)}`;

        const ws = new WebSocket(wsUrl);
        wsRef.current = ws;

        ws.onopen = () => {
          if (!active) return;
          setStatus('STREAMING');
          setLogs([]);
        };

        ws.onmessage = (event) => {
          if (!active) return;
          const line = event.data;
          if (line === '[EOF]') {
            setStatus('COMPLETED');
            ws.close();
            fetchSignedUrl(); // Fetch signed URL when build finishes
            return;
          }
          setLogs((prev) => [...prev, line]);
        };

        ws.onerror = (err) => {
          console.error("WebSocket error:", err);
        };

        ws.onclose = () => {
          if (!active) return;
          // If closed prematurely or completed
          setStatus((prev) => (prev === 'STREAMING' ? 'DISCONNECTED' : prev));
        };
      } catch (err) {
        if (!active) return;
        console.error("Failed to establish websocket logging:", err);
        setError(err.message || 'Failed to connect to build log server.');
        setStatus('FAILED');
        // Fallback to Signed URL log fetch immediately
        fetchSignedUrl();
      }
    };

    connectLogsStream();

    return () => {
      active = false;
      if (wsRef.current) {
        wsRef.current.close();
      }
    };
  }, [owner, repo, buildId]);

  // Scroll to bottom of terminal whenever new log lines arrive
  useEffect(() => {
    logEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [logs]);

  // Status-specific badges
  const renderStatusBadge = () => {
    switch (status) {
      case 'STREAMING':
        return (
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.25rem', fontSize: '0.85rem', color: '#3b82f6', background: 'rgba(59, 130, 246, 0.1)', padding: '0.2rem 0.6rem', borderRadius: '9999px', border: '1px solid rgba(59, 130, 246, 0.2)' }}>
            <span className="pulse-dot" style={{ width: '8px', height: '8px', borderRadius: '50%', background: '#3b82f6' }}></span>
            Live Streaming
          </span>
        );
      case 'COMPLETED':
        return (
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.25rem', fontSize: '0.85rem', color: '#10b981', background: 'rgba(16, 185, 129, 0.1)', padding: '0.2rem 0.6rem', borderRadius: '9999px', border: '1px solid rgba(16, 185, 129, 0.2)' }}>
            <CheckCircle2 size={14} />
            Finished
          </span>
        );
      case 'DISCONNECTED':
        return (
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.25rem', fontSize: '0.85rem', color: '#f59e0b', background: 'rgba(245, 158, 11, 0.1)', padding: '0.2rem 0.6rem', borderRadius: '9999px', border: '1px solid rgba(245, 158, 11, 0.2)' }}>
            <AlertCircle size={14} />
            Stream Closed
          </span>
        );
      case 'CONNECTING':
        return (
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.25rem', fontSize: '0.85rem', color: '#94a3b8', background: 'rgba(148, 163, 184, 0.1)', padding: '0.2rem 0.6rem', borderRadius: '9999px', border: '1px solid rgba(148, 163, 184, 0.2)' }}>
            <RefreshCw size={14} className="spin" />
            Connecting...
          </span>
        );
      default:
        return (
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.25rem', fontSize: '0.85rem', color: '#ef4444', background: 'rgba(239, 68, 68, 0.1)', padding: '0.2rem 0.6rem', borderRadius: '9999px', border: '1px solid rgba(239, 68, 68, 0.2)' }}>
            <XCircle size={14} />
            Offline
          </span>
        );
    }
  };

  return (
    <div>
      <button 
        onClick={() => onNavigate('commit', { owner, repo, sha })} 
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
          marginBottom: '1.25rem',
          padding: 0
        }}
      >
        <ArrowLeft size={14} /> Back to commit details
      </button>

      <div className="page-header" style={{ marginBottom: '1.5rem' }}>
        <div className="page-header-title">
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <Terminal size={24} style={{ color: '#38bdf8' }} />
            <h1 style={{ margin: 0, fontSize: '1.75rem' }}>Cloud Build Logs</h1>
          </div>
          <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '0.75rem', marginTop: '0.5rem', fontSize: '0.9rem' }}>
            <span style={{ color: '#94a3b8' }}>Repo: </span>
            <span style={{ fontWeight: 500 }}>{owner}/{repo}</span>
            <span style={{ color: '#475569' }}>&bull;</span>
            <span style={{ color: '#94a3b8' }}>Commit: </span>
            <span style={{ fontFamily: 'var(--font-mono)', background: 'rgba(255,255,255,0.06)', padding: '0.1rem 0.3rem', borderRadius: '4px', color: '#e2e8f0' }}>
              {sha.substring(0, 7)}
            </span>
            <span style={{ color: '#475569' }}>&bull;</span>
            <span style={{ color: '#94a3b8' }}>Build ID: </span>
            <span style={{ fontFamily: 'var(--font-mono)', color: '#38bdf8' }}>{buildId}</span>
          </div>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
          {renderStatusBadge()}
          {signedUrl && (
            <a 
              href={signedUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="btn btn-secondary"
              style={{ display: 'flex', alignItems: 'center', gap: '0.35rem', textDecoration: 'none', padding: '0.4rem 0.8rem', fontSize: '0.85rem' }}
            >
              <ExternalLink size={14} />
              Open GCS Log
            </a>
          )}
        </div>
      </div>

      <div 
        style={{
          background: '#04060a',
          border: '1px solid var(--border-color)',
          borderRadius: '8px',
          fontFamily: 'var(--font-mono)',
          padding: '1.25rem',
          minHeight: '400px',
          maxHeight: '650px',
          overflowY: 'auto',
          fontSize: '0.85rem',
          lineHeight: '1.5',
          color: '#e2e8f0',
          boxShadow: 'inset 0 2px 8px rgba(0, 0, 0, 0.8)'
        }}
      >
        {logs.length === 0 && status === 'CONNECTING' && (
          <div style={{ color: '#64748b', display: 'flex', alignItems: 'center', gap: '0.5rem', padding: '1rem 0' }}>
            <RefreshCw size={16} className="spin" />
            <span>Establishing live stream to Google Cloud Logging...</span>
          </div>
        )}
        
        {logs.length === 0 && status === 'FAILED' && (
          <div style={{ color: '#fb7185', display: 'flex', flexDirection: 'column', gap: '0.5rem', padding: '1rem 0' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <AlertCircle size={16} />
              <span>Failed to fetch live build logs.</span>
            </div>
            {signedUrl ? (
              <span style={{ color: '#94a3b8' }}>
                You can download the full logs directly from Google Cloud Storage using the button above.
              </span>
            ) : (
              <span style={{ color: '#94a3b8' }}>
                Logs might still be finalizing or Cloud Build has not written them to GCS yet.
              </span>
            )}
          </div>
        )}

        {logs.map((line, index) => {
          let lineStyle = { color: '#e2e8f0' };
          if (line.toLowerCase().includes('error') || line.toLowerCase().includes('failed')) {
            lineStyle = { color: '#fb7185' };
          } else if (line.toLowerCase().includes('success') || line.toLowerCase().includes('passed')) {
            lineStyle = { color: '#34d399' };
          } else if (line.startsWith('Step ') || line.startsWith('Starting ')) {
            lineStyle = { color: '#38bdf8', fontWeight: 'bold' };
          }

          return (
            <div key={index} style={lineStyle}>
              {line}
            </div>
          );
        })}
        <div ref={logEndRef} />
      </div>
      
      {/* Dynamic pulse CSS */}
      <style>{`
        @keyframes pulse {
          0%, 100% { opacity: 1; transform: scale(1); }
          50% { opacity: 0.4; transform: scale(0.9); }
        }
        .pulse-dot {
          animation: pulse 1.5s infinite ease-in-out;
        }
        @keyframes spin {
          to { transform: rotate(360deg); }
        }
        .spin {
          animation: spin 1s linear infinite;
        }
      `}</style>
    </div>
  );
}
