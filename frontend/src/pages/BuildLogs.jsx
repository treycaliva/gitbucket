import { useState, useEffect, useRef } from 'react';
import { apiClient } from '../apiClient';
import { ArrowLeft, ExternalLink, Terminal, CheckCircle2, XCircle, AlertCircle, RefreshCw } from 'lucide-react';
import Card from '../components/Card';
import Chip from '../components/Chip';

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
          <Chip variant="accent" icon={<span className="pulse-dot" style={{ width: '7px', height: '7px', borderRadius: '50%', background: 'var(--gb-accent)' }}></span>}>
            Live Streaming
          </Chip>
        );
      case 'COMPLETED':
        return (
          <Chip variant="ok" icon={<CheckCircle2 size={13} />}>
            Finished
          </Chip>
        );
      case 'DISCONNECTED':
        return (
          <Chip variant="warn" icon={<AlertCircle size={13} />}>
            Stream Closed
          </Chip>
        );
      case 'CONNECTING':
        return (
          <Chip icon={<RefreshCw size={13} className="spin" />}>
            Connecting...
          </Chip>
        );
      default:
        return (
          <Chip variant="err" icon={<XCircle size={13} />}>
            Offline
          </Chip>
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
          color: 'var(--gb-fg-2)',
          display: 'flex',
          alignItems: 'center',
          gap: '0.25rem',
          cursor: 'pointer',
          fontSize: 13,
          fontWeight: 500,
          marginBottom: 18,
          padding: 0
        }}
      >
        <ArrowLeft size={14} /> Back to commit details
      </button>

      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', flexWrap: 'wrap', gap: 16, marginBottom: 24 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 20, fontWeight: 600, letterSpacing: '-0.015em', color: 'var(--gb-fg)', display: 'flex', alignItems: 'center', gap: 9 }}>
            <Terminal size={18} style={{ color: 'var(--gb-accent)' }} />
            Cloud Build Logs
          </h1>
          <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8, marginTop: 6, fontSize: 13, color: 'var(--gb-fg-3)' }}>
            <span>Repo:</span>
            <span style={{ fontWeight: 500, color: 'var(--gb-fg-2)' }}>{owner}/{repo}</span>
            <span style={{ color: 'var(--gb-fg-4)' }}>&bull;</span>
            <span>Commit:</span>
            <span style={{ fontFamily: 'var(--gb-mono)', background: 'var(--gb-surface-2)', padding: '0.1rem 0.4rem', borderRadius: 4, color: 'var(--gb-fg-2)' }}>
              {sha.substring(0, 7)}
            </span>
            <span style={{ color: 'var(--gb-fg-4)' }}>&bull;</span>
            <span>Build ID:</span>
            <span style={{ fontFamily: 'var(--gb-mono)', color: 'var(--gb-accent)' }}>{buildId}</span>
          </div>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          {renderStatusBadge()}
          {signedUrl && (
            <a
              href={signedUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="btn btn-secondary"
              style={{ display: 'flex', alignItems: 'center', gap: '0.35rem', textDecoration: 'none', padding: '0.4rem 0.8rem', fontSize: 13 }}
            >
              <ExternalLink size={14} />
              Open GCS Log
            </a>
          )}
        </div>
      </div>

      <Card style={{ padding: 0, overflow: 'hidden' }}>
        <div
          style={{
            background: 'var(--gb-page)',
            fontFamily: 'var(--gb-mono)',
            padding: '1.25rem',
            minHeight: '400px',
            maxHeight: '650px',
            overflowY: 'auto',
            fontSize: '0.85rem',
            lineHeight: '1.5',
            color: 'var(--gb-fg-2)',
          }}
        >
          {logs.length === 0 && status === 'CONNECTING' && (
            <div style={{ color: 'var(--gb-fg-4)', display: 'flex', alignItems: 'center', gap: '0.5rem', padding: '1rem 0' }}>
              <RefreshCw size={16} className="spin" />
              <span>Establishing live stream to Google Cloud Logging...</span>
            </div>
          )}

          {logs.length === 0 && status === 'FAILED' && (
            <div style={{ color: 'var(--gb-err)', display: 'flex', flexDirection: 'column', gap: '0.5rem', padding: '1rem 0' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                <AlertCircle size={16} />
                <span>Failed to fetch live build logs.</span>
              </div>
              {signedUrl ? (
                <span style={{ color: 'var(--gb-fg-3)' }}>
                  You can download the full logs directly from Google Cloud Storage using the button above.
                </span>
              ) : (
                <span style={{ color: 'var(--gb-fg-3)' }}>
                  Logs might still be finalizing or Cloud Build has not written them to GCS yet.
                </span>
              )}
            </div>
          )}

          {logs.map((line, index) => {
            let lineStyle = { color: 'var(--gb-fg-2)' };
            if (line.toLowerCase().includes('error') || line.toLowerCase().includes('failed')) {
              lineStyle = { color: 'var(--gb-err)' };
            } else if (line.toLowerCase().includes('success') || line.toLowerCase().includes('passed')) {
              lineStyle = { color: 'var(--gb-ok)' };
            } else if (line.startsWith('Step ') || line.startsWith('Starting ')) {
              lineStyle = { color: 'var(--gb-accent)', fontWeight: 'bold' };
            }

            return (
              <div key={index} style={lineStyle}>
                {line}
              </div>
            );
          })}
          <div ref={logEndRef} />
        </div>
      </Card>

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
