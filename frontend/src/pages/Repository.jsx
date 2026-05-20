import React, { useState, useEffect, useMemo } from 'react';
import { apiClient } from '../apiClient';
import { 
  Folder, 
  FileCode, 
  GitBranch, 
  Copy, 
  Check, 
  Layers, 
  Clock, 
  Settings as SettingsIcon, 
  ArrowLeft, 
  FileText,
  Trash2,
  Lock,
  Globe,
  AlertTriangle,
  GitPullRequest,
  MessageSquare,
  GitMerge
} from 'lucide-react';

export default function Repository({ user, owner, repo, initialTab = 'code', initialPath = '', prNumber, onNavigate }) {
  const [meta, setMeta] = useState(null);
  const activeTab = initialTab;
  const [currentBranch, setCurrentBranch] = useState('');
  const [prevInitialPath, setPrevInitialPath] = useState(initialPath);
  const [currentPath, setCurrentPath] = useState(initialPath);
  const [viewingFile, setViewingFile] = useState(null); // If non-null, we are viewing a file. Holds file metadata.

  // Adjust state when initialPath prop changes
  if (initialPath !== prevInitialPath) {
    setPrevInitialPath(initialPath);
    setCurrentPath(initialPath);
    setViewingFile(null);
  }
  
  // Loaded Contents
  const [treeItems, setTreeItems] = useState([]);
  const [fileContent, setFileContent] = useState('');
  const [commits, setCommits] = useState([]);
  const [readmeContent, setReadmeContent] = useState('');
  
  // States
  const [loading, setLoading] = useState(true);
  const [contentLoading, setContentLoading] = useState(false);
  const [error, setError] = useState('');
  const [copied, setCopied] = useState(false);

  // Settings tab state
  const [deleteConfirm, setDeleteConfirm] = useState('');
  const [deleting, setDeleting] = useState(false);

  // General settings state
  const [repoDescription, setRepoDescription] = useState('');
  const [repoVisibility, setRepoVisibility] = useState('public');
  const [autoDeleteBranches, setAutoDeleteBranches] = useState(false);
  const [savingSettings, setSavingSettings] = useState(false);
  const [settingsMessage, setSettingsMessage] = useState('');

  useEffect(() => {
    if (meta) {
      Promise.resolve().then(() => {
        setRepoDescription(meta.description || '');
        setRepoVisibility(meta.visibility || 'public');
        setAutoDeleteBranches(meta.autoDeleteHeadBranches || false);
      });
    }
  }, [meta]);

  const handleSaveSettings = async () => {
    setSavingSettings(true);
    setSettingsMessage('');
    try {
      const updated = await apiClient.patch(`/api/repos/${owner}/${repo}`, {
        description: repoDescription,
        visibility: repoVisibility,
        autoDeleteHeadBranches: autoDeleteBranches
      });
      setMeta(updated);
      setSettingsMessage('Settings saved successfully!');
      setTimeout(() => setSettingsMessage(''), 3000);
    } catch (err) {
      console.error(err);
      setSettingsMessage(err.message || 'Failed to save settings.');
    } finally {
      setSavingSettings(false);
    }
  };

  const cloneUrl = `${window.location.origin}/r/${owner}/${repo}.git`;
  const isOwner = user && user.username && user.username.toLowerCase() === owner.toLowerCase();

  // 1. Load Repository Metadata
  useEffect(() => {
    const loadMetadata = async () => {
      try {
        setLoading(true);
        const data = await apiClient.get(`/api/repos/${owner}/${repo}`);
        setMeta(data);
        
        // Use default branch from metadata, fallback to main
        const defaultBranch = data.defaultBranch || 'main';
        setCurrentBranch(defaultBranch);
      } catch (err) {
        console.error(err);
        setError(err.message || 'Failed to load repository details.');
      } finally {
        setLoading(false);
      }
    };

    Promise.resolve().then(() => {
      loadMetadata();
    });
  }, [owner, repo]);

  // 2. Load Content based on Active Tab / Branch / Path
  useEffect(() => {
    if (!currentBranch) return;

    const loadContent = async () => {
      setContentLoading(true);
      setError('');
      try {
        if (activeTab === 'code') {
          if (viewingFile) {
            // Load File Content
            const content = await apiClient.get(`/api/repos/${owner}/${repo}/blob/${currentBranch}/${viewingFile.path}`, {
              headers: { 'Accept': 'text/plain' }
            });
            // Handle response stream or text
            if (typeof content === 'string') {
              setFileContent(content);
            } else if (content && content.text) {
              const text = await content.text();
              setFileContent(text);
            } else {
              setFileContent('[Binary File]');
            }
          } else {
            // Load Directory Tree
            const items = await apiClient.get(`/api/repos/${owner}/${repo}/tree/${currentBranch}/${currentPath}`);
            setTreeItems(items);
            
            // Look for README.md in the root directory
            const readmeFile = items.find(item => item.type === 'blob' && item.name.toLowerCase() === 'readme.md');
            if (readmeFile && !currentPath) {
              const readme = await apiClient.get(`/api/repos/${owner}/${repo}/blob/${currentBranch}/${readmeFile.path}`);
              const text = typeof readme === 'string' ? readme : (readme.text ? await readme.text() : '');
              setReadmeContent(text);
            } else {
              setReadmeContent('');
            }
          }
        } else if (activeTab === 'commits') {
          const commitList = await apiClient.get(`/api/repos/${owner}/${repo}/commits/${currentBranch}`);
          setCommits(commitList);
        }
      } catch (err) {
        console.error(err);
        setError(err.message || 'Failed to load content.');
      } finally {
        setContentLoading(false);
      }
    };

    Promise.resolve().then(() => {
      loadContent();
    });
  }, [activeTab, currentBranch, currentPath, viewingFile, owner, repo]);

  const copyCloneUrl = () => {
    navigator.clipboard.writeText(cloneUrl);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const handleDirectoryClick = (path) => {
    setViewingFile(null);
    setCurrentPath(path);
  };

  const handleFileClick = (file) => {
    setViewingFile(file);
  };

  const handleBackToFolder = () => {
    setViewingFile(null);
    setFileContent('');
  };

  const handleBreadcrumbClick = (index) => {
    setViewingFile(null);
    setFileContent('');
    if (index === -1) {
      setCurrentPath('');
      return;
    }
    const parts = currentPath.split('/');
    const newPath = parts.slice(0, index + 1).join('/');
    setCurrentPath(newPath);
  };

  const handleDeleteRepository = async () => {
    if (deleteConfirm !== repo) return;
    setDeleting(true);
    try {
      await apiClient.delete(`/api/repos/${owner}/${repo}`);
      onNavigate('dashboard');
    } catch (err) {
      setError(err.message || 'Failed to delete repository');
      setDeleting(false);
    }
  };

  // Simple Markdown Renderer
  const renderReadme = (markdown) => {
    if (!markdown) return '';
    let html = markdown
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;');
    
    // Headers
    html = html.replace(/^# (.*?)$/gm, '<h1 style="font-size: 1.75rem; border-bottom: 1px solid var(--border-color); padding-bottom: 0.35rem; margin-top: 1.5rem; margin-bottom: 1rem;">$1</h1>');
    html = html.replace(/^## (.*?)$/gm, '<h2 style="font-size: 1.4rem; border-bottom: 1px solid var(--border-color); padding-bottom: 0.3rem; margin-top: 1.5rem; margin-bottom: 0.85rem;">$1</h2>');
    html = html.replace(/^### (.*?)$/gm, '<h3 style="font-size: 1.15rem; margin-top: 1.25rem; margin-bottom: 0.75rem;">$1</h3>');
    
    // Code blocks
    html = html.replace(/```([\s\S]*?)```/gm, '<pre style="background: rgba(0,0,0,0.4); border: 1px solid var(--border-color); padding: 1rem; border-radius: 6px; font-family: var(--font-mono); margin-bottom: 1rem; overflow-x: auto;"><code>$1</code></pre>');
    
    // Inline code
    html = html.replace(/`([^`]+)`/g, '<code style="background: rgba(255,255,255,0.08); padding: 0.15rem 0.35rem; border-radius: 4px; font-family: var(--font-mono); font-size: 0.9em;">$1</code>');
    
    // Bold
    html = html.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    
    // Unordered lists
    html = html.replace(/^\* (.*?)$/gm, '<li style="margin-left: 1.5rem; margin-bottom: 0.35rem; color: var(--text-secondary);">$1</li>');
    html = html.replace(/^- (.*?)$/gm, '<li style="margin-left: 1.5rem; margin-bottom: 0.35rem; color: var(--text-secondary);">$1</li>');

    // Paragraphs / Linebreaks
    html = html.split('\n').map(line => {
      if (line.trim().startsWith('<h') || line.trim().startsWith('<pre') || line.trim().startsWith('<li') || line.trim() === '') {
        return line;
      }
      return `<p style="margin-bottom: 0.85rem; color: var(--text-secondary); line-height: 1.6;">${line}</p>`;
    }).join('\n');

    return html;
  };

  if (loading) {
    return (
      <div className="loader-container">
        <div className="loader"></div>
      </div>
    );
  }

  if (error && !meta) {
    return (
      <div className="glass-card" style={{ textAlign: 'center', padding: '3rem 1.5rem' }}>
        <h2 style={{ color: '#f43f5e', marginBottom: '1rem' }}>Repository not found</h2>
        <p style={{ color: '#94a3b8', marginBottom: '1.5rem' }}>{error}</p>
        <button className="btn btn-secondary" onClick={() => onNavigate('dashboard')}>
          <ArrowLeft size={16} /> Back to Dashboard
        </button>
      </div>
    );
  }

  return (
    <div>
      {/* 1. Repository Title & Header Info */}
      <div style={{ marginBottom: '2rem' }}>
        <button 
          onClick={() => onNavigate('dashboard')} 
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
            marginBottom: '0.75rem'
          }}
        >
          <ArrowLeft size={14} /> Back to dashboard
        </button>

        <div className="page-header">
          <div className="page-header-title">
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', flexWrap: 'wrap' }}>
              <h1 style={{ fontSize: '1.85rem', fontWeight: 800, margin: 0 }}>
                <span style={{ color: '#94a3b8', fontWeight: 400 }}>{meta.owner}</span>
                <span style={{ color: '#64748b', margin: '0 0.4rem', fontWeight: 300 }}>/</span>
                <span style={{ color: '#38bdf8' }}>{meta.name}</span>
              </h1>
              <span className={`badge badge-${meta.visibility}`} style={{ height: 'fit-content' }}>
                {meta.visibility === 'private' ? <Lock size={12} style={{ marginRight: '0.25rem' }} /> : <Globe size={12} style={{ marginRight: '0.25rem' }} />}
                {meta.visibility}
              </span>
            </div>
            {meta.description && (
              <p style={{ color: '#94a3b8', marginTop: '0.5rem', fontSize: '0.95rem', maxWidth: '800px', lineHeight: '1.4' }}>
                {meta.description}
              </p>
            )}
          </div>

          {/* HTTPS Clone Link */}
          <div className="page-header-actions">
            <div className="glass-card" style={{ 
              padding: '0.5rem 1rem', 
              display: 'flex', 
              alignItems: 'center', 
              gap: '0.75rem',
              background: 'rgba(15, 23, 42, 0.4)',
              borderRadius: '8px',
              boxShadow: 'none'
            }}>
              <span style={{ fontSize: '0.85rem', color: '#64748b', fontWeight: 600 }}>Clone HTTPS</span>
              <input 
                type="text" 
                readOnly 
                value={cloneUrl} 
                style={{
                  background: 'none',
                  border: 'none',
                  color: '#e2e8f0',
                  fontFamily: 'var(--font-mono)',
                  fontSize: '0.85rem',
                  width: '320px',
                  outline: 'none'
                }}
                onClick={copyCloneUrl}
              />
              <button 
                onClick={copyCloneUrl} 
                style={{
                  background: 'none',
                  border: 'none',
                  color: copied ? '#10b981' : '#38bdf8',
                  cursor: 'pointer',
                  display: 'flex',
                  alignItems: 'center'
                }}
                title="Copy to clipboard"
              >
                {copied ? <Check size={16} /> : <Copy size={16} />}
              </button>
            </div>
          </div>
        </div>
      </div>

      {/* 2. Tabs Selector */}
      <div className="tabs-container">
        <button 
          className={`tab ${activeTab === 'code' ? 'active' : ''}`}
          onClick={() => onNavigate('repository', { owner, repo, tab: 'code' })}
        >
          <Layers size={18} />
          Code
        </button>
        <button 
          className={`tab ${activeTab === 'commits' ? 'active' : ''}`}
          onClick={() => onNavigate('repository', { owner, repo, tab: 'commits' })}
        >
          <Clock size={18} />
          Commits
        </button>
        <button 
          className={`tab ${activeTab === 'pulls' || activeTab === 'pull_detail' ? 'active' : ''}`}
          onClick={() => onNavigate('pulls', { owner, repo })}
        >
          <GitPullRequest size={18} />
          Pull Requests
        </button>
        {isOwner && (
          <button 
            className={`tab ${activeTab === 'settings' ? 'active' : ''}`}
            onClick={() => onNavigate('repository', { owner, repo, tab: 'settings' })}
          >
            <SettingsIcon size={18} />
            Settings
          </button>
        )}
      </div>

      {/* Error handling */}
      {error && (
        <div style={{
          background: 'rgba(244, 63, 94, 0.1)',
          border: '1px solid rgba(244, 63, 94, 0.2)',
          color: '#fb7185',
          padding: '1rem',
          borderRadius: '8px',
          marginBottom: '1.5rem'
        }}>
          {error}
        </div>
      )}

      {/* 3. Tab Contents */}
      {contentLoading ? (
        <div className="loader-container">
          <div className="loader"></div>
        </div>
      ) : (
        <>
          {activeTab === 'code' && (
            <div>
              {/* Branch Selector and Path breadcrumbs */}
              <div style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                marginBottom: '1rem',
                flexWrap: 'wrap',
                gap: '1rem'
              }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
                  <div style={{ 
                    display: 'flex', 
                    alignItems: 'center', 
                    gap: '0.5rem',
                    background: 'rgba(255,255,255,0.04)',
                    border: '1px solid var(--border-color)',
                    padding: '0.4rem 0.8rem',
                    borderRadius: '6px',
                    fontSize: '0.9rem'
                  }}>
                    <GitBranch size={16} style={{ color: '#38bdf8' }} />
                    <select
                      value={currentBranch}
                      onChange={(e) => {
                        setCurrentBranch(e.target.value);
                        setViewingFile(null);
                        setFileContent('');
                      }}
                      style={{
                        background: 'none',
                        border: 'none',
                        color: '#f8fafc',
                        fontFamily: 'inherit',
                        fontWeight: 600,
                        fontSize: '0.9rem',
                        outline: 'none',
                        cursor: 'pointer'
                      }}
                    >
                      {meta.branches && meta.branches.map(b => (
                        <option key={b} value={b} style={{ background: '#0f172a' }}>{b}</option>
                      ))}
                    </select>
                  </div>

                  {/* Breadcrumbs */}
                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.25rem', fontSize: '0.95rem' }}>
                    <span 
                      style={{ color: '#38bdf8', fontWeight: 600, cursor: 'pointer' }}
                      onClick={() => handleBreadcrumbClick(-1)}
                    >
                      {repo}
                    </span>
                    {currentPath.split('/').filter(Boolean).map((part, i) => (
                      <React.Fragment key={i}>
                        <span style={{ color: '#64748b' }}>/</span>
                        <span 
                          style={{ 
                            color: i === currentPath.split('/').filter(Boolean).length - 1 && !viewingFile ? '#f8fafc' : '#38bdf8', 
                            fontWeight: i === currentPath.split('/').filter(Boolean).length - 1 && !viewingFile ? 600 : 400,
                            cursor: 'pointer' 
                          }}
                          onClick={() => handleBreadcrumbClick(i)}
                        >
                          {part}
                        </span>
                      </React.Fragment>
                    ))}
                    {viewingFile && (
                      <>
                        <span style={{ color: '#64748b' }}>/</span>
                        <span style={{ color: '#f8fafc', fontWeight: 600 }}>{viewingFile.name}</span>
                      </>
                    )}
                  </div>
                </div>
                
                {viewingFile && (
                  <button className="btn btn-secondary btn-icon" onClick={handleBackToFolder} style={{ padding: '0.4rem 0.8rem', fontSize: '0.85rem' }}>
                    <ArrowLeft size={14} /> Back to Folder
                  </button>
                )}
              </div>

              {/* A. If viewing a File (Blob) */}
              {viewingFile ? (
                <div className="code-viewer-container">
                  <div className="code-viewer-header">
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                      <FileCode size={18} style={{ color: '#38bdf8' }} />
                      <span style={{ fontWeight: 600 }}>{viewingFile.name}</span>
                    </div>
                    <span className="file-size">
                      {(viewingFile.size / 1024).toFixed(2)} KB
                    </span>
                  </div>
                  <pre className="code-viewer-body">
                    {fileContent}
                  </pre>
                </div>
              ) : (
                /* B. If viewing a Folder (Tree) */
                <div>
                  <div className="file-list">
                    <div className="file-header">
                      <span>Files</span>
                    </div>
                    
                    {/* Back arrow if in a subfolder */}
                    {currentPath && (
                      <div className="file-row" onClick={() => {
                        const parts = currentPath.split('/');
                        parts.pop();
                        setCurrentPath(parts.join('/'));
                      }}>
                        <span className="file-icon"><Folder size={18} style={{ color: '#38bdf8' }} /></span>
                        <span className="file-name" style={{ color: '#38bdf8', fontWeight: 600 }}>..</span>
                      </div>
                    )}

                    {treeItems.length === 0 ? (
                      <div style={{ textAlign: 'center', padding: '3rem 1rem', color: '#64748b' }}>
                        This folder (or repository) is empty. Push some code to get started!
                      </div>
                    ) : (
                      <>
                        {/* Folders first */}
                        {treeItems.filter(item => item.type === 'tree').map(item => (
                          <div 
                            key={item.path} 
                            className="file-row"
                            onClick={() => handleDirectoryClick(item.path)}
                          >
                            <span className="file-icon"><Folder size={18} style={{ color: '#38bdf8' }} /></span>
                            <span className="file-name" style={{ fontWeight: 500 }}>{item.name}</span>
                            <span className="file-size">-</span>
                          </div>
                        ))}
                        {/* Blobs second */}
                        {treeItems.filter(item => item.type === 'blob').map(item => (
                          <div 
                            key={item.path} 
                            className="file-row"
                            onClick={() => handleFileClick(item)}
                          >
                            <span className="file-icon">
                              {item.name.toLowerCase() === 'readme.md' ? <FileText size={18} style={{ color: '#a78bfa' }} /> : <FileCode size={18} style={{ color: '#94a3b8' }} />}
                            </span>
                            <span className="file-name">{item.name}</span>
                            <span className="file-size">{(item.size / 1024).toFixed(1)} KB</span>
                          </div>
                        ))}
                      </>
                    )}
                  </div>

                  {/* README Renderer */}
                  {readmeContent && (
                    <div className="readme-box">
                      <div className="readme-header">
                        <FileText size={18} style={{ color: '#a78bfa' }} />
                        <span>README.md</span>
                      </div>
                      <div 
                        className="readme-body"
                        dangerouslySetInnerHTML={{ __html: renderReadme(readmeContent) }}
                      />
                    </div>
                  )}
                </div>
              )}
            </div>
          )}

          {/* Commits Tab */}
          {activeTab === 'commits' && (
            <div className="glass-card" style={{ padding: 0, overflow: 'hidden' }}>
              {commits.length === 0 ? (
                <div style={{ textAlign: 'center', padding: '3rem 1rem', color: '#64748b' }}>
                  No commits found in this branch.
                </div>
              ) : (
                commits.map(commit => (
                  <div 
                    key={commit.sha} 
                    className="commit-row"
                    style={{ cursor: 'pointer' }}
                    onClick={() => onNavigate('commit', { owner, repo, sha: commit.sha })}
                  >
                    <div style={{ flex: 1 }}>
                      <div className="commit-message">{commit.message}</div>
                      <div className="commit-meta">
                        <span style={{ color: '#f8fafc', fontWeight: 600 }}>{commit.authorName}</span>
                        <span>committed on {new Date(commit.date).toLocaleDateString()}</span>
                      </div>
                    </div>
                    <span className="commit-sha">{commit.sha.substring(0, 7)}</span>
                  </div>
                ))
              )}
            </div>
          )}

          {/* Settings Tab (Owner Only) */}
          {activeTab === 'settings' && isOwner && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '2rem' }}>
              {/* Repository Setup Quickstart */}
              <div className="glass-card">
                <h3 style={{ fontSize: '1.25rem', marginBottom: '1rem', color: '#38bdf8' }}>Repository Command Quickstart</h3>
                <p style={{ color: '#94a3b8', fontSize: '0.9rem', marginBottom: '1rem' }}>
                  Configure your local command-line client to push and pull from this repository:
                </p>
                <pre style={{
                  background: 'rgba(0,0,0,0.4)',
                  border: '1px solid var(--border-color)',
                  padding: '1.25rem',
                  borderRadius: '6px',
                  fontFamily: 'var(--font-mono)',
                  fontSize: '0.85rem',
                  color: '#e2e8f0',
                  lineHeight: '1.6',
                  whiteSpace: 'pre'
                }}>
                  {`# 1. Initialize a new git directory locally
git init
git checkout -b main

# 2. Add files and commit
git add .
git commit -m "initial commit"

# 3. Link remote repository
git remote add origin ${cloneUrl}

# 4. Push to Cloud Run (use your Username and PAT when prompted)
git push -u origin main`}
                </pre>
                <div style={{
                  marginTop: '1rem',
                  padding: '0.75rem 1rem',
                  background: 'rgba(245, 158, 11, 0.08)',
                  border: '1px solid rgba(245, 158, 11, 0.2)',
                  borderRadius: '6px',
                  color: '#f59e0b',
                  fontSize: '0.85rem'
                }}>
                  <strong>Note:</strong> When git asks you for credentials on push, use your username (<strong>{user.username}</strong>) and your generated <strong>Personal Access Token (PAT)</strong> as the password. Standard Firebase account passwords will not work on the command line.
                </div>
              </div>

              {/* Repository Settings */}
              <div className="glass-card">
                <h3 style={{ fontSize: '1.25rem', marginBottom: '1.5rem', color: '#38bdf8' }}>Repository Settings</h3>
                
                <div className="form-group" style={{ marginBottom: '1.5rem' }}>
                  <label className="form-label" style={{ display: 'block', marginBottom: '0.5rem', color: '#e2e8f0', fontSize: '0.9rem' }}>Repository Description</label>
                  <input
                    type="text"
                    className="text-input"
                    value={repoDescription}
                    onChange={(e) => setRepoDescription(e.target.value)}
                    placeholder="Short description of this repository"
                    style={{ width: '100%', maxWidth: '600px' }}
                  />
                </div>

                <div className="form-group" style={{ marginBottom: '1.5rem' }}>
                  <label className="form-label" style={{ display: 'block', marginBottom: '0.5rem', color: '#e2e8f0', fontSize: '0.9rem' }}>Visibility</label>
                  <select
                    value={repoVisibility}
                    onChange={(e) => setRepoVisibility(e.target.value)}
                    style={{
                      background: 'rgba(15, 23, 42, 0.6)',
                      border: '1px solid var(--border-color)',
                      color: '#f8fafc',
                      padding: '0.5rem 0.75rem',
                      borderRadius: '6px',
                      outline: 'none',
                      fontSize: '0.9rem',
                      width: '100%',
                      maxWidth: '200px'
                    }}
                  >
                    <option value="public" style={{ background: '#0f172a' }}>Public</option>
                    <option value="private" style={{ background: '#0f172a' }}>Private</option>
                  </select>
                </div>

                <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', marginBottom: '1.5rem' }}>
                  <input
                    type="checkbox"
                    id="autoDeleteBranchesCheckbox"
                    checked={autoDeleteBranches}
                    onChange={(e) => setAutoDeleteBranches(e.target.checked)}
                    style={{
                      width: '1.1rem',
                      height: '1.1rem',
                      accentColor: '#38bdf8',
                      cursor: 'pointer'
                    }}
                  />
                  <label
                    htmlFor="autoDeleteBranchesCheckbox"
                    style={{ color: '#e2e8f0', fontSize: '0.9rem', cursor: 'pointer', userSelect: 'none' }}
                  >
                    Automatically delete head branches after pull requests are merged.
                  </label>
                </div>

                {settingsMessage && (
                  <div style={{
                    marginBottom: '1rem',
                    color: settingsMessage.includes('fail') || settingsMessage.includes('Failed') ? '#ef4444' : '#10b981',
                    fontSize: '0.85rem'
                  }}>
                    {settingsMessage}
                  </div>
                )}

                <button
                  className="btn btn-primary"
                  onClick={handleSaveSettings}
                  disabled={savingSettings}
                >
                  {savingSettings ? 'Saving...' : 'Save Settings'}
                </button>
              </div>

              {/* Danger Zone */}
              <div className="glass-card" style={{ borderColor: 'rgba(244, 63, 94, 0.3)' }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', color: '#f43f5e', marginBottom: '1rem' }}>
                  <AlertTriangle size={20} />
                  <h3 style={{ fontSize: '1.25rem', fontWeight: 700 }}>Danger Zone</h3>
                </div>
                
                <p style={{ color: '#94a3b8', fontSize: '0.9rem', marginBottom: '1.5rem' }}>
                  Deleting this repository is permanent and will delete the metadata from Firestore and the repository archive files in Google Cloud Storage.
                </p>

                <div className="form-group" style={{ maxWidth: '400px' }}>
                  <label className="form-label">Type <strong>{repo}</strong> to confirm deletion</label>
                  <div style={{ display: 'flex', gap: '0.75rem' }}>
                    <input
                      type="text"
                      className="text-input"
                      style={{ borderColor: deleteConfirm === repo ? 'var(--error)' : 'var(--border-color)' }}
                      placeholder={repo}
                      value={deleteConfirm}
                      onChange={e => setDeleteConfirm(e.target.value)}
                      disabled={deleting}
                    />
                    <button
                      className="btn btn-danger"
                      onClick={handleDeleteRepository}
                      disabled={deleteConfirm !== repo || deleting}
                    >
                      {deleting ? 'Deleting...' : <><Trash2 size={16} /> Delete</>}
                    </button>
                  </div>
                </div>
              </div>
            </div>
          )}

          {/* Pull Requests Tab */}
          {activeTab === 'pulls' && (
            <PullRequestList owner={owner} repo={repo} onNavigate={onNavigate} />
          )}

          {/* Create Pull Request Tab */}
          {activeTab === 'pull_new' && (
            <NewPullRequest owner={owner} repo={repo} meta={meta} onNavigate={onNavigate} />
          )}

          {/* Pull Request Detail Tab */}
          {activeTab === 'pull_detail' && (
            <PullRequestDetail owner={owner} repo={repo} prNumber={prNumber} meta={meta} onNavigate={onNavigate} user={user} />
          )}
        </>
      )}
    </div>
  );
}

// ==========================================
// Pull Request / Merge Request Live Components
// ==========================================

function PullRequestList({ owner, repo, onNavigate }) {
  const [pulls, setPulls] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    const fetchPulls = async () => {
      try {
        const data = await apiClient.get(`/api/repos/${owner}/${repo}/pulls`);
        setPulls(data || []);
      } catch (err) {
        setError(err.message);
      } finally {
        setLoading(false);
      }
    };
    fetchPulls();
  }, [owner, repo]);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <h2 className="gradient-text" style={{ fontSize: '1.5rem', fontWeight: 700, margin: 0 }}>Pull Requests</h2>
          <span style={{ fontSize: '0.9rem', color: '#64748b', background: 'rgba(255,255,255,0.05)', padding: '0.1rem 0.5rem', borderRadius: '12px' }}>
            {pulls.length}
          </span>
        </div>
        <button 
          className="btn btn-primary"
          style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}
          onClick={() => onNavigate('pull_new', { owner, repo })}
        >
          <GitPullRequest size={16} />
          New Pull Request
        </button>
      </div>

      {loading && (
        <div className="loader-container">
          <div className="loader"></div>
        </div>
      )}

      {error && (
        <div className="error-box" style={{ margin: 0 }}>
          {error}
        </div>
      )}

      {!loading && !error && pulls.length === 0 && (
        <div className="glass-card" style={{ padding: '3rem', textAlign: 'center', display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '1rem' }}>
          <GitPullRequest size={48} style={{ color: '#64748b' }} />
          <h3 style={{ margin: 0, fontSize: '1.2rem', color: '#f8fafc' }}>No Pull Requests Yet</h3>
          <p style={{ margin: 0, color: '#94a3b8', maxWidth: '400px' }}>
            Pull requests let you propose changes to branches, review code, and merge updates into other branches.
          </p>
          <button 
            className="btn btn-secondary"
            onClick={() => onNavigate('pull_new', { owner, repo })}
            style={{ marginTop: '0.5rem' }}
          >
            Create first pull request
          </button>
        </div>
      )}

      {!loading && !error && pulls.length > 0 && (
        <div className="pr-list">
          {pulls.map(pr => {
            let statusBadgeClass = 'badge-pr-open';
            if (pr.status === 'merged') statusBadgeClass = 'badge-pr-merged';
            if (pr.status === 'closed') statusBadgeClass = 'badge-pr-closed';

            return (
              <div 
                key={pr.number} 
                className="pr-row"
                onClick={() => onNavigate('pull_detail', { owner, repo, number: pr.number })}
              >
                <div className="pr-status-icon">
                  <GitPullRequest size={20} style={{ color: pr.status === 'merged' ? '#a855f7' : pr.status === 'closed' ? '#ef4444' : '#10b981' }} />
                </div>
                <div className="pr-info">
                  <div className="pr-title">
                    <span>{pr.title}</span>
                    <span style={{ color: '#64748b', fontWeight: 400 }}>#{pr.number}</span>
                  </div>
                  <div className="pr-meta">
                    <span style={{ color: '#f8fafc', fontWeight: 600 }}>@{pr.authorUsername}</span>
                    <span>opened on {new Date(pr.createdAt).toLocaleDateString()}</span>
                    <span>&bull;</span>
                    <span className="pr-compare-branch" style={{ fontSize: '0.75rem', padding: '0.1rem 0.3rem' }}>{pr.sourceBranch}</span>
                    <span style={{ color: '#64748b' }}>&rarr;</span>
                    <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.75rem', color: '#64748b' }}>{pr.targetBranch}</span>
                  </div>
                </div>
                <div>
                  <span className={`badge ${statusBadgeClass}`} style={{ fontSize: '0.75rem', fontWeight: 700, padding: '0.2rem 0.5rem', borderRadius: '4px', textTransform: 'uppercase' }}>
                    {pr.status}
                  </span>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

function NewPullRequest({ owner, repo, meta, onNavigate }) {
  const branches = useMemo(() => meta?.branches || [], [meta?.branches]);
  const [sourceBranch, setSourceBranch] = useState('');
  const [targetBranch, setTargetBranch] = useState('');
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  // Initialize branch selections
  useEffect(() => {
    if (branches.length > 0) {
      Promise.resolve().then(() => {
        const defaultTarget = branches.find(b => b === 'main' || b === 'master') || branches[0];
        setTargetBranch(defaultTarget);
        
        const defaultSource = branches.find(b => b !== defaultTarget) || '';
        setSourceBranch(defaultSource);
      });
    }
  }, [branches]);

  const handleSubmit = async (e) => {
    e.preventDefault();
    if (!title.trim() || !sourceBranch || !targetBranch) {
      setError('Title, source branch, and target branch are required.');
      return;
    }
    if (sourceBranch === targetBranch) {
      setError('Source branch and target branch must be different.');
      return;
    }

    setSubmitting(true);
    setError('');
    try {
      const pr = await apiClient.post(`/api/repos/${owner}/${repo}/pulls`, {
        title,
        description,
        sourceBranch,
        targetBranch
      });
      onNavigate('pull_detail', { owner, repo, number: pr.number });
    } catch (err) {
      setError(err.message || 'Failed to create pull request.');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div style={{ maxWidth: '800px', margin: '0 auto', display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
      <div>
        <button 
          onClick={() => onNavigate('pulls', { owner, repo })} 
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
            marginBottom: '0.75rem',
            padding: 0
          }}
        >
          <ArrowLeft size={14} /> Back to Pull Requests
        </button>
        <h2 className="gradient-text" style={{ fontSize: '1.75rem', fontWeight: 700, margin: 0 }}>Create a Pull Request / Merge Request</h2>
        <p style={{ color: '#94a3b8', fontSize: '0.9rem', marginTop: '0.25rem', marginBottom: 0 }}>
          Propose changes from a development branch to be reviewed and merged into a target branch.
        </p>
      </div>

      {error && (
        <div className="error-box" style={{ margin: 0 }}>
          {error}
        </div>
      )}

      {branches.length < 2 ? (
        <div className="glass-card" style={{ padding: '2rem', display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '1rem', textAlign: 'center' }}>
          <AlertTriangle size={36} style={{ color: '#f59e0b' }} />
          <h3 style={{ margin: 0, fontSize: '1.15rem', color: '#f8fafc' }}>Insufficient Branches</h3>
          <p style={{ margin: 0, color: '#94a3b8', fontSize: '0.9rem', maxWidth: '400px' }}>
            This repository only has {branches.length} branch ({branches[0] || 'none'}). You need at least two branches to compare and create a pull request.
          </p>
          <div style={{ marginTop: '0.5rem', color: '#64748b', fontSize: '0.85rem' }}>
            Create a branch locally using <code style={{ background: 'rgba(255,255,255,0.06)', padding: '0.1rem 0.3rem', borderRadius: '4px' }}>git checkout -b branch-name</code> and push it to GitBucket.
          </div>
        </div>
      ) : (
        <form className="glass-card" style={{ padding: '1.5rem', display: 'flex', flexDirection: 'column', gap: '1.25rem' }} onSubmit={handleSubmit}>
          {/* Branch Selector Row */}
          <div className="pr-compare-header" style={{ margin: 0 }}>
            <span style={{ fontSize: '0.9rem', color: '#94a3b8', fontWeight: 500 }}>Comparing:</span>
            
            {/* Target Branch Selector */}
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <span style={{ fontSize: '0.85rem', color: '#64748b' }}>base:</span>
              <select
                className="text-input"
                style={{ width: 'auto', padding: '0.35rem 2rem 0.35rem 0.75rem', fontSize: '0.85rem', fontFamily: 'var(--font-mono)' }}
                value={targetBranch}
                onChange={e => setTargetBranch(e.target.value)}
              >
                {branches.map(b => (
                  <option key={b} value={b}>{b}</option>
                ))}
              </select>
            </div>

            <span style={{ color: '#64748b', fontWeight: 600 }}>&larr;</span>

            {/* Source Branch Selector */}
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <span style={{ fontSize: '0.85rem', color: '#64748b' }}>compare:</span>
              <select
                className="text-input"
                style={{ width: 'auto', padding: '0.35rem 2rem 0.35rem 0.75rem', fontSize: '0.85rem', fontFamily: 'var(--font-mono)', borderColor: sourceBranch === targetBranch ? '#ef4444' : 'rgba(255,255,255,0.1)' }}
                value={sourceBranch}
                onChange={e => setSourceBranch(e.target.value)}
              >
                {branches.map(b => (
                  <option key={b} value={b}>{b}</option>
                ))}
              </select>
            </div>
          </div>

          {sourceBranch === targetBranch && (
            <div style={{ color: '#ef4444', fontSize: '0.85rem', display: 'flex', alignItems: 'center', gap: '0.25rem' }}>
              <AlertTriangle size={14} /> Source branch and target branch must be different.
            </div>
          )}

          {/* Title Input */}
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
            <label style={{ fontSize: '0.9rem', fontWeight: 600, color: '#f8fafc' }}>Title</label>
            <input
              type="text"
              className="text-input"
              style={{ width: '100%' }}
              placeholder="e.g. Add syntax highlighting for code viewer"
              value={title}
              onChange={e => setTitle(e.target.value)}
              required
            />
          </div>

          {/* Description Input */}
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
            <label style={{ fontSize: '0.9rem', fontWeight: 600, color: '#f8fafc' }}>Description</label>
            <textarea
              className="text-input"
              style={{ width: '100%', minHeight: '120px', resize: 'vertical' }}
              placeholder="Describe the changes in this pull request..."
              value={description}
              onChange={e => setDescription(e.target.value)}
            />
          </div>

          {/* Submit Button */}
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.75rem', borderTop: '1px solid rgba(255,255,255,0.05)', paddingTop: '1rem', marginTop: '0.5rem' }}>
            <button 
              type="button" 
              className="btn btn-secondary"
              onClick={() => onNavigate('pulls', { owner, repo })}
              disabled={submitting}
            >
              Cancel
            </button>
            <button 
              type="submit" 
              className="btn btn-primary"
              disabled={submitting || sourceBranch === targetBranch}
            >
              {submitting ? 'Creating...' : 'Create Pull Request'}
            </button>
          </div>
        </form>
      )}
    </div>
  );
}

function PullRequestDetail({ owner, repo, prNumber, meta, onNavigate, user }) {
  const [pr, setPr] = useState(null);
  const [commits, setCommits] = useState([]);
  const [diff, setDiff] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [prTab, setPrTab] = useState('conversation'); // 'conversation', 'commits', 'diff'

  const [comments, setComments] = useState([]);
  const [newComment, setNewComment] = useState('');

  const [actionLoading, setActionLoading] = useState(false);
  const [actionError, setActionError] = useState('');

  const [repoBranches, setRepoBranches] = useState([]);
  const [deletingBranch, setDeletingBranch] = useState(false);

  const isOwner = user && user.username && user.username.toLowerCase() === owner.toLowerCase();

  useEffect(() => {
    if (meta) {
      Promise.resolve().then(() => {
        setRepoBranches(meta.branches || []);
      });
    }
  }, [meta]);

  const handleDeleteBranch = async () => {
    if (!window.confirm(`Are you sure you want to delete branch ${pr.sourceBranch}?`)) return;
    setDeletingBranch(true);
    setActionError('');
    try {
      const resp = await apiClient.delete(`/api/repos/${owner}/${repo}/branches/${pr.sourceBranch}`);
      if (resp.success) {
        setRepoBranches(prev => prev.filter(b => b !== pr.sourceBranch));
      }
    } catch (err) {
      setActionError(err.message || 'Failed to delete branch.');
    } finally {
      setDeletingBranch(false);
    }
  };

  useEffect(() => {
    const loadPrDetails = async () => {
      setLoading(true);
      setError('');
      try {
        const [prData, commitsData, diffData] = await Promise.all([
          apiClient.get(`/api/repos/${owner}/${repo}/pulls/${prNumber}`),
          apiClient.get(`/api/repos/${owner}/${repo}/pulls/${prNumber}/commits`).catch(() => []),
          apiClient.get(`/api/repos/${owner}/${repo}/pulls/${prNumber}/diff`).catch(() => ({ rawDiff: '' }))
        ]);

        setPr(prData);
        setCommits(commitsData || []);
        setDiff(diffData?.rawDiff || '');

        const saved = localStorage.getItem(`pr_comments_${owner}_${repo}_${prNumber}`);
        if (saved) {
          setComments(JSON.parse(saved));
        } else {
          setComments([]);
        }
      } catch (err) {
        setError(err.message || 'Failed to load pull request details.');
      } finally {
        setLoading(false);
      }
    };

    loadPrDetails();
  }, [owner, repo, prNumber]);

  const handleAddComment = (e) => {
    e.preventDefault();
    if (!newComment.trim()) return;
    const added = [
      ...comments,
      {
        id: Date.now(),
        author: 'currentUser',
        body: newComment,
        date: new Date().toISOString()
      }
    ];
    setComments(added);
    localStorage.setItem(`pr_comments_${owner}_${repo}_${prNumber}`, JSON.stringify(added));
    setNewComment('');
  };

  const handleMerge = async () => {
    if (!window.confirm('Are you sure you want to merge this pull request?')) return;
    setActionLoading(true);
    setActionError('');
    try {
      const resp = await apiClient.post(`/api/repos/${owner}/${repo}/pulls/${prNumber}/merge`);
      if (resp.success) {
        setPr(prev => ({ ...prev, status: 'merged' }));
      } else {
        setActionError(resp.message || 'Failed to merge pull request.');
      }
    } catch (err) {
      setActionError(err.message || 'Failed to merge pull request. There might be a merge conflict.');
    } finally {
      setActionLoading(false);
    }
  };

  const handleClose = async () => {
    if (!window.confirm('Are you sure you want to close this pull request without merging?')) return;
    setActionLoading(true);
    setActionError('');
    try {
      const resp = await apiClient.post(`/api/repos/${owner}/${repo}/pulls/${prNumber}/close`);
      if (resp.success) {
        setPr(prev => ({ ...prev, status: 'closed' }));
      }
    } catch (err) {
      setActionError(err.message || 'Failed to close pull request.');
    } finally {
      setActionLoading(false);
    }
  };

  if (loading) {
    return (
      <div className="loader-container">
        <div className="loader"></div>
      </div>
    );
  }

  if (error || !pr) {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
        <button 
          onClick={() => onNavigate('pulls', { owner, repo })} 
          className="btn btn-secondary"
          style={{ width: 'fit-content' }}
        >
          <ArrowLeft size={14} /> Back to Pull Requests
        </button>
        <div className="error-box" style={{ margin: 0 }}>
          {error || 'Pull request not found.'}
        </div>
      </div>
    );
  }

  let badgeColor = '#38bdf8'; // open
  let statusBadgeClass = 'badge-pr-open';
  if (pr.status === 'merged') {
    badgeColor = '#a855f7';
    statusBadgeClass = 'badge-pr-merged';
  } else if (pr.status === 'closed') {
    badgeColor = '#ef4444';
    statusBadgeClass = 'badge-pr-closed';
  }

  const renderDiffLines = (rawDiff) => {
    if (!rawDiff) return <div style={{ color: '#64748b', fontStyle: 'italic', padding: '2rem', textAlign: 'center' }}>No file modifications in this diff.</div>;
    const lines = rawDiff.split('\n');
    return (
      <pre style={{ margin: 0, padding: '1.25rem', overflowX: 'auto', fontFamily: 'var(--font-mono)', fontSize: '0.85rem', lineHeight: '1.5' }}>
        {lines.map((line, idx) => {
          let color = '#e2e8f0';
          let bg = 'transparent';
          if (line.startsWith('+') && !line.startsWith('+++')) {
            color = '#4ade80';
            bg = 'rgba(74, 222, 128, 0.04)';
          } else if (line.startsWith('-') && !line.startsWith('---')) {
            color = '#f87171';
            bg = 'rgba(248, 113, 113, 0.04)';
          } else if (line.startsWith('@@')) {
            color = '#38bdf8';
            bg = 'rgba(56, 189, 248, 0.05)';
          }
          return (
            <div key={idx} style={{ color, backgroundColor: bg, padding: '0.1rem 0.25rem', whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
              {line}
            </div>
          );
        })}
      </pre>
    );
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
      {/* Header Info */}
      <div>
        <button 
          onClick={() => onNavigate('pulls', { owner, repo })} 
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
            marginBottom: '0.75rem',
            padding: 0
          }}
        >
          <ArrowLeft size={14} /> Back to Pull Requests
        </button>
        <h2 style={{ fontSize: '1.75rem', fontWeight: 700, display: 'flex', alignItems: 'center', gap: '0.75rem', margin: '0 0 0.5rem 0' }}>
          <span style={{ color: '#f8fafc' }}>{pr.title}</span>
          <span style={{ color: '#64748b', fontWeight: 400 }}>#{pr.number}</span>
        </h2>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', flexWrap: 'wrap' }}>
          <span className={`badge ${statusBadgeClass}`} style={{ 
            padding: '0.25rem 0.75rem',
            borderRadius: '6px',
            fontSize: '0.8rem',
            fontWeight: 700,
            textTransform: 'uppercase'
          }}>
            {pr.status}
          </span>
          <span style={{ color: '#94a3b8', fontSize: '0.9rem' }}>
            <strong>@{pr.authorUsername}</strong> wants to merge {commits.length} commit{commits.length !== 1 ? 's' : ''} into <code style={{ background: 'rgba(255,255,255,0.06)', padding: '0.1rem 0.3rem', borderRadius: '4px', fontFamily: 'var(--font-mono)' }}>{pr.targetBranch}</code> from <code style={{ background: 'rgba(255,255,255,0.06)', padding: '0.1rem 0.3rem', borderRadius: '4px', fontFamily: 'var(--font-mono)' }}>{pr.sourceBranch}</code>
          </span>
        </div>
      </div>

      {actionError && (
        <div className="error-box" style={{ margin: 0 }}>
          {actionError}
        </div>
      )}

      {/* Sub tabs navigation */}
      <div className="tabs-container" style={{ marginBottom: '1rem' }}>
        <button 
          className={`tab ${prTab === 'conversation' ? 'active' : ''}`}
          onClick={() => setPrTab('conversation')}
        >
          <MessageSquare size={16} />
          Conversation ({comments.length + 1})
        </button>
        <button 
          className={`tab ${prTab === 'commits' ? 'active' : ''}`}
          onClick={() => setPrTab('commits')}
        >
          <Clock size={16} />
          Commits ({commits.length})
        </button>
        <button 
          className={`tab ${prTab === 'diff' ? 'active' : ''}`}
          onClick={() => setPrTab('diff')}
        >
          <FileText size={16} />
          Files Changed
        </button>
      </div>

      {/* Content based on sub tabs */}
      {prTab === 'conversation' && (
        <div style={{ display: 'grid', gridTemplateColumns: '2fr 1fr', gap: '1.5rem', alignItems: 'start' }}>
          {/* Main timeline */}
          <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
            {/* PR Description card */}
            <div className="glass-card" style={{ padding: '1.25rem' }}>
              <div style={{ borderBottom: '1px solid var(--border-color)', paddingBottom: '0.75rem', marginBottom: '0.75rem', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                <span style={{ fontWeight: 600, color: '#f8fafc' }}>@{pr.authorUsername}</span>
                <span style={{ color: '#64748b', fontSize: '0.85rem' }}>commented on {new Date(pr.createdAt).toLocaleDateString()}</span>
              </div>
              <p style={{ color: '#e2e8f0', margin: 0, lineHeight: '1.5', whiteSpace: 'pre-wrap' }}>
                {pr.description || 'No description provided.'}
              </p>
            </div>

            {/* Conversation timeline */}
            {comments.length > 0 && (
              <div className="pr-timeline" style={{ marginTop: 0 }}>
                {comments.map(comment => (
                  <div key={comment.id} className="timeline-item" style={{ display: 'flex', gap: '1rem', position: 'relative', marginBottom: '1.5rem' }}>
                    <div className="timeline-icon-wrapper" style={{
                      width: '32px',
                      height: '32px',
                      borderRadius: '50%',
                      background: 'rgba(255, 255, 255, 0.05)',
                      border: '1px solid var(--border-color)',
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      color: '#94a3b8',
                      flexShrink: 0
                    }}>
                      <MessageSquare size={14} />
                    </div>
                    <div className="timeline-content-card" style={{
                      flex: 1,
                      background: 'var(--bg-card)',
                      border: '1px solid var(--border-color)',
                      borderRadius: 'var(--border-radius)',
                      padding: '1rem'
                    }}>
                      <div className="timeline-content-header" style={{
                        display: 'flex',
                        justifyContent: 'space-between',
                        alignItems: 'center',
                        borderBottom: '1px solid rgba(255,255,255,0.05)',
                        paddingBottom: '0.5rem',
                        marginBottom: '0.5rem',
                        fontSize: '0.85rem',
                        color: '#64748b'
                      }}>
                        <span style={{ fontWeight: 600, color: comment.author === 'currentUser' ? '#38bdf8' : '#f8fafc' }}>@{comment.author === 'currentUser' ? 'you' : comment.author}</span>
                        <span>{new Date(comment.date).toLocaleString()}</span>
                      </div>
                      <div className="timeline-content-body" style={{
                        color: '#e2e8f0',
                        fontSize: '0.9rem',
                        lineHeight: '1.4',
                        whiteSpace: 'pre-wrap'
                      }}>
                        {comment.body}
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            )}

            {/* Add comment form */}
            <form className="glass-card" style={{ padding: '1rem' }} onSubmit={handleAddComment}>
              <h4 style={{ margin: '0 0 0.75rem 0', fontSize: '0.95rem', fontWeight: 600, color: '#f8fafc' }}>Add a comment</h4>
              <textarea
                className="text-input"
                style={{ width: '100%', minHeight: '80px', marginBottom: '0.75rem', resize: 'vertical', fontFamily: 'inherit' }}
                placeholder="Leave a comment..."
                value={newComment}
                onChange={e => setNewComment(e.target.value)}
              />
              <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
                <button type="submit" className="btn btn-secondary" style={{ padding: '0.4rem 1rem', fontSize: '0.9rem' }}>
                  Comment
                </button>
              </div>
            </form>

            {/* Merge box at bottom */}
            {pr.status === 'open' && (
              <div className="pr-merge-box" style={{
                display: 'flex',
                alignItems: 'center',
                gap: '1.25rem',
                padding: '1.25rem',
                background: 'rgba(30, 41, 59, 0.25)',
                border: '1px solid var(--border-color)',
                borderRadius: 'var(--border-radius)',
                marginTop: '1rem'
              }}>
                <div className="merge-status-indicator success" style={{
                  width: '40px',
                  height: '40px',
                  borderRadius: '50%',
                  background: 'rgba(16, 185, 129, 0.1)',
                  border: '1px solid rgba(16, 185, 129, 0.2)',
                  color: '#10b981',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  flexShrink: 0
                }}>
                  <GitMerge size={20} />
                </div>
                <div className="merge-box-content" style={{ flex: 1 }}>
                  <h3 className="merge-box-title" style={{ color: '#f8fafc', margin: 0, fontSize: '1rem', fontWeight: 600 }}>This branch has no conflicts</h3>
                  <div className="merge-box-desc" style={{ color: '#94a3b8', fontSize: '0.85rem' }}>Merging can be performed automatically.</div>
                </div>
                <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
                  <button 
                    className="btn btn-secondary" 
                    onClick={handleClose}
                    disabled={actionLoading}
                  >
                    Close
                  </button>
                  <button 
                    className="btn btn-primary" 
                    onClick={handleMerge}
                    disabled={actionLoading}
                    style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}
                  >
                    {actionLoading ? (
                      <span className="loader" style={{ width: '14px', height: '14px', borderWidth: '2px' }}></span>
                    ) : (
                      <>
                        <GitMerge size={16} />
                        Merge
                      </>
                    )}
                  </button>
                </div>
              </div>
            )}

            {pr.status === 'merged' && (
              <div className="pr-merge-box" style={{
                display: 'flex',
                alignItems: 'center',
                gap: '1.25rem',
                padding: '1.25rem',
                background: 'rgba(168, 85, 247, 0.05)',
                border: '1px solid rgba(168, 85, 247, 0.2)',
                borderRadius: 'var(--border-radius)',
                marginTop: '1rem'
              }}>
                <div className="merge-status-indicator merged" style={{
                  width: '40px',
                  height: '40px',
                  borderRadius: '50%',
                  background: 'rgba(168, 85, 247, 0.1)',
                  border: '1px solid rgba(168, 85, 247, 0.2)',
                  color: '#a855f7',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  flexShrink: 0
                }}>
                  <GitMerge size={20} />
                </div>
                <div className="merge-box-content" style={{ flex: 1 }}>
                  <h3 className="merge-box-title" style={{ color: '#f8fafc', margin: 0, fontSize: '1rem', fontWeight: 600 }}>Pull request successfully merged</h3>
                  <div className="merge-box-desc" style={{ color: '#94a3b8', fontSize: '0.85rem' }}>
                    Commits are now integrated into <code style={{ fontFamily: 'var(--font-mono)' }}>{pr.targetBranch}</code>.
                  </div>
                </div>
                {isOwner && (
                  <div style={{ display: 'flex', alignItems: 'center' }}>
                    {repoBranches.includes(pr.sourceBranch) ? (
                      <button
                        className="btn btn-secondary"
                        onClick={handleDeleteBranch}
                        disabled={deletingBranch}
                        style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', borderColor: 'rgba(239, 68, 68, 0.3)', color: '#f87171' }}
                      >
                        {deletingBranch ? 'Deleting...' : <><Trash2 size={14} /> Delete branch</>}
                      </button>
                    ) : (
                      <span style={{ fontSize: '0.85rem', color: '#64748b', display: 'flex', alignItems: 'center', gap: '0.25rem' }}>
                        Branch <code style={{ fontFamily: 'var(--font-mono)' }}>{pr.sourceBranch}</code> deleted
                      </span>
                    )}
                  </div>
                )}
              </div>
            )}

            {pr.status === 'closed' && (
              <div className="pr-merge-box" style={{
                display: 'flex',
                alignItems: 'center',
                gap: '1.25rem',
                padding: '1.25rem',
                background: 'rgba(244, 63, 94, 0.05)',
                border: '1px solid rgba(244, 63, 94, 0.2)',
                borderRadius: 'var(--border-radius)',
                marginTop: '1rem'
              }}>
                <div className="merge-status-indicator error" style={{
                  width: '40px',
                  height: '40px',
                  borderRadius: '50%',
                  background: 'rgba(244, 63, 94, 0.1)',
                  border: '1px solid rgba(244, 63, 94, 0.2)',
                  color: '#f43f5e',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  flexShrink: 0
                }}>
                  <AlertTriangle size={20} />
                </div>
                <div className="merge-box-content" style={{ flex: 1 }}>
                  <h3 className="merge-box-title" style={{ color: '#f8fafc', margin: 0, fontSize: '1rem', fontWeight: 600 }}>Pull request closed</h3>
                  <div className="merge-box-desc" style={{ color: '#94a3b8', fontSize: '0.85rem' }}>
                    This pull request was closed without merging changes.
                  </div>
                </div>
              </div>
            )}
          </div>

          {/* Right Column: Metadata / info */}
          <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
            <div className="glass-card" style={{ padding: '1.25rem' }}>
              <h3 style={{ fontSize: '1rem', fontWeight: 600, color: '#f8fafc', margin: '0 0 1rem 0' }}>Review Status</h3>
              <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem', fontSize: '0.9rem' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: '#64748b' }}>Status</span>
                  <span style={{ color: badgeColor, fontWeight: 600, textTransform: 'uppercase' }}>{pr.status}</span>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: '#64748b' }}>Branches</span>
                  <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.8rem', color: '#94a3b8' }}>
                    {pr.sourceBranch} &rarr; {pr.targetBranch}
                  </span>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: '#64748b' }}>Created</span>
                  <span style={{ color: '#e2e8f0' }}>{new Date(pr.createdAt).toLocaleDateString()}</span>
                </div>
              </div>
            </div>
          </div>
        </div>
      )}

      {prTab === 'commits' && (
        <div className="glass-card" style={{ padding: 0, overflow: 'hidden' }}>
          <div style={{ padding: '1rem', borderBottom: '1px solid var(--border-color)' }}>
            <h3 style={{ fontSize: '1.1rem', fontWeight: 600, color: '#f8fafc', margin: 0 }}>Commits ({commits.length})</h3>
          </div>
          <div>
            {commits.length === 0 ? (
              <div style={{ color: '#64748b', fontStyle: 'italic', padding: '2rem', textAlign: 'center' }}>No commits found.</div>
            ) : (
              commits.map(c => (
                <div key={c.sha} className="commit-row" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.25rem' }}>
                      <span className="commit-message" style={{ fontSize: '0.95rem', margin: 0 }}>{c.message}</span>
                    </div>
                    <div className="commit-meta">
                      <span style={{ color: '#f8fafc', fontWeight: 600 }}>@{c.authorName}</span>
                      <span>committed on {new Date(c.date).toLocaleDateString()}</span>
                    </div>
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center' }}>
                    <code style={{ color: '#38bdf8', fontSize: '0.85rem', fontWeight: 600, background: 'rgba(56,189,248,0.1)', padding: '0.2rem 0.5rem', borderRadius: '4px', fontFamily: 'var(--font-mono)' }}>
                      {c.sha.substring(0, 8)}
                    </code>
                  </div>
                </div>
              ))
            )}
          </div>
        </div>
      )}

      {prTab === 'diff' && (
        <div className="glass-card" style={{ padding: 0, overflow: 'hidden' }}>
          <div style={{ padding: '1rem', borderBottom: '1px solid var(--border-color)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <h3 style={{ fontSize: '1.1rem', fontWeight: 600, color: '#f8fafc', margin: 0 }}>Files Changed</h3>
          </div>
          <div style={{ background: 'rgba(0,0,0,0.15)' }}>
            {renderDiffLines(diff)}
          </div>
        </div>
      )}
    </div>
  );
}
