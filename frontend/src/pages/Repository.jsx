import React, { useState, useEffect, useMemo } from 'react';
import { apiClient } from '../apiClient';
import { authService } from '../authService';
import BranchProtectionModal from '../components/BranchProtectionModal';
import BranchTagPicker from '../components/BranchTagPicker';
import LatestCommitBar from '../components/LatestCommitBar';
import Card from '../components/Card';
import Chip from '../components/Chip';
import SectionHead from '../components/SectionHead';
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
  Hash,
  Users,
  Activity,
} from 'lucide-react';
import PullRequestDetail from '../components/PullRequestDetail';
import InsightsTab from '../components/InsightsTab';
import { renderReadme } from '../utils/markdown';



const FILE_MIX_PALETTE = {
  '.go': '#5fc7f5',
  '.jsx': '#fbbf24', '.tsx': '#fbbf24',
  '.js': '#fde68a', '.ts': '#fde68a', '.mjs': '#fde68a',
  '.md': '#a78bfa',
  '.css': '#f9a8d4', '.scss': '#f9a8d4',
  '.sh': '#4ade80',
  other: '#5d6678', config: '#5d6678',
};
const mixColor = (name) => FILE_MIX_PALETTE[name] || FILE_MIX_PALETTE.other;

// Derive a top-5 + "other" file-mix from the current tree listing. No backend.
function fileMix(treeItems) {
  const blobs = (treeItems || []).filter((t) => t.type === 'blob');
  if (blobs.length === 0) return { rows: [], total: 0 };
  const byExt = {};
  for (const it of blobs) {
    const dot = it.name.lastIndexOf('.');
    const ext = dot <= 0 ? 'config' : it.name.slice(dot).toLowerCase();
    byExt[ext] = (byExt[ext] || 0) + 1;
  }
  const sorted = Object.entries(byExt).sort((a, b) => b[1] - a[1]);
  const top = sorted.slice(0, 5);
  const otherCount = sorted.slice(5).reduce((s, [, n]) => s + n, 0);
  if (otherCount) top.push(['other', otherCount]);
  return {
    rows: top.map(([name, count]) => ({ name, count, color: mixColor(name) })),
    total: blobs.length,
  };
}

function AboutRailCard({ meta, collaboratorsCount, commits }) {
  const VisIcon = meta.visibility === 'private' ? Lock : Globe;
  return (
    <Card style={{ padding: 16 }}>
      <SectionHead kicker="ABOUT" title="" right={<Chip variant="ok" dot>RDY</Chip>} />
      {meta.description && (
        <p style={{ fontSize: 13, color: 'var(--gb-fg-2)', lineHeight: 1.5, margin: '0 0 12px' }}>
          {meta.description}
        </p>
      )}
      <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 8, fontSize: 12.5, color: 'var(--gb-fg-3)' }}>
        <li style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <VisIcon size={12} /> {meta.visibility}
        </li>
        <li style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <GitBranch size={12} /> {(meta.branches || []).length} branches
          <span style={{ color: 'var(--gb-fg-4)' }}>· default</span>
          <span className="mono" style={{ fontSize: 11.5 }}>{meta.defaultBranch || 'main'}</span>
        </li>
        {collaboratorsCount != null && (
          <li style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <Users size={12} /> {collaboratorsCount + 1} collaborators
          </li>
        )}
        {commits && commits.length > 0 && (
          <li style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <Clock size={12} /> {commits.length}{commits.length >= 50 ? '+' : ''} commits loaded
          </li>
        )}
      </ul>
    </Card>
  );
}

function FileMixCard({ mix }) {
  return (
    <Card style={{ padding: 16 }}>
      <SectionHead kicker="FILE MIX" title="" right={<Chip variant="ok" dot>RDY</Chip>} />
      <div style={{ display: 'flex', height: 8, borderRadius: 4, overflow: 'hidden', marginBottom: 10, background: 'var(--gb-surface-2)' }}>
        {mix.rows.map((r) => (
          <div key={r.name} style={{ width: `${(r.count / mix.total) * 100}%`, background: r.color }} />
        ))}
      </div>
      <ul style={{ listStyle: 'none', margin: '0 0 8px', padding: 0, display: 'flex', flexWrap: 'wrap', gap: '4px 12px', fontSize: 11.5 }}>
        {mix.rows.map((r) => (
          <li key={r.name} style={{ display: 'inline-flex', alignItems: 'center', gap: 6, color: 'var(--gb-fg-2)' }}>
            <span style={{ width: 7, height: 7, borderRadius: 999, background: r.color, display: 'inline-block' }} />
            <span className="mono" style={{ fontWeight: 500 }}>{r.name}</span>
            <span className="mono" style={{ color: 'var(--gb-fg-4)' }}>{r.count}</span>
          </li>
        ))}
      </ul>
      <div style={{ fontSize: 11, color: 'var(--gb-fg-4)' }}>
        by file count — {mix.total} files at this ref
      </div>
    </Card>
  );
}

function BranchesRailCard({ branches, defaultBranch, currentBranch, onSelect }) {
  return (
    <Card style={{ padding: 16 }}>
      <SectionHead kicker="BRANCHES" title="" right={<Chip variant="ok" dot>RDY</Chip>} />
      <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 2 }}>
        {branches.map((b) => (
          <li key={b}>
            <button
              type="button"
              onClick={() => onSelect(b)}
              style={{
                width: '100%', display: 'flex', alignItems: 'center', gap: 8,
                padding: '6px 8px', borderRadius: 6, border: 'none', cursor: 'pointer',
                background: b === currentBranch ? 'var(--gb-hover)' : 'transparent',
                color: 'var(--gb-fg-2)', fontSize: 12.5, textAlign: 'left',
              }}
            >
              <GitBranch size={12} style={{ color: 'var(--gb-fg-3)', flexShrink: 0 }} />
              <span className="mono" style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{b}</span>
              {b === defaultBranch && (
                <span style={{ marginLeft: 'auto' }}><Chip variant="accent">default</Chip></span>
              )}
            </button>
          </li>
        ))}
      </ul>
    </Card>
  );
}

function TagsRailCard({ tags }) {
  const top = tags.slice(0, 5);
  return (
    <Card style={{ padding: 16 }}>
      <SectionHead kicker="TAGS" title="" right={<Chip variant="ok" dot>RDY</Chip>} />
      <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 6 }}>
        {top.map((t) => (
          <li key={t.name} style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12.5 }}>
            <Hash size={12} style={{ color: 'var(--gb-fg-3)', flexShrink: 0 }} />
            <span className="mono" style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{t.name}</span>
          </li>
        ))}
      </ul>
      <div style={{ fontSize: 11, color: 'var(--gb-fg-4)', marginTop: 10 }}>
        Promoting tags to Releases (with body + assets) needs new backend work.
      </div>
    </Card>
  );
}

// Wrapper to isolate dangerouslySetInnerHTML (content is sanitized by renderReadme)
function ReadmeBody({ content }) {
  const html = renderReadme(content);
  return <div className="readme-body" dangerouslySetInnerHTML={{ __html: html }} />;
}

function QuickstartCard({ cloneUrl, username }) {
  return (
    <div style={{ background: 'var(--gb-surface)', border: '1px solid var(--gb-line)', borderRadius: 10, padding: '1.5rem' }}>
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
        <strong>Note:</strong> When git asks you for credentials on push, use your username (<strong>{username}</strong>) and your generated <strong>Personal Access Token (PAT)</strong> as the password. Standard Firebase account passwords will not work on the command line.
      </div>
    </div>
  );
}

const COMMITS_PAGE_SIZE = 50;

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
  const [codeowners, setCodeowners] = useState({}); // map: entry name → ["@alice", ...]
  const [fileContent, setFileContent] = useState('');
  const [commits, setCommits] = useState([]);
  const [commitsLoading, setCommitsLoading] = useState(false);
  const [commitsHasMore, setCommitsHasMore] = useState(true);
  const [commitsError, setCommitsError] = useState('');
  const [readmeContent, setReadmeContent] = useState('');
  const [tags, setTags] = useState(null);          // null = not loaded yet; [] = loaded empty
  const [tagsError, setTagsError] = useState('');
  const [collaboratorsCount, setCollaboratorsCount] = useState(null);
  const mix = useMemo(() => fileMix(treeItems), [treeItems]);
  
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

  // Collaborators state
  const [collaborators, setCollaborators] = useState([]);
  const [newCollabUsername, setNewCollabUsername] = useState('');
  const [collaboratorError, setCollaboratorError] = useState('');

  // Branch protection state
  const [protectionRules, setProtectionRules] = useState([]);
  const [protectionError, setProtectionError] = useState('');
  const [showProtectionModal, setShowProtectionModal] = useState(false);
  const [editingRule, setEditingRule] = useState(null); // null => create mode

  // Insights-only state
  const [insightsLoaded, setInsightsLoaded] = useState(false);
  const [insightsLoading, setInsightsLoading] = useState(false);
  const [insightsPulls, setInsightsPulls] = useState([]);
  const [insightsCommits, setInsightsCommits] = useState([]);
  const [insightsCollaborators, setInsightsCollaborators] = useState([]);
  const [insightsRules, setInsightsRules] = useState([]);
  const [codeownersRoot, setCodeownersRoot] = useState({}); // { childName: ["@a", ...] }

  // Record this repo visit for the Dashboard "Recently visited" rail
  // (gitbucket.recent = JSON array of "owner/repo" slugs, most-recent-first).
  useEffect(() => {
    if (!owner || !repo) return;
    try {
      const KEY = 'gitbucket.recent';
      const slug = `${owner}/${repo}`;
      const prev = JSON.parse(localStorage.getItem(KEY) || '[]');
      const arr = (Array.isArray(prev) ? prev : []).filter((s) => s !== slug);
      arr.unshift(slug);
      localStorage.setItem(KEY, JSON.stringify(arr.slice(0, 10)));
    } catch {
      // localStorage unavailable / malformed — non-critical, ignore
    }
  }, [owner, repo]);

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

  const config = authService.getConfig();
  const gitBaseUrl = (config && config.gitUrl) ? config.gitUrl : window.location.origin;
  const cloneUrl = `${gitBaseUrl}/r/${owner}/${repo}.git`;
  const isOwner = user && user.username && user.username.toLowerCase() === owner.toLowerCase();

  // Load collaborators when entering Settings tab as owner
  useEffect(() => {
    if (activeTab !== 'settings' || !isOwner) return;
    apiClient.get(`/api/repos/${owner}/${repo}/collaborators`)
      .then((data) => setCollaborators(Array.isArray(data) ? data : []))
      .catch(() => {});
  }, [owner, repo, activeTab, isOwner]);

  const addCollaborator = async () => {
    setCollaboratorError('');
    try {
      await apiClient.post(`/api/repos/${owner}/${repo}/collaborators`, {
        username: newCollabUsername.trim(),
      });
      setNewCollabUsername('');
      const list = await apiClient.get(`/api/repos/${owner}/${repo}/collaborators`);
      setCollaborators(Array.isArray(list) ? list : []);
    } catch (err) {
      setCollaboratorError(err.message || 'Failed to add');
    }
  };

  const removeCollaborator = async (username) => {
    try {
      await apiClient.delete(`/api/repos/${owner}/${repo}/collaborators/${username}`);
      setCollaborators((prev) => prev.filter((c) => c.username !== username));
    } catch (err) {
      setCollaboratorError(err.message || 'Failed to remove');
    }
  };

  // Load branch protection rules when entering Settings as owner
  useEffect(() => {
    if (activeTab !== 'settings' || !isOwner) return;
    apiClient.get(`/api/repos/${owner}/${repo}/branch-protection`)
      .then((data) => setProtectionRules(Array.isArray(data) ? data : []))
      .catch(() => {});
  }, [owner, repo, activeTab, isOwner]);

  // Lazy Insights fetch — fires once per repo when Insights first opens
  useEffect(() => {
    if (activeTab !== 'insights' || insightsLoaded) return;
    const branch = currentBranch || meta?.defaultBranch || 'main';
    if (!branch) return;

    let cancelled = false;
    Promise.resolve().then(() => { setInsightsLoading(true); });
    const safe = (p, fallback) => p.then((d) => d).catch(() => fallback);
    const coParams = new URLSearchParams({ path: '', ref: branch });

    Promise.all([
      safe(apiClient.get(`/api/repos/${owner}/${repo}/commits/${branch}?limit=${COMMITS_PAGE_SIZE}&offset=0`), []),
      safe(apiClient.get(`/api/repos/${owner}/${repo}/tags`), []),
      safe(apiClient.get(`/api/repos/${owner}/${repo}/pulls`), []),
      safe(apiClient.get(`/api/repos/${owner}/${repo}/collaborators`), []),
      isOwner
        ? safe(apiClient.get(`/api/repos/${owner}/${repo}/branch-protection`), [])
        : Promise.resolve([]),
      safe(apiClient.get(`/api/repos/${owner}/${repo}/codeowners?${coParams.toString()}`), { entries: {} }),
    ]).then(([commitList, tagList, pullList, collabList, rules, co]) => {
      if (cancelled) return;
      setInsightsCommits(Array.isArray(commitList) ? commitList : []);
      setTags(Array.isArray(tagList) ? tagList : []);
      setInsightsPulls(Array.isArray(pullList) ? pullList : []);
      setInsightsCollaborators(Array.isArray(collabList) ? collabList : []);
      setInsightsRules(Array.isArray(rules) ? rules : []);
      setCodeownersRoot((co && co.entries) || {});
      setInsightsLoaded(true);
      setInsightsLoading(false);
    }).catch(() => {
      if (!cancelled) setInsightsLoading(false);
      // leave insightsLoaded false so a retry is possible on next activation
    });

    return () => { cancelled = true; };
  }, [activeTab, insightsLoaded, owner, repo, currentBranch, meta?.defaultBranch, isOwner]);

  // Load tags for Code tab (reset on branch change)
  useEffect(() => {
    if (activeTab !== 'code' || !currentBranch) return;
    let cancelled = false;
    Promise.resolve().then(() => { setTags(null); setTagsError(''); });
    apiClient.get(`/api/repos/${owner}/${repo}/tags`)
      .then((data) => { if (!cancelled) setTags(Array.isArray(data) ? data : []); })
      .catch((err) => {
        if (!cancelled) { setTagsError(err.message || 'Failed to load tags'); setTags([]); }
      });
    return () => { cancelled = true; };
  }, [activeTab, owner, repo, currentBranch]);

  // Load collaborators count for Code tab
  useEffect(() => {
    if (activeTab !== 'code') return;
    let cancelled = false;
    apiClient.get(`/api/repos/${owner}/${repo}/collaborators`)
      .then((data) => { if (!cancelled) setCollaboratorsCount(Array.isArray(data) ? data.length : 0); })
      .catch(() => { if (!cancelled) setCollaboratorsCount(null); }); // count just hides on failure
    return () => { cancelled = true; };
  }, [activeTab, owner, repo]);

  const refetchProtectionRules = async () => {
    try {
      const list = await apiClient.get(`/api/repos/${owner}/${repo}/branch-protection`);
      setProtectionRules(Array.isArray(list) ? list : []);
    } catch {
      // leave existing list; surface error elsewhere
    }
  };

  const handleProtectionSubmit = async (rule) => {
    setProtectionError('');
    if (rule.id) {
      await apiClient.put(`/api/repos/${owner}/${repo}/branch-protection/${rule.id}`, rule);
    } else {
      await apiClient.post(`/api/repos/${owner}/${repo}/branch-protection`, rule);
    }
    await refetchProtectionRules();
    setShowProtectionModal(false);
    setEditingRule(null);
  };

  const handleProtectionDelete = async (ruleId) => {
    setProtectionError('');
    try {
      await apiClient.delete(`/api/repos/${owner}/${repo}/branch-protection/${ruleId}`);
      await refetchProtectionRules();
    } catch (err) {
      setProtectionError(err.message || 'Failed to delete rule.');
    }
  };

  const loadMetadata = async () => {
    try {
      setLoading(true);
      const data = await apiClient.get(`/api/repos/${owner}/${repo}`);
      setMeta(data);
      
      // Use default branch from metadata, fallback to main
      const defaultBranch = data.defaultBranch || 'main';
      setCurrentBranch(prev => {
        if (!prev || !data.branches || !data.branches.includes(prev)) {
          return defaultBranch;
        }
        return prev;
      });
    } catch (err) {
      console.error(err);
      setError(err.message || 'Failed to load repository details.');
    } finally {
      setLoading(false);
    }
  };

  // 1. Load Repository Metadata
  useEffect(() => {
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

            // Load CODEOWNERS map for the current directory (skip when empty).
            if (items && items.length > 0) {
              const params = new URLSearchParams({
                path: currentPath || '',
                ref: currentBranch,
              });
              try {
                const co = await apiClient.get(`/api/repos/${owner}/${repo}/codeowners?${params.toString()}`);
                setCodeowners((co && co.entries) || {});
              } catch {
                setCodeowners({});
              }
            } else {
              setCodeowners({});
            }

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
          setCommitsError('');
          const commitList = await apiClient.get(`/api/repos/${owner}/${repo}/commits/${currentBranch}?limit=${COMMITS_PAGE_SIZE}&offset=0`);
          const list = commitList || [];
          setCommits(list);
          setCommitsHasMore(list.length === COMMITS_PAGE_SIZE);
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

  const loadMoreCommits = async () => {
    if (commitsLoading || !commitsHasMore) return;
    setCommitsLoading(true);
    setCommitsError('');
    try {
      const next = await apiClient.get(
        `/api/repos/${owner}/${repo}/commits/${currentBranch}?limit=${COMMITS_PAGE_SIZE}&offset=${commits.length}`
      );
      const list = next || [];
      setCommits((prev) => [...prev, ...list]);
      setCommitsHasMore(list.length === COMMITS_PAGE_SIZE);
    } catch (err) {
      setCommitsError(err.message || 'Failed to load more commits');
    } finally {
      setCommitsLoading(false);
    }
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
            color: 'var(--gb-fg-2)',
            display: 'flex',
            alignItems: 'center',
            gap: '0.4rem',
            cursor: 'pointer',
            fontSize: 13,
            fontWeight: 500,
            marginBottom: '0.75rem'
          }}
        >
          <ArrowLeft size={14} /> Back to dashboard
        </button>

        <div className="gb-repo-header" style={{ paddingLeft: 0, paddingRight: 0 }}>
          <h1 className="title">
            <span className="owner">{meta.owner}</span>
            <span className="slash">/</span>
            <span className="repo">{meta.name}</span>
            <span className="gb-chip" style={{ marginLeft: 4 }}>
              {meta.visibility === 'private' ? <Lock size={11} /> : <Globe size={11} />}
              {meta.visibility}
            </span>
          </h1>
          {meta.description && (
            <p className="desc">{meta.description}</p>
          )}
          <div className="stats">
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
              <GitBranch size={13} color="var(--gb-fg-3)" /> <span className="mono" style={{ fontSize: 11.5 }}>{meta.defaultBranch || 'main'}</span>
            </span>
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
              <GitBranch size={13} color="var(--gb-fg-3)" /> {(meta.branches || []).length} branches
            </span>
            {tags != null && (
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
                <Hash size={13} color="var(--gb-fg-3)" /> {tags.length} tags
              </span>
            )}
            {collaboratorsCount != null && (
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
                <Users size={13} color="var(--gb-fg-3)" /> {collaboratorsCount + 1} collaborators
              </span>
            )}
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
        <button
          className={`tab ${activeTab === 'insights' ? 'active' : ''}`}
          onClick={() => onNavigate('repository', { owner, repo, tab: 'insights' })}
        >
          <Activity size={18} />
          Insights
          <span
            style={{
              marginLeft: '0.4rem', fontFamily: 'var(--gb-mono)', fontSize: '9.5px',
              letterSpacing: '0.06em', textTransform: 'uppercase', color: 'var(--gb-accent)',
              background: 'var(--gb-accent-bg)', border: '1px solid var(--gb-line-accent)',
              borderRadius: '4px', padding: '0.05rem 0.3rem', lineHeight: 1.4,
            }}
          >
            New
          </span>
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
              <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 14 }}>
                <BranchTagPicker
                  owner={owner}
                  repo={repo}
                  branches={meta.branches || []}
                  defaultBranch={meta.defaultBranch || 'main'}
                  currentBranch={currentBranch}
                  onChange={(ref) => {
                    setCurrentBranch(ref);
                    setViewingFile(null);
                    setFileContent('');
                  }}
                />

                {(currentPath || viewingFile) && (
                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.25rem', fontSize: '0.95rem' }}>
                    {currentPath.split('/').filter(Boolean).map((part, i, arr) => {
                      const isLast = i === arr.length - 1 && !viewingFile;
                      return (
                        <React.Fragment key={i}>
                          {i > 0 && <span style={{ color: '#64748b' }}>/</span>}
                          <span
                            style={{
                              color: isLast ? '#f8fafc' : '#38bdf8',
                              fontWeight: isLast ? 600 : 400,
                              cursor: 'pointer',
                            }}
                            onClick={() => handleBreadcrumbClick(i)}
                          >
                            {part}
                          </span>
                        </React.Fragment>
                      );
                    })}
                    {viewingFile && (
                      <>
                        {currentPath.split('/').filter(Boolean).length > 0 && <span style={{ color: '#64748b' }}>/</span>}
                        <span style={{ color: '#f8fafc', fontWeight: 600 }}>{viewingFile.name}</span>
                      </>
                    )}
                  </div>
                )}

                <span className="mono" style={{ fontSize: 12, color: 'var(--gb-fg-4)' }}>
                  {(meta.branches || []).length} branches{tags != null ? ` · ${tags.length} tags` : ''}
                </span>

                <button
                  type="button"
                  onClick={copyCloneUrl}
                  title="Copy clone URL"
                  style={{
                    marginLeft: 'auto', display: 'inline-flex', alignItems: 'center', gap: 8,
                    padding: '6px 10px', borderRadius: 7, background: 'var(--gb-surface)',
                    border: '1px solid var(--gb-line)', fontFamily: 'var(--gb-mono)',
                    fontSize: 12, color: 'var(--gb-fg-4)', cursor: 'pointer', maxWidth: 320,
                  }}
                >
                  <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: 220 }}>{cloneUrl}</span>
                  {copied ? <Check size={13} style={{ color: 'var(--gb-accent)' }} /> : <Copy size={13} />}
                </button>

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
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 280px', gap: 18, alignItems: 'start' }}>
                  {/* MAIN COLUMN */}
                  <div>
                    {currentBranch && (
                      <LatestCommitBar
                        owner={owner}
                        repo={repo}
                        branch={currentBranch}
                        onViewCommits={() => onNavigate('repository', { owner, repo, tab: 'commits' })}
                      />
                    )}
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
                              {codeowners[item.name] && codeowners[item.name].length > 0 && (
                                <span style={{ display: 'inline-flex', gap: 5, flexWrap: 'wrap', marginRight: 12 }}
                                      title={`CODEOWNERS: ${codeowners[item.name].join(', ')}`}>
                                  {codeowners[item.name].map((o) => (
                                    <span key={o} style={{
                                      height: 18, lineHeight: '18px', padding: '0 7px', borderRadius: 999,
                                      border: '1px solid var(--gb-line)', fontSize: 10.5, color: 'var(--gb-fg-3)',
                                      fontFamily: 'var(--gb-mono)', whiteSpace: 'nowrap',
                                    }}>{o}</span>
                                  ))}
                                </span>
                              )}
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
                              {codeowners[item.name] && codeowners[item.name].length > 0 && (
                                <span style={{ display: 'inline-flex', gap: 5, flexWrap: 'wrap', marginRight: 12 }}
                                      title={`CODEOWNERS: ${codeowners[item.name].join(', ')}`}>
                                  {codeowners[item.name].map((o) => (
                                    <span key={o} style={{
                                      height: 18, lineHeight: '18px', padding: '0 7px', borderRadius: 999,
                                      border: '1px solid var(--gb-line)', fontSize: 10.5, color: 'var(--gb-fg-3)',
                                      fontFamily: 'var(--gb-mono)', whiteSpace: 'nowrap',
                                    }}>{o}</span>
                                  ))}
                                </span>
                              )}
                              <span className="file-size">{(item.size / 1024).toFixed(1)} KB</span>
                            </div>
                          ))}
                        </>
                      )}
                    </div>

                    {/* Quickstart for empty repo (owner only) */}
                    {isOwner && treeItems.length === 0 && !currentPath && (
                      <div style={{ marginTop: '1.5rem' }}>
                        <QuickstartCard cloneUrl={cloneUrl} username={user.username} />
                      </div>
                    )}

                    {/* Suggest adding a README when the repo has files but none at root */}
                    {isOwner && treeItems.length > 0 && !currentPath && !readmeContent && (
                      <div className="glass-card" style={{ marginTop: '1.5rem', display: 'flex', gap: '0.85rem', alignItems: 'flex-start' }}>
                        <FileText size={20} style={{ color: '#a78bfa', flexShrink: 0, marginTop: '0.15rem' }} />
                        <div>
                          <div style={{ fontWeight: 600, color: '#f8fafc', marginBottom: '0.35rem' }}>
                            Help people understand your project
                          </div>
                          <div style={{ color: '#94a3b8', fontSize: '0.9rem', lineHeight: 1.5 }}>
                            Add a <code style={{ fontFamily: 'var(--font-mono)', background: 'rgba(255,255,255,0.06)', padding: '0.1rem 0.35rem', borderRadius: '4px' }}>README.md</code> to the root of this repository to describe what it does, how to use it, and how to contribute.
                          </div>
                        </div>
                      </div>
                    )}

                    {/* README Renderer */}
                    {readmeContent && (
                      <div className="readme-box">
                        <div className="readme-header">
                          <FileText size={18} style={{ color: '#a78bfa' }} />
                          <span>README.md</span>
                        </div>
                        <ReadmeBody content={readmeContent} />
                      </div>
                    )}
                  </div>

                  {/* RIGHT RAIL */}
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
                    <AboutRailCard meta={meta} collaboratorsCount={collaboratorsCount} commits={commits} />
                    {mix.total > 0 && <FileMixCard mix={mix} />}
                    <BranchesRailCard
                      branches={meta.branches || []}
                      defaultBranch={meta.defaultBranch || 'main'}
                      currentBranch={currentBranch}
                      onSelect={(b) => { setCurrentBranch(b); setViewingFile(null); setFileContent(''); }}
                    />
                    {tags && tags.length > 0 && <TagsRailCard tags={tags} />}
                    {tagsError && (!tags || tags.length === 0) && (
                      <Card style={{ padding: 16 }}>
                        <SectionHead kicker="TAGS" title="" />
                        <div style={{ fontSize: 12, color: 'var(--gb-err)' }}>{tagsError}</div>
                      </Card>
                    )}
                  </div>
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
                      <div className="commit-message" style={{ display: 'flex', alignItems: 'center' }}>
                        {commit.message}
                        {commit.overallStatus && (
                          <span 
                            style={{
                              display: 'inline-flex',
                              alignItems: 'center',
                              gap: '0.25rem',
                              fontSize: '0.7rem',
                              padding: '0.1rem 0.4rem',
                              borderRadius: '9999px',
                              background: commit.overallStatus === 'SUCCESS' ? 'rgba(16, 185, 129, 0.1)' : 
                                          (commit.overallStatus === 'FAILURE' || commit.overallStatus === 'TIMEOUT' || commit.overallStatus === 'CANCELLED') ? 'rgba(239, 68, 68, 0.1)' : 
                                          'rgba(59, 130, 246, 0.1)',
                              color: commit.overallStatus === 'SUCCESS' ? '#10b981' : 
                                     (commit.overallStatus === 'FAILURE' || commit.overallStatus === 'TIMEOUT' || commit.overallStatus === 'CANCELLED') ? '#ef4444' : 
                                     '#3b82f6',
                              border: `1px solid ${commit.overallStatus === 'SUCCESS' ? 'rgba(16, 185, 129, 0.2)' : 
                                                    (commit.overallStatus === 'FAILURE' || commit.overallStatus === 'TIMEOUT' || commit.overallStatus === 'CANCELLED') ? 'rgba(239, 68, 68, 0.2)' : 
                                                    'rgba(59, 130, 246, 0.2)'}`,
                              marginLeft: '0.75rem',
                              cursor: 'pointer'
                            }}
                            title={`Status: ${commit.overallStatus}. Click to view build logs.`}
                            onClick={(e) => {
                              e.stopPropagation();
                              if (commit.statuses && commit.statuses.length > 0) {
                                const buildId = commit.statuses[0].buildId;
                                onNavigate('build_logs', { owner, repo, sha: commit.sha, buildId });
                              }
                            }}
                          >
                            <span style={{ 
                              width: '6px', 
                              height: '6px', 
                              borderRadius: '50%', 
                              background: commit.overallStatus === 'SUCCESS' ? '#10b981' : 
                                          (commit.overallStatus === 'FAILURE' || commit.overallStatus === 'TIMEOUT' || commit.overallStatus === 'CANCELLED') ? '#ef4444' : 
                                          '#3b82f6' 
                            }}></span>
                            {commit.overallStatus.toLowerCase()}
                          </span>
                        )}
                      </div>
                      <div className="commit-meta">
                        <span style={{ color: '#f8fafc', fontWeight: 600 }}>{commit.authorName}</span>
                        <span>committed on {new Date(commit.date).toLocaleDateString()}</span>
                      </div>
                    </div>
                    <span className="commit-sha">{commit.sha.substring(0, 7)}</span>
                  </div>
                ))
              )}
              {commitsError && (
                <div className="error-box" style={{ margin: '0.5rem 1rem' }}>{commitsError}</div>
              )}
              {commitsHasMore && commits.length > 0 && (
                <div className="load-more-row">
                  <button
                    type="button"
                    className="load-more-btn"
                    onClick={loadMoreCommits}
                    disabled={commitsLoading}
                  >
                    {commitsLoading ? 'Loading...' : 'Load more'}
                  </button>
                </div>
              )}
            </div>
          )}

          {/* Settings Tab (Owner Only) */}
          {activeTab === 'settings' && isOwner && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '2rem' }}>
              {/* Collaborators */}
              <div className="glass-card">
                <h3 style={{ fontSize: '1.25rem', marginBottom: '1rem', color: '#38bdf8' }}>Collaborators</h3>
                <p style={{ color: '#94a3b8', fontSize: '0.9rem', marginBottom: '1rem' }}>
                  Users with push and read access to this repository.
                </p>
                <div style={{ display: 'flex', gap: '0.5rem', marginBottom: '1rem' }}>
                  <input
                    className="text-input"
                    placeholder="username"
                    value={newCollabUsername}
                    onChange={(e) => setNewCollabUsername(e.target.value)}
                    style={{ flex: 1 }}
                  />
                  <button className="btn btn-secondary" onClick={addCollaborator} disabled={!newCollabUsername.trim()}>Add</button>
                </div>
                {collaboratorError && <div style={{ color: '#ef4444', marginBottom: '0.5rem' }}>{collaboratorError}</div>}
                <ul style={{ listStyle: 'none', padding: 0, margin: 0 }}>
                  {collaborators.map((c) => (
                    <li key={c.uid} style={{ display: 'flex', justifyContent: 'space-between', padding: '0.5rem 0', borderBottom: '1px solid var(--border-color)' }}>
                      <span>{c.username}</span>
                      <button className="btn-ghost" onClick={() => removeCollaborator(c.username)}>Remove</button>
                    </li>
                  ))}
                  {collaborators.length === 0 && <li style={{ color: '#64748b' }}>No collaborators yet.</li>}
                </ul>
              </div>

              {/* Branch protection */}
              <div className="glass-card">
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem' }}>
                  <h3 style={{ fontSize: '1.25rem', margin: 0, color: '#38bdf8' }}>Branch protection</h3>
                  <button
                    className="btn btn-primary"
                    onClick={() => { setEditingRule(null); setShowProtectionModal(true); }}
                    style={{ padding: '0.4rem 0.9rem', fontSize: '0.85rem' }}
                  >
                    Add rule
                  </button>
                </div>
                <p style={{ color: '#94a3b8', fontSize: '0.9rem', marginBottom: '1rem' }}>
                  Restrict who can push or merge to matching branches, require pull requests, and block destructive operations.
                </p>
                {protectionError && (
                  <div style={{ color: '#ef4444', marginBottom: '0.75rem', fontSize: '0.85rem' }}>{protectionError}</div>
                )}
                {protectionRules.length === 0 ? (
                  <div style={{ color: '#64748b', fontSize: '0.9rem', padding: '0.5rem 0' }}>
                    No branch protection rules. Click "Add rule" to create one.
                  </div>
                ) : (
                  <div style={{ overflowX: 'auto' }}>
                    <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.9rem' }}>
                      <thead>
                        <tr style={{ textAlign: 'left', color: '#94a3b8', borderBottom: '1px solid var(--border-color)' }}>
                          <th style={{ padding: '0.5rem 0.5rem', fontWeight: 600 }}>Pattern</th>
                          <th style={{ padding: '0.5rem 0.5rem', fontWeight: 600 }}>Push</th>
                          <th style={{ padding: '0.5rem 0.5rem', fontWeight: 600 }}>Merge</th>
                          <th style={{ padding: '0.5rem 0.5rem', fontWeight: 600 }}>Toggles</th>
                          <th style={{ padding: '0.5rem 0.5rem', fontWeight: 600, textAlign: 'right' }}>Actions</th>
                        </tr>
                      </thead>
                      <tbody>
                        {protectionRules.map((r) => {
                          const badges = [];
                          if (r.requirePullRequest) badges.push('PR required');
                          if (r.requireApprovals && r.requireApprovals > 0) badges.push(`${r.requireApprovals} approval${r.requireApprovals === 1 ? '' : 's'}`);
                          if (r.requireCodeownerApproval) badges.push('Code-owner');
                          if (r.blockForcePush) badges.push('No force push');
                          if (r.blockDeletion) badges.push('No delete');
                          return (
                            <tr key={r.id} style={{ borderBottom: '1px solid var(--border-color)' }}>
                              <td style={{ padding: '0.6rem 0.5rem', fontFamily: 'var(--font-mono)', color: '#e2e8f0' }}>{r.pattern}</td>
                              <td style={{ padding: '0.6rem 0.5rem', color: '#cbd5e1' }}>
                                {Array.isArray(r.pushAllowlist) ? r.pushAllowlist.length : 0}
                              </td>
                              <td style={{ padding: '0.6rem 0.5rem', color: '#cbd5e1' }}>
                                {Array.isArray(r.mergeAllowlist) ? r.mergeAllowlist.length : 0}
                              </td>
                              <td style={{ padding: '0.6rem 0.5rem' }}>
                                {badges.length === 0 ? (
                                  <span style={{ color: '#64748b', fontSize: '0.8rem' }}>—</span>
                                ) : (
                                  <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.3rem' }}>
                                    {badges.map((b) => (
                                      <span key={b} style={{
                                        background: 'rgba(56, 189, 248, 0.12)',
                                        border: '1px solid rgba(56, 189, 248, 0.3)',
                                        color: '#38bdf8',
                                        padding: '0.1rem 0.5rem',
                                        borderRadius: '999px',
                                        fontSize: '0.75rem',
                                        fontWeight: 500,
                                      }}>{b}</span>
                                    ))}
                                  </div>
                                )}
                              </td>
                              <td style={{ padding: '0.6rem 0.5rem', textAlign: 'right', whiteSpace: 'nowrap' }}>
                                <button
                                  className="btn-ghost"
                                  onClick={() => { setEditingRule(r); setShowProtectionModal(true); }}
                                  style={{ marginRight: '0.5rem' }}
                                >
                                  Edit
                                </button>
                                <button
                                  className="btn-ghost"
                                  onClick={() => { if (window.confirm(`Delete rule for "${r.pattern}"?`)) handleProtectionDelete(r.id); }}
                                  style={{ color: '#ef4444' }}
                                >
                                  Delete
                                </button>
                              </td>
                            </tr>
                          );
                        })}
                      </tbody>
                    </table>
                  </div>
                )}
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

              {/* Manage Branches */}
              <div className="glass-card">
                <h3 style={{ fontSize: '1.25rem', marginBottom: '1rem', color: '#38bdf8' }}>Manage Branches</h3>
                <p style={{ color: '#94a3b8', fontSize: '0.9rem', marginBottom: '1.5rem' }}>
                  Manage the branches in this repository. The default branch cannot be deleted.
                </p>
                <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
                  {meta && meta.branches && meta.branches.map(branchName => {
                    const isDefault = branchName === (meta.defaultBranch || 'main');
                    return (
                      <div 
                        key={branchName}
                        style={{
                          display: 'flex',
                          alignItems: 'center',
                          justifyContent: 'space-between',
                          padding: '0.75rem 1rem',
                          background: 'rgba(255, 255, 255, 0.02)',
                          border: '1px solid var(--border-color)',
                          borderRadius: '8px'
                        }}
                      >
                        <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
                          <span style={{ fontFamily: 'var(--font-mono)', fontWeight: 600, color: '#f8fafc' }}>
                            {branchName}
                          </span>
                          {isDefault && (
                            <span style={{
                              fontSize: '0.7rem',
                              background: 'rgba(56, 189, 248, 0.15)',
                              border: '1px solid rgba(56, 189, 248, 0.3)',
                              color: '#38bdf8',
                              padding: '0.1rem 0.4rem',
                              borderRadius: '4px',
                              fontWeight: 600
                            }}>
                              default
                            </span>
                          )}
                        </div>
                        
                        {!isDefault && (
                          <button
                            className="btn btn-danger"
                            style={{
                              padding: '0.35rem 0.75rem',
                              fontSize: '0.8rem'
                            }}
                            onClick={async () => {
                              if (window.confirm(`Are you sure you want to delete branch "${branchName}"?`)) {
                                try {
                                  await apiClient.delete(`/api/repos/${owner}/${repo}/branches/${branchName}`);
                                  loadMetadata();
                                } catch (err) {
                                  alert('Failed to delete branch: ' + (err.response?.data || err.message));
                                }
                              }
                            }}
                          >
                            Delete
                          </button>
                        )}
                      </div>
                    );
                  })}
                </div>
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

          {/* Insights Tab */}
          {activeTab === 'insights' && (
            <InsightsTab
              meta={meta}
              commits={insightsCommits}
              tags={tags}
              pulls={insightsPulls}
              collaborators={insightsCollaborators}
              protectionRules={insightsRules}
              codeownersRoot={codeownersRoot}
              isOwner={isOwner}
              loading={insightsLoading || !insightsLoaded}
              onNavigate={onNavigate}
              owner={owner}
              repo={repo}
            />
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

      {/* Branch Protection Modal */}
      {showProtectionModal && (
        <BranchProtectionModal
          key={editingRule ? `edit-${editingRule.id}` : 'create'}
          mode={editingRule ? 'edit' : 'create'}
          initialRule={editingRule}
          owner={owner}
          collaborators={collaborators}
          onClose={() => { setShowProtectionModal(false); setEditingRule(null); }}
          onSubmit={handleProtectionSubmit}
        />
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
  const [prFilter, setPrFilter] = useState('open');

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

  const counts = pulls.reduce(
    (acc, p) => { acc[p.status] = (acc[p.status] || 0) + 1; return acc; },
    { open: 0, closed: 0, merged: 0 }
  );
  const filteredPulls = pulls.filter((p) => p.status === prFilter);

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

      <div className="pr-pills">
        {[
          { key: 'open', label: 'Open' },
          { key: 'closed', label: 'Closed' },
          { key: 'merged', label: 'Merged' },
        ].map((p) => (
          <button
            key={p.key}
            type="button"
            className={`pr-pill ${prFilter === p.key ? 'active' : ''}`}
            onClick={() => setPrFilter(p.key)}
          >
            {p.label}
            <span className="pr-pill-count">{counts[p.key] || 0}</span>
          </button>
        ))}
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

      {!loading && !error && filteredPulls.length === 0 && (
        <div className="glass-card" style={{ padding: '3rem', textAlign: 'center', display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '1rem' }}>
          <GitPullRequest size={48} style={{ color: '#64748b' }} />
          <h3 style={{ margin: 0, fontSize: '1.2rem', color: '#f8fafc' }}>
            No {prFilter} pull requests
          </h3>
          {pulls.length === 0 && (
            <>
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
            </>
          )}
        </div>
      )}

      {!loading && !error && filteredPulls.length > 0 && (
        <div className="pr-list">
          {filteredPulls.map(pr => {
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
            color: 'var(--gb-fg-2)',
            display: 'flex',
            alignItems: 'center',
            gap: '0.4rem',
            cursor: 'pointer',
            fontSize: 13,
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


