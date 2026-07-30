import React, { useState, useEffect, useMemo } from 'react';
import { apiClient } from '../apiClient';
import { authService } from '../authService';
import BranchProtectionModal from '../components/BranchProtectionModal';
import BranchTagPicker from '../components/BranchTagPicker';
import LatestCommitBar from '../components/LatestCommitBar';
import Card from '../components/Card';
import Chip from '../components/Chip';
import SectionHead from '../components/SectionHead';
import Avatar from '../components/Avatar';
import { formatRelative } from '../utils/relativeTime';
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
  GitMerge,
  ChevronRight,
  ChevronDown,
  Search,
  Hash,
  Users,
  Activity,
  GitCommit,
  Tag,
  ShieldCheck,
  CheckCircle2,
  XCircle,
  MessageSquareText,
} from 'lucide-react';

// --- Task 5: PR detail helpers ---
function globMatch(pattern, value) {
  if (!pattern) return false;
  const re = new RegExp('^' + pattern
    .replace(/[.+^${}()|[\]\\]/g, '\\$&')
    .replace(/\*/g, '[^/]*')
    .replace(/\?/g, '[^/]') + '$');
  return re.test(value);
}
function getMatchingRule(rules, targetBranch) {
  if (!Array.isArray(rules) || !targetBranch) return null;
  const wildcardCount = (p) => [...p].filter((c) => c === '*' || c === '?').length;
  // ⚡ Bolt: Schwartzian transform to precompute wildcardCount (O(N) instead of O(N log N)).
  const decorated = rules.map(r => ({ r, wc: wildcardCount(r.pattern), len: r.pattern.length }));
  decorated.sort((a, b) => {
    if (a.wc !== b.wc) return a.wc - b.wc;                       // fewer wildcards = more specific
    if (a.len !== b.len) return b.len - a.len;                   // longer = more specific
    return a.r.pattern < b.r.pattern ? -1 : 1;                   // lexicographic tiebreak
  });
  const sorted = decorated.map(d => d.r);
  return sorted.find((r) => globMatch(r.pattern, targetBranch)) || null;
}
function buildReviewerList(requestedReviewers, reviews) {
  const byUser = new Map();
  for (const username of (requestedReviewers || [])) {
    byUser.set(username.toLowerCase(), { username, status: 'pending', role: 'Requested · CODEOWNERS' });
  }
  for (const r of (reviews || [])) {
    const key = r.username.toLowerCase();
    byUser.set(key, { username: r.username, status: r.state, submittedAt: r.submittedAt, body: r.body, role: byUser.get(key)?.role || 'Reviewer' });
  }
  return [...byUser.values()];
}
function approvalsProgress(rule, reviews) {
  if (!rule) return null;
  const distinct = new Map();
  for (const r of (reviews || [])) distinct.set(r.username.toLowerCase(), r);
  const vals = [...distinct.values()];
  const given = vals.filter(r => r.state === 'approved').length;
  const changesRequested = vals.filter(r => r.state === 'changes_requested').map(r => r.username);
  return {
    required: rule.requireApprovals || 0,
    given,
    changesRequested,
    codeownersRequired: !!rule.requireCodeownerApproval,
    satisfied: given >= (rule.requireApprovals || 0) && changesRequested.length === 0,
  };
}
function reviewMeta(status) {
  switch (status) {
    case 'approved':          return { variant: 'ok',  Icon: CheckCircle2,      color: 'var(--gb-ok)',   label: 'Approved' };
    case 'changes_requested': return { variant: 'err', Icon: XCircle,           color: 'var(--gb-err)',  label: 'Changes requested' };
    case 'commented':         return { variant: 'default', Icon: MessageSquareText, color: 'var(--gb-fg-3)', label: 'Commented' };
    default:                  return { variant: 'warn', Icon: Clock,            color: 'var(--gb-warn)', label: 'Pending' };
  }
}
function buildStatusMeta(overallStatus) {
  const s = (overallStatus || '').toUpperCase();
  if (s === 'SUCCESS')
    return { variant: 'ok',  Icon: CheckCircle2,  color: 'var(--gb-ok)',   bg: 'var(--gb-ok-dim)',   chip: 'SUCCESS', title: 'Build passing on head commit' };
  if (s === 'FAILURE' || s === 'TIMEOUT' || s === 'CANCELLED')
    return { variant: 'err', Icon: AlertTriangle, color: 'var(--gb-err)',  bg: 'var(--gb-err-dim)',  chip: s,        title: `Build ${s.toLowerCase()} on head commit` };
  return { variant: 'warn', Icon: Clock, color: 'var(--gb-warn)', bg: 'var(--gb-warn-dim)', chip: s || 'PENDING', title: 'Build pending — no status reported' };
}

function renderReadme(markdown) {
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

  // Italic: *text* and _text_ (underscore form ignores snake_case)
  html = html.replace(/\*([^*\n]+)\*/g, '<em>$1</em>');
  html = html.replace(/(^|[^A-Za-z0-9])_([^_\n]+)_(?![A-Za-z0-9])/g, '$1<em>$2</em>');

  // Links: [text](url) — restrict to safe schemes; leave others as raw text
  html = html.replace(/\[([^\]\n]+)\]\(([^)\s]+)\)/g, (match, text, url) => {
    if (!/^(https?:\/\/|mailto:|\/|#)/i.test(url)) return match;
    const href = url.replace(/"/g, '&quot;');
    return `<a href="${href}" target="_blank" rel="noopener noreferrer" style="color: #38bdf8; text-decoration: none;">${text}</a>`;
  });

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
}

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
// Task 5: PR Detail Sub-components
// ==========================================

function ApprovalsCallout({ approvals, matchingRule, mergeAllowed, onMerge, merging }) {
  const meta = !matchingRule
    ? { Icon: CheckCircle2, color: 'var(--gb-ok)', bg: 'var(--gb-ok-dim)' }
    : approvals.satisfied
      ? { Icon: CheckCircle2, color: 'var(--gb-ok)', bg: 'var(--gb-ok-dim)' }
      : approvals.changesRequested.length
        ? { Icon: XCircle, color: 'var(--gb-err)', bg: 'var(--gb-err-dim)' }
        : { Icon: Clock, color: 'var(--gb-warn)', bg: 'var(--gb-warn-dim)' };
  const Bubble = meta.Icon;
  const headline = !matchingRule ? 'No approvals required' : `Approvals · ${approvals.given} of ${approvals.required}`;
  const subhead = !matchingRule
    ? 'No protection rule applies — anyone with write access can merge.'
    : [
        `Rule for ${matchingRule.pattern} requires ${approvals.required} approval${approvals.required === 1 ? '' : 's'}${approvals.codeownersRequired ? ' + CODEOWNERS' : ''}`,
        approvals.changesRequested.length ? `changes requested by ${approvals.changesRequested.map(u => '@' + u).join(', ')}` : null,
      ].filter(Boolean).join(' · ');
  return (
    <Card style={{ padding: 0, marginTop: 16 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 14, padding: '14px 18px' }}>
        <div style={{ width: 36, height: 36, borderRadius: '50%', background: meta.bg, color: meta.color, display: 'grid', placeItems: 'center', flexShrink: 0 }}>
          <Bubble size={18} />
        </div>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontSize: 14, fontWeight: 600, color: 'var(--gb-fg)' }}>{headline}</div>
          <div style={{ fontSize: 12, color: 'var(--gb-fg-3)' }}>{subhead}</div>
        </div>
        <button type="button" onClick={onMerge} disabled={!mergeAllowed || merging}
          style={{ display: 'inline-flex', alignItems: 'center', gap: 6, padding: '7px 12px', borderRadius: 6, fontSize: 13, border: '1px solid var(--gb-line)', background: 'var(--gb-surface-2)', color: 'var(--gb-fg)', cursor: mergeAllowed && !merging ? 'pointer' : 'not-allowed', opacity: mergeAllowed && !merging ? 1 : 0.6, flexShrink: 0 }}>
          <GitMerge size={13} /> {merging ? 'Merging…' : 'Merge pull request'}
        </button>
      </div>
    </Card>
  );
}

function BuildCallout({ meta, headCommit, buildId, owner, repo, onNavigate }) {
  const short = headCommit.sha.substring(0, 7);
  const isFail = meta.variant === 'err';
  const Bubble = meta.Icon;
  return (
    <Card style={{ padding: 0, marginTop: 16 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 14, padding: '14px 18px' }}>
        <div style={{ width: 36, height: 36, borderRadius: '50%', background: meta.bg, color: meta.color, display: 'grid', placeItems: 'center', flexShrink: 0 }}>
          <Bubble size={18} />
        </div>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontSize: 14, fontWeight: 600, color: 'var(--gb-fg)' }}>{meta.title}</div>
          <div style={{ fontSize: 12, color: 'var(--gb-fg-3)' }}>
            <span style={{ fontFamily: 'var(--gb-mono)' }}>{short}</span> · status: {meta.chip}
            {isFail && buildId && (
              <>
                {' · '}
                <a role="button" tabIndex={0}
                   onClick={() => onNavigate('build_logs', { owner, repo, sha: headCommit.sha, buildId })}
                   style={{ color: 'var(--gb-err)', textDecoration: 'underline', cursor: 'pointer' }}>view build logs →</a>
              </>
            )}
          </div>
        </div>
        <Chip variant={meta.variant}>{meta.chip}</Chip>
      </div>
    </Card>
  );
}

function ReviewerRail({ reviewers }) {
  return (
    <Card style={{ padding: 16 }}>
      <SectionHead kicker="REVIEWERS" title="" />
      {reviewers.length === 0 ? (
        <div style={{ fontSize: 12.5, color: 'var(--gb-fg-3)' }}>No reviewers yet.</div>
      ) : (
        <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 9 }}>
          {reviewers.map(r => {
            const m = reviewMeta(r.status);
            const RowIcon = m.Icon;
            return (
              <li key={r.username} style={{ display: 'grid', gridTemplateColumns: '22px 1fr 14px', gap: 9, alignItems: 'center' }}>
                <Avatar name={r.username} size={22} />
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontSize: 12.5, fontWeight: 500, display: 'flex', alignItems: 'center', gap: 6 }}>
                    <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{r.username}</span>
                    <span style={{ fontSize: 10.5, color: m.color, fontWeight: 500, flexShrink: 0 }}>· {m.label}</span>
                  </div>
                  <div style={{ fontSize: 10.5, color: 'var(--gb-fg-4)' }}>{r.role}</div>
                </div>
                <RowIcon size={13} color={m.color} strokeWidth={2.4} />
              </li>
            );
          })}
        </ul>
      )}
    </Card>
  );
}

function RuleMatchedCard({ rule, targetBranch }) {
  if (!rule) return null;
  const lines = [
    rule.requirePullRequest ? 'PR required' : null,
    rule.requireApprovals > 0 ? `${rule.requireApprovals} approval${rule.requireApprovals === 1 ? '' : 's'} required` : null,
    rule.requireCodeownerApproval ? 'CODEOWNERS review required' : null,
    (rule.blockForcePush || rule.blockDeletion)
      ? `No ${[rule.blockForcePush && 'force-push', rule.blockDeletion && 'delete'].filter(Boolean).join(', no ')}`
      : null,
  ].filter(Boolean);
  return (
    <Card style={{ padding: 16 }}>
      <SectionHead kicker="RULE MATCHED" title="" />
      <div style={{ fontSize: 12, color: 'var(--gb-fg-3)', marginBottom: 8 }}>
        Target <span style={{ fontFamily: 'var(--gb-mono)' }}>{targetBranch}</span> matches protection rule
        {' '}<span style={{ fontFamily: 'var(--gb-mono)' }}>{rule.pattern}</span>:
      </div>
      <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 4 }}>
        {lines.map((l, i) => (
          <li key={i} style={{ fontSize: 12.5, color: 'var(--gb-fg-2)' }}>
            <span style={{ color: 'var(--gb-fg-4)', marginRight: 6 }}>·</span>{l}
          </li>
        ))}
      </ul>
    </Card>
  );
}

function CommitsList({ commits }) {
  return (
    <Card style={{ padding: 0, overflow: 'hidden' }}>
      <div style={{ padding: '10px 14px', borderBottom: '1px solid var(--gb-line)', background: 'var(--gb-surface-2)' }}>
        <SectionHead kicker="COMMITS" title="" right={<span style={{ fontSize: 11, color: 'var(--gb-fg-4)' }}>{commits.length}</span>} />
      </div>
      {commits.length === 0 ? (
        <div style={{ padding: '24px', textAlign: 'center', fontSize: 12.5, color: 'var(--gb-fg-3)' }}>No commits in this pull request.</div>
      ) : commits.map((c, i) => (
        <div key={c.sha} style={{ display: 'grid', gridTemplateColumns: '22px 1fr auto auto', gap: 10, alignItems: 'center', padding: '10px 14px', borderTop: i === 0 ? 'none' : '1px solid var(--gb-line)' }}>
          <Avatar name={c.authorName} size={22} />
          <span style={{ fontSize: 13, color: 'var(--gb-fg)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{(c.message || '').split('\n')[0]}</span>
          <code style={{ fontFamily: 'var(--gb-mono)', fontSize: 11.5, color: 'var(--gb-accent)', background: 'var(--gb-accent-bg)', padding: '1px 6px', borderRadius: 3 }}>{c.sha.substring(0, 7)}</code>
          <span style={{ fontSize: 11, color: 'var(--gb-fg-4)' }}>{new Date(c.date).toLocaleDateString()}</span>
        </div>
      ))}
    </Card>
  );
}

// ==========================================
// Insights Tab Components
// ==========================================

function InsightsTab({
  meta, commits, tags, pulls, collaborators, protectionRules,
  codeownersRoot, isOwner, loading, owner, repo, onNavigate,
}) {
  const branchCount = (meta?.branches || []).length;
  const tagCount = (tags || []).length;
  const openPrCount = (pulls || []).filter((p) => p.status === 'open').length;
  const collabCount = (collaborators || []).length;
  const ruleCount = (protectionRules || []).length;
  const commitCount = (commits || []).length;
  const commitLabel = commitCount >= COMMITS_PAGE_SIZE ? `${commitCount}+` : `${commitCount}`;

  const stats = [
    { kicker: 'COMMITS', value: commitLabel, icon: <GitCommit size={13} /> },
    { kicker: 'BRANCHES', value: `${branchCount}`, icon: <GitBranch size={13} /> },
    { kicker: 'TAGS', value: `${tagCount}`, icon: <Tag size={13} /> },
    { kicker: 'OPEN PRs', value: `${openPrCount}`, icon: <GitPullRequest size={13} /> },
    { kicker: 'COLLABS', value: `${collabCount + 1}`, icon: <Users size={13} /> },
    { kicker: 'PROT. RULES', value: isOwner ? `${ruleCount}` : '—', icon: <ShieldCheck size={13} /> },
  ];

  if (loading) {
    return <div className="loader-container"><div className="loader"></div></div>;
  }

  const coEntries = Object.entries(codeownersRoot || {});
  const recentCommits = (commits || []).slice(0, 8);
  const defaultBranch = meta?.defaultBranch || 'main';

  return (
    <div style={{ fontFamily: 'var(--gb-sans)' }}>
      <SectionHead kicker="OVERVIEW" title="Repository at a glance" />
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(6, 1fr)', gap: 12, marginTop: 12, marginBottom: 22 }}>
        {stats.map((s) => (
          <div key={s.kicker} style={{ padding: 14, background: 'var(--gb-surface)', border: '1px solid var(--gb-line)', borderRadius: 10 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 5, fontFamily: 'var(--gb-mono)', fontSize: 10, textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--gb-fg-4)', marginBottom: 6 }}>
              {s.icon}{s.kicker}
            </div>
            <div style={{ fontSize: 22, fontWeight: 600, letterSpacing: '-0.01em', color: 'var(--gb-fg)' }}>{s.value}</div>
          </div>
        ))}
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 18, alignItems: 'start' }}>
        <RecentCommitsPanel commits={recentCommits} owner={owner} repo={repo} onNavigate={onNavigate} />
        <div style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
          <CollaboratorsPanel meta={meta} collaborators={collaborators} />
          <ProtectionPanel rules={protectionRules} isOwner={isOwner} />
          <CodeownersPanel entries={coEntries} branch={defaultBranch} />
        </div>
      </div>
    </div>
  );
}

function RecentCommitsPanel({ commits, owner, repo, onNavigate }) {
  return (
    <Card style={{ padding: 16 }}>
      <SectionHead kicker="RECENT" title="Last 8 commits" />
      {commits.length === 0 ? (
        <div style={{ color: 'var(--gb-fg-4)', fontSize: 12.5, padding: '8px 0' }}>No commits yet.</div>
      ) : (
        <ul style={{ listStyle: 'none', margin: 0, padding: 0 }}>
          {commits.map((c) => (
            <li key={c.sha}
                onClick={() => onNavigate('commit', { owner, repo, sha: c.sha })}
                style={{ display: 'grid', gridTemplateColumns: '22px 1fr auto auto', gap: 10, alignItems: 'center', padding: '10px 0', cursor: 'pointer' }}
                onMouseEnter={(e) => (e.currentTarget.style.background = 'var(--gb-hover)')}
                onMouseLeave={(e) => (e.currentTarget.style.background = 'transparent')}>
              <Avatar name={c.authorName} size={22} />
              <span style={{ minWidth: 0, display: 'flex', gap: 7, alignItems: 'baseline' }}>
                <strong style={{ fontSize: 12.5, color: 'var(--gb-fg)', whiteSpace: 'nowrap' }}>{c.authorName}</strong>
                <span style={{ fontSize: 12, color: 'var(--gb-fg-3)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{c.message}</span>
              </span>
              <span style={{ fontFamily: 'var(--gb-mono)', fontSize: 11.5, color: 'var(--gb-fg-4)' }}>{c.sha.slice(0, 7)}</span>
              <span style={{ fontSize: 11, color: 'var(--gb-fg-4)', whiteSpace: 'nowrap' }}>{formatRelative(c.date)}</span>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

function CollaboratorsPanel({ meta, collaborators }) {
  const ownerName = meta?.owner || '';
  return (
    <Card style={{ padding: 16 }}>
      <SectionHead kicker="COLLABORATORS" title="" />
      <ul style={{ listStyle: 'none', margin: 0, padding: 0 }}>
        {ownerName && (
          <li key="__owner" style={{ display: 'grid', gridTemplateColumns: '24px 1fr auto', gap: 10, alignItems: 'center', padding: '9px 0' }}>
            <Avatar name={ownerName} size={24} />
            <span style={{ fontSize: 12.5, fontWeight: 600, color: 'var(--gb-fg)' }}>{ownerName}</span>
            <Chip variant="accent">owner</Chip>
          </li>
        )}
        {(collaborators || []).map((c) => (
          <li key={c.uid || c.username} style={{ display: 'grid', gridTemplateColumns: '24px 1fr auto auto', gap: 10, alignItems: 'center', padding: '9px 0', borderTop: '1px solid var(--gb-line)' }}>
            <Avatar name={c.username} size={24} />
            <span style={{ fontSize: 12.5, fontWeight: 500, color: 'var(--gb-fg-2)' }}>{c.username}</span>
            <span style={{ fontSize: 11, color: 'var(--gb-fg-4)', whiteSpace: 'nowrap' }}>{c.addedAt ? formatRelative(c.addedAt) : ''}</span>
            <Chip variant="default">collaborator</Chip>
          </li>
        ))}
        {ownerName && (collaborators || []).length === 0 && (
          <li style={{ fontSize: 12, color: 'var(--gb-fg-4)', padding: '9px 0', borderTop: '1px solid var(--gb-line)' }}>No additional collaborators.</li>
        )}
      </ul>
    </Card>
  );
}

function ProtectionPanel({ rules, isOwner }) {
  return (
    <Card style={{ padding: 16 }}>
      <SectionHead kicker="BRANCH PROTECTION" title="" />
      {!isOwner ? (
        <div style={{ fontSize: 12, color: 'var(--gb-fg-4)' }}>Branch protection settings are visible to repo owners only.</div>
      ) : (rules || []).length === 0 ? (
        <div style={{ fontSize: 12, color: 'var(--gb-fg-4)' }}>No protection rules defined.</div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
          {rules.map((rule) => (
            <div key={rule.id} style={{ display: 'flex', flexDirection: 'column', gap: 6, paddingBottom: 10 }}>
              <div style={{ display: 'flex', alignItems: 'baseline', gap: 8, flexWrap: 'wrap' }}>
                <span style={{ fontFamily: 'var(--gb-mono)', fontSize: 12, color: 'var(--gb-fg)' }}>{rule.pattern}</span>
                <span style={{ fontFamily: 'var(--gb-mono)', fontSize: 11, color: 'var(--gb-fg-4)' }}>
                  push:{(rule.pushAllowlist || []).length} · merge:{(rule.mergeAllowlist || []).length}
                </span>
              </div>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                {rule.requirePullRequest && <Chip variant="accent">PR required</Chip>}
                {rule.requireApprovals > 0 && <Chip variant="accent">{rule.requireApprovals} approval{rule.requireApprovals === 1 ? '' : 's'}</Chip>}
                {rule.requireCodeownerApproval && <Chip variant="accent">CODEOWNERS</Chip>}
                {rule.blockForcePush && <Chip variant="accent">no force-push</Chip>}
                {rule.blockDeletion && <Chip variant="accent">no delete</Chip>}
              </div>
            </div>
          ))}
        </div>
      )}
    </Card>
  );
}

function CodeownersPanel({ entries, branch }) {
  return (
    <Card style={{ padding: 16 }}>
      <SectionHead kicker="CODEOWNERS" title="" right={<span style={{ fontFamily: 'var(--gb-mono)', fontSize: 10.5, color: 'var(--gb-fg-4)' }}>{branch}</span>} />
      {(entries || []).length === 0 ? (
        <div style={{ fontSize: 12, color: 'var(--gb-fg-4)' }}>No CODEOWNERS rules on default branch.</div>
      ) : (
        <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 7 }}>
          {entries.map(([name, ownersList]) => (
            <li key={name} style={{ display: 'grid', gridTemplateColumns: '1fr auto', gap: 12, alignItems: 'baseline', fontFamily: 'var(--gb-mono)', fontSize: 11.5 }}>
              <span style={{ color: 'var(--gb-fg-2)' }}>{name}/</span>
              <span style={{ color: 'var(--gb-fg-3)', textAlign: 'right' }}>{(ownersList || []).join(' ')}</span>
            </li>
          ))}
        </ul>
      )}
    </Card>
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

function PullRequestDetail({ owner, repo, prNumber, meta, onNavigate, user }) {
  const [pr, setPr] = useState(null);
  const [commits, setCommits] = useState([]);
  const [diff, setDiff] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [prTab, setPrTab] = useState('conversation'); // 'conversation', 'commits', 'diff'

  // Detailed Diff and File Tree States
  const [filterText, setFilterText] = useState('');
  const [collapsedPaths, setCollapsedPaths] = useState({});
  const [collapsedFiles, setCollapsedFiles] = useState({});
  const [viewedFiles, setViewedFiles] = useState({});
  const [activeFile, setActiveFile] = useState(null);
  const [activeHighlight, setActiveHighlight] = useState(null);

  const parsedFiles = useMemo(() => {
    return parseDiff(diff);
  }, [diff]);

  const fileTree = useMemo(() => {
    return buildFileTree(parsedFiles);
  }, [parsedFiles]);

  const filteredTree = useMemo(() => {
    if (!filterText) return fileTree;
    const filterLower = filterText.toLowerCase();

    const filterNode = (nodeList) => {
      const result = [];
      for (const node of nodeList) {
        if (!node.isDirectory) {
          if (node.name.toLowerCase().includes(filterLower) || node.path.toLowerCase().includes(filterLower)) {
            result.push(node);
          }
        } else {
          const filteredChildren = filterNode(node.children);
          if (filteredChildren.length > 0) {
            result.push({ ...node, children: filteredChildren });
          }
        }
      }
      return result;
    };

    return filterNode(fileTree);
  }, [fileTree, filterText]);

  const filteredFiles = useMemo(() => {
    if (!filterText) return parsedFiles;
    const filterLower = filterText.toLowerCase();
    return parsedFiles.filter(f => f.path.toLowerCase().includes(filterLower));
  }, [parsedFiles, filterText]);

  const toggleFolder = (path) => {
    setCollapsedPaths(prev => ({
      ...prev,
      [path]: !prev[path]
    }));
  };

  const toggleFileCollapse = React.useCallback((path) => {
    setCollapsedFiles(prev => ({
      ...prev,
      [path]: !prev[path]
    }));
  }, []);

  const toggleViewed = React.useCallback((path) => {
    setViewedFiles(prev => {
      const next = { ...prev, [path]: !prev[path] };
      if (next[path]) {
        setCollapsedFiles(fPrev => ({ ...fPrev, [path]: true }));
      } else {
        setCollapsedFiles(fPrev => ({ ...fPrev, [path]: false }));
      }
      return next;
    });
  }, []);

  const scrollToFile = (path) => {
    setActiveFile(path);
    const element = document.getElementById(`diff-file-${path}`);
    if (element) {
      element.scrollIntoView({ behavior: 'smooth', block: 'start' });
      setActiveHighlight(path);
      setTimeout(() => {
        setActiveHighlight(null);
      }, 1500);
    }
  };

  const renderTreeNode = (node, depth = 0) => {
    const isCollapsed = collapsedPaths[node.path];
    const isNodeActive = activeFile === node.path;
    const isNodeViewed = !node.isDirectory && viewedFiles[node.path];

    if (node.isDirectory) {
      return (
        <div key={node.path}>
          <div
            className={`diff-tree-node ${isNodeActive ? 'active' : ''}`}
            style={{ paddingLeft: `${depth * 12 + 8}px` }}
            onClick={() => toggleFolder(node.path)}
          >
            <span className="diff-tree-icon">
              {isCollapsed ? <ChevronRight size={14} /> : <ChevronDown size={14} />}
            </span>
            <span className="diff-tree-icon" style={{ color: '#f59e0b' }}>
              <Folder size={14} fill="currentColor" fillOpacity={0.1} />
            </span>
            <span className="diff-tree-label">{node.name}</span>
            <span className="diff-tree-stats">
              {node.additions > 0 && <span className="diff-tree-stat-add">+{node.additions}</span>}
              {node.deletions > 0 && <span className="diff-tree-stat-del">-{node.deletions}</span>}
            </span>
          </div>
          {!isCollapsed && node.children.map(child => renderTreeNode(child, depth + 1))}
        </div>
      );
    } else {
      return (
        <div
          key={node.path}
          className={`diff-tree-node ${isNodeActive ? 'active' : ''} ${isNodeViewed ? 'viewed' : ''}`}
          style={{ paddingLeft: `${depth * 12 + 20}px` }}
          onClick={() => scrollToFile(node.path)}
        >
          <span className="diff-tree-icon" style={{ color: isNodeViewed ? '#64748b' : '#38bdf8' }}>
            <FileCode size={14} />
          </span>
          <span className="diff-tree-label" title={node.path}>{node.name}</span>
          <span className="diff-tree-stats">
            {node.additions > 0 && <span className="diff-tree-stat-add">+{node.additions}</span>}
            {node.deletions > 0 && <span className="diff-tree-stat-del">-{node.deletions}</span>}
          </span>
        </div>
      );
    }
  };

  const [comments, setComments] = useState([]);
  const [newComment, setNewComment] = useState('');

  const [actionLoading, setActionLoading] = useState(false);
  const [actionError, setActionError] = useState('');

  const [repoBranches, setRepoBranches] = useState([]);
  const [deletingBranch, setDeletingBranch] = useState(false);

  // Reviews
  const [reviews, setReviews] = useState([]);
  const [reviewError, setReviewError] = useState('');
  const [reviewSubmitting, setReviewSubmitting] = useState('');
  const [reviewBody, setReviewBody] = useState('');

  // Branch protection rules for PR detail
  const [prProtectionRules, setPrProtectionRules] = useState([]);

  const isOwner = user && user.username && user.username.toLowerCase() === owner.toLowerCase();
  const isPrAuthor = user && user.username && pr && pr.authorUsername && user.username.toLowerCase() === pr.authorUsername.toLowerCase();

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
        const [prData, commitsData, diffData, reviewsData, rulesData] = await Promise.all([
          apiClient.get(`/api/repos/${owner}/${repo}/pulls/${prNumber}`),
          apiClient.get(`/api/repos/${owner}/${repo}/pulls/${prNumber}/commits`).catch(() => []),
          apiClient.get(`/api/repos/${owner}/${repo}/pulls/${prNumber}/diff`).catch(() => ({ rawDiff: '' })),
          apiClient.get(`/api/repos/${owner}/${repo}/pulls/${prNumber}/reviews`).catch(() => []),
          apiClient.get(`/api/repos/${owner}/${repo}/branch-protection`).catch(() => []),
        ]);

        setPr(prData);
        setCommits(commitsData || []);
        setDiff(diffData?.rawDiff || '');
        setReviews(Array.isArray(reviewsData) ? reviewsData : []);
        setPrProtectionRules(Array.isArray(rulesData) ? rulesData : []);

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

  const refetchReviews = async () => {
    try {
      const list = await apiClient.get(`/api/repos/${owner}/${repo}/pulls/${prNumber}/reviews`);
      setReviews(Array.isArray(list) ? list : []);
    } catch {
      // Keep prior list on transient failure; surface error via reviewError separately.
    }
  };

  const handleSubmitReview = async (state) => {
    setReviewError('');
    setReviewSubmitting(state);
    try {
      await apiClient.post(`/api/repos/${owner}/${repo}/pulls/${prNumber}/reviews`, {
        state,
        body: reviewBody,
      });
      setReviewBody('');
      await refetchReviews();
    } catch (err) {
      setReviewError(err.message || 'Failed to submit review.');
    } finally {
      setReviewSubmitting('');
    }
  };

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

	const handleUpdateBranch = async (strategy) => {
		if (!window.confirm(`Are you sure you want to update this branch using ${strategy}?`)) return;
		setActionLoading(true);
		setActionError('');
		try {
			const resp = await apiClient.post(`/api/repos/${owner}/${repo}/pulls/${prNumber}/update`, { strategy });
			if (resp.success) {
				const [prData, commitsData, diffData] = await Promise.all([
					apiClient.get(`/api/repos/${owner}/${repo}/pulls/${prNumber}`),
					apiClient.get(`/api/repos/${owner}/${repo}/pulls/${prNumber}/commits`).catch(() => []),
					apiClient.get(`/api/repos/${owner}/${repo}/pulls/${prNumber}/diff`).catch(() => ({ rawDiff: '' }))
				]);
				setPr(prData);
				setCommits(commitsData || []);
				setDiff(diffData?.rawDiff || '');
			} else {
				setActionError(resp.message || `Failed to update branch using ${strategy}.`);
			}
		} catch (err) {
			setActionError(err.message || `Failed to update branch using ${strategy}. There might be unresolvable conflicts.`);
		} finally {
			setActionLoading(false);
		}
	};

  // Task 5: Derived hook values for approvals/build callouts.
  // Hooks must run unconditionally on every render, BEFORE any early return.
  // Hoist the pr-derived inputs into plain consts first so the useMemo dependency
  // arrays reference simple identifiers (the React Compiler rejects optional-chaining
  // member expressions in dep arrays). Null-tolerant helpers keep these safe while pr is null.
  const prTargetBranch = pr ? pr.targetBranch : undefined;
  const prRequestedReviewers = pr ? pr.requestedReviewers : undefined;
  const matchingRule = useMemo(() => getMatchingRule(prProtectionRules, prTargetBranch), [prProtectionRules, prTargetBranch]);
  const approvals = useMemo(() => approvalsProgress(matchingRule, reviews), [matchingRule, reviews]);
  const reviewerList = useMemo(() => buildReviewerList(prRequestedReviewers, reviews), [prRequestedReviewers, reviews]);

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

  // Task 5: Non-hook derived consts (pr is guaranteed non-null past the guard above).
  const headCommit = commits[0] || null;
  const buildMeta = buildStatusMeta(headCommit?.overallStatus);
  const headBuildId = headCommit?.statuses?.[0]?.buildId || null;
  const approvalsOk = !matchingRule || (approvals && approvals.satisfied);
  const mergeAllowed = pr.mergeable === true && approvalsOk;
  const isOpen = pr.status === 'open';

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
      {/* Header Info */}
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
        <h2 style={{ fontSize: 22, fontWeight: 600, letterSpacing: '-0.015em', display: 'flex', alignItems: 'baseline', gap: 8, margin: '0 0 10px 0' }}>
          <span style={{ color: 'var(--gb-fg)' }}>{pr.title}</span>
          <span style={{ color: 'var(--gb-fg-4)', fontWeight: 400 }}>#{pr.number}</span>
        </h2>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
          <Chip
            variant={pr.status === 'merged' ? 'merged' : pr.status === 'closed' ? 'err' : 'ok'}
            icon={pr.status === 'merged' ? <GitMerge size={11} /> : pr.status === 'closed' ? <XCircle size={11} /> : <GitPullRequest size={11} />}
            style={{ padding: '3px 9px', height: 22, textTransform: 'capitalize' }}
          >
            {pr.status}
          </Chip>
          <span style={{ fontSize: 12.5, color: 'var(--gb-fg-3)' }}>
            <strong style={{ color: 'var(--gb-fg)' }}>{pr.authorUsername}</strong> wants to merge{' '}
            <span style={{ fontFamily: 'var(--gb-mono)', color: 'var(--gb-fg-2)' }}>{commits.length} commit{commits.length !== 1 ? 's' : ''}</span> into{' '}
            <span style={{ fontFamily: 'var(--gb-mono)', background: 'var(--gb-surface-2)', padding: '1px 6px', borderRadius: 3, color: 'var(--gb-fg)' }}>{pr.targetBranch}</span>
            <span style={{ color: 'var(--gb-fg-4)' }}> ← </span>
            <span style={{ fontFamily: 'var(--gb-mono)', background: 'var(--gb-accent-bg)', padding: '1px 6px', borderRadius: 3, color: 'var(--gb-accent)' }}>{pr.sourceBranch}</span>
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
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 280px', gap: 22, alignItems: 'start' }}>
          {/* Main timeline */}
          <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
            {/* PR Description card */}
            <Card style={{ padding: 0 }}>
              <div style={{ padding: '10px 14px', borderBottom: '1px solid var(--gb-line)', background: 'var(--gb-surface-2)', display: 'flex', alignItems: 'center', gap: 8, fontSize: 12.5 }}>
                <Avatar name={pr.authorUsername} size={20} />
                <strong style={{ color: 'var(--gb-fg)' }}>{pr.authorUsername}</strong>
                <span style={{ color: 'var(--gb-fg-3)' }}>opened this on {new Date(pr.createdAt).toLocaleDateString()}</span>
              </div>
              <div className="markdown-body" style={{ padding: '14px 18px', color: 'var(--gb-fg-2)', fontSize: 13.5, lineHeight: 1.55 }}>
                {pr.description ? (
                  <ReadmeBody content={pr.description} />
                ) : (
                  <p style={{ margin: 0, color: 'var(--gb-fg-3)' }}>No description provided.</p>
                )}
              </div>
            </Card>

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
                      <div
                        className="timeline-content-body markdown-body"
                        style={{
                          color: '#e2e8f0',
                          fontSize: '0.9rem',
                          lineHeight: '1.4'
                        }}
                        dangerouslySetInnerHTML={{ __html: renderReadme(comment.body) }}
                      />
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

            {/* Task 5: Approvals + Build callouts (ADDITIVE — above the existing merge box) */}
            {isOpen && (
              <ApprovalsCallout approvals={approvals} matchingRule={matchingRule} mergeAllowed={mergeAllowed} onMerge={handleMerge} merging={actionLoading} />
            )}
            {isOpen && headCommit && (
              <BuildCallout meta={buildMeta} headCommit={headCommit} buildId={headBuildId} owner={owner} repo={repo} onNavigate={onNavigate} />
            )}

            {/* Merge box at bottom */}
            {pr.status === 'open' && (
              <div className="pr-merge-box" style={{
                display: 'flex',
                flexDirection: 'column',
                gap: '1rem',
                padding: '1.25rem',
                background: 'var(--gb-surface)',
                border: '1px solid var(--gb-line)',
                borderRadius: 10,
                marginTop: '1rem'
              }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '1.25rem' }}>
                  <div className="merge-status-indicator" style={{
                    width: '40px',
                    height: '40px',
                    borderRadius: '50%',
                    background: pr.mergeable === true 
                      ? 'rgba(16, 185, 129, 0.1)' 
                      : pr.mergeable === false 
                        ? 'rgba(239, 68, 68, 0.1)' 
                        : 'rgba(148, 163, 184, 0.1)',
                    border: pr.mergeable === true 
                      ? '1px solid rgba(16, 185, 129, 0.2)' 
                      : pr.mergeable === false 
                        ? '1px solid rgba(239, 68, 68, 0.2)' 
                        : '1px solid rgba(148, 163, 184, 0.2)',
                    color: pr.mergeable === true 
                      ? '#10b981' 
                      : pr.mergeable === false 
                        ? '#ef4444' 
                        : '#94a3b8',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    flexShrink: 0
                  }}>
                    {pr.mergeable === true && <GitMerge size={20} />}
                    {pr.mergeable === false && <AlertTriangle size={20} />}
                    {(pr.mergeable === undefined || pr.mergeable === null) && <span className="loader" style={{ width: '18px', height: '18px', borderWidth: '2px' }}></span>}
                  </div>
                  <div className="merge-box-content" style={{ flex: 1 }}>
                    <h3 className="merge-box-title" style={{ color: 'var(--gb-fg)', margin: 0, fontSize: 14, fontWeight: 600 }}>
                      {pr.mergeable === true && "This branch has no conflicts"}
                      {pr.mergeable === false && "This branch has conflicts that must be resolved"}
                      {(pr.mergeable === undefined || pr.mergeable === null) && "Checking mergeability..."}
                    </h3>
                    <div className="merge-box-desc" style={{ color: 'var(--gb-fg-3)', fontSize: 12 }}>
                      {pr.mergeable === true && "Merging can be performed automatically."}
                      {pr.mergeable === false && "Conflicts must be resolved before this pull request can be merged."}
                      {(pr.mergeable === undefined || pr.mergeable === null) && "We're checking if this branch can be merged cleanly."}
                    </div>
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
                      disabled={actionLoading || pr.mergeable !== true || !approvalsOk}
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

                {pr.mergeable === false && (
                  <div style={{ borderTop: '1px solid rgba(255, 255, 255, 0.06)', paddingTop: '0.75rem' }}>
                    <span style={{ color: '#94a3b8', fontSize: '0.85rem', display: 'block', marginBottom: '0.5rem' }}>
                      You can attempt to update the source branch automatically to resolve conflicts:
                    </span>
                    <div style={{ display: 'flex', gap: '0.5rem' }}>
                      <button
                        className="btn btn-secondary"
                        style={{ padding: '0.35rem 0.8rem', fontSize: '0.8rem', display: 'flex', alignItems: 'center', gap: '0.4rem' }}
                        onClick={() => handleUpdateBranch('merge')}
                        disabled={actionLoading}
                      >
                        Update via Merge
                      </button>
                      <button
                        className="btn btn-secondary"
                        style={{ padding: '0.35rem 0.8rem', fontSize: '0.8rem', display: 'flex', alignItems: 'center', gap: '0.4rem' }}
                        onClick={() => handleUpdateBranch('rebase')}
                        disabled={actionLoading}
                      >
                        Update via Rebase
                      </button>
                    </div>
                  </div>
                )}
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
            {/* Task 5: ReviewerRail replaces the old IIFE reviewer list */}
            <ReviewerRail reviewers={reviewerList} />

            {/* Review action form (preserved — visible to signed-in non-author on open PRs) */}
            {user && pr.status === 'open' && !isPrAuthor && (
              <Card style={{ padding: '1.25rem' }}>
                <textarea
                  className="text-input"
                  placeholder="Leave a note with your review (optional)"
                  value={reviewBody}
                  onChange={(e) => setReviewBody(e.target.value)}
                  style={{ width: '100%', minHeight: '60px', marginBottom: '0.75rem', resize: 'vertical', fontFamily: 'inherit', fontSize: '0.85rem' }}
                />
                {reviewError && (
                  <div style={{ color: '#ef4444', fontSize: '0.8rem', marginBottom: '0.5rem' }}>{reviewError}</div>
                )}
                <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
                  <button
                    type="button"
                    className="btn btn-primary"
                    onClick={() => handleSubmitReview('approved')}
                    disabled={!!reviewSubmitting}
                    style={{ padding: '0.35rem 0.7rem', fontSize: '0.8rem', flex: 1 }}
                  >
                    {reviewSubmitting === 'approved' ? 'Approving…' : 'Approve'}
                  </button>
                  <button
                    type="button"
                    className="btn btn-secondary"
                    onClick={() => handleSubmitReview('changes_requested')}
                    disabled={!!reviewSubmitting}
                    style={{ padding: '0.35rem 0.7rem', fontSize: '0.8rem', flex: 1 }}
                  >
                    {reviewSubmitting === 'changes_requested' ? 'Submitting…' : 'Request changes'}
                  </button>
                  <button
                    type="button"
                    className="btn btn-secondary"
                    onClick={() => handleSubmitReview('commented')}
                    disabled={!!reviewSubmitting}
                    style={{ padding: '0.35rem 0.7rem', fontSize: '0.8rem', flex: 1 }}
                  >
                    {reviewSubmitting === 'commented' ? 'Submitting…' : 'Comment'}
                  </button>
                </div>
              </Card>
            )}

            {/* Task 5: RuleMatchedCard replaces the old "Review Status" glass-card */}
            <RuleMatchedCard rule={matchingRule} targetBranch={pr.targetBranch} />
          </div>
        </div>
      )}

      {prTab === 'commits' && (
        <CommitsList commits={commits} />
      )}

      {prTab === 'diff' && (
        <div className="diff-split-layout">
          {/* Left Column: File Tree Sidebar */}
          <div className="diff-sidebar">
            <div className="diff-sidebar-header">
              <span className="diff-sidebar-title">Files Changed</span>
              <div style={{ position: 'relative', display: 'flex', alignItems: 'center' }}>
                <span style={{ position: 'absolute', left: '0.75rem', color: 'var(--text-muted)', display: 'flex', alignItems: 'center' }}>
                  <Search size={14} />
                </span>
                <input
                  type="text"
                  placeholder="Filter files..."
                  className="text-input"
                  value={filterText}
                  onChange={(e) => setFilterText(e.target.value)}
                  style={{
                    paddingLeft: '2rem',
                    fontSize: '0.85rem',
                    width: '100%',
                    background: 'rgba(0,0,0,0.2)',
                    borderColor: 'var(--border-color)',
                    height: '32px',
                    borderRadius: '6px'
                  }}
                />
              </div>
            </div>
            <div className="diff-tree-container">
              {filteredTree.length === 0 ? (
                <div style={{ color: 'var(--text-muted)', fontSize: '0.85rem', fontStyle: 'italic', padding: '1rem 0' }}>
                  No matching files found.
                </div>
              ) : (
                filteredTree.map(node => renderTreeNode(node))
              )}
            </div>
          </div>

          {/* Right Column: Diff Cards List */}
          <div className="diff-main-content">
            {filteredFiles.length === 0 ? (
              <div className="glass-card" style={{ padding: '2.5rem', textAlign: 'center', color: 'var(--text-muted)', fontStyle: 'italic' }}>
                No modified files to display.
              </div>
            ) : (
              filteredFiles.map((file) => {
                const isViewed = !!viewedFiles[file.path];
                const isCollapsed = collapsedFiles[file.path] !== undefined ? collapsedFiles[file.path] : isViewed;
                const isHighlight = activeHighlight === file.path;

                return (
                  <DiffFileCard
                    key={file.path}
                    file={file}
                    isViewed={isViewed}
                    isCollapsed={isCollapsed}
                    isHighlight={isHighlight}
                    onToggleCollapse={toggleFileCollapse}
                    onToggleViewed={toggleViewed}
                    onNavigate={onNavigate}
                    owner={owner}
                    repo={repo}
                    branch={pr.sourceBranch || 'main'}
                  />
                );
              })
            )}
          </div>
        </div>
      )}
    </div>
  );
}


const DiffFileCard = React.memo(({ file, isViewed, isCollapsed, isHighlight, onToggleCollapse, onToggleViewed, onNavigate, owner, repo, branch }) => {
  return (
    <div
      id={`diff-file-${file.path}`}
      className={`diff-file-card ${isViewed ? 'viewed' : ''} ${isHighlight ? 'scroll-highlight' : ''}`}
    >
      <div className="diff-file-card-header">
        <div
          className="diff-file-card-header-left"
          onClick={() => onToggleCollapse(file.path)}
        >
          <span style={{ color: 'var(--text-muted)', display: 'flex', alignItems: 'center' }}>
            {isCollapsed ? <ChevronRight size={16} /> : <ChevronDown size={16} />}
          </span>
          <span className="diff-file-card-path">{file.path}</span>
          <span className="diff-file-card-stats">
            {file.additions > 0 && <span style={{ color: 'var(--success)' }}>+{file.additions}</span>}
            {file.deletions > 0 && <span style={{ color: 'var(--error)' }}>-{file.deletions}</span>}
          </span>
        </div>
        <div className="diff-file-card-header-right">
          <button
            className="diff-file-card-action-btn"
            onClick={(e) => {
              e.stopPropagation();
              onNavigate('blob', { owner, repo, branch, path: file.path });
            }}
          >
            View file
          </button>
          <label className={`diff-viewed-label ${isViewed ? 'checked' : ''}`} onClick={(e) => e.stopPropagation()}>
            <input
              type="checkbox"
              checked={isViewed}
              onChange={() => onToggleViewed(file.path)}
            />
            Viewed
          </label>
        </div>
      </div>

      {!isCollapsed && (
        <div className="diff-file-card-body">
          {file.lines.length === 0 ? (
            <div style={{ padding: '1.5rem', color: 'var(--text-muted)', fontStyle: 'italic', fontSize: '0.85rem', textAlign: 'center' }}>
              No line changes (e.g. empty or binary file).
            </div>
          ) : (
            file.lines.map((line, idx) => (
              <div key={idx} className={`diff-line-row diff-row-${line.type}`}>
                <div className="diff-line-gutter diff-line-gutter-old">
                  {line.oldLineNum !== null ? line.oldLineNum : ''}
                </div>
                <div className="diff-line-gutter diff-line-gutter-new">
                  {line.newLineNum !== null ? line.newLineNum : ''}
                </div>
                <div className="diff-line-code">
                  {line.content}
                </div>
              </div>
            ))
          )}
        </div>
      )}
    </div>
  );
});

// Client-side diff parsing and tree building helpers
const parseDiff = (rawDiff) => {
  if (!rawDiff) return [];
  const files = [];
  const lines = rawDiff.split('\n');
  let currentFile = null;

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (line.startsWith('diff --git ')) {
      const match = line.match(/^diff --git a\/(.+) b\/(.+)$/);
      let path = '';
      if (match) {
        path = match[2];
      } else {
        const parts = line.substring(11).split(' ');
        if (parts.length >= 2) {
          path = parts[parts.length - 1].replace(/^b\//, '');
        }
      }
      currentFile = {
        path: path,
        additions: 0,
        deletions: 0,
        type: 'modified',
        rawLines: []
      };
      files.push(currentFile);
    } else if (currentFile) {
      currentFile.rawLines.push(line);
      if (line.startsWith('--- ')) {
        if (line.includes('/dev/null')) {
          currentFile.type = 'added';
        }
      } else if (line.startsWith('+++ ')) {
        if (line.includes('/dev/null')) {
          currentFile.type = 'deleted';
        }
        const pathMatch = line.match(/^\+\+\+ b\/(.+)$/);
        if (pathMatch && pathMatch[1] !== '/dev/null') {
          currentFile.path = pathMatch[1];
        }
      } else if (line.startsWith('rename to ')) {
        currentFile.type = 'rename';
        currentFile.path = line.substring(10);
      } else if (line.startsWith('new file mode ')) {
        currentFile.type = 'added';
      } else if (line.startsWith('deleted file mode ')) {
        currentFile.type = 'deleted';
      } else if (line.startsWith('+') && !line.startsWith('+++')) {
        currentFile.additions++;
      } else if (line.startsWith('-') && !line.startsWith('---')) {
        currentFile.deletions++;
      }
    }
  }

  files.forEach(file => {
    file.lines = parseFileDiffLines(file.rawLines);
  });

  return files;
};

const parseFileDiffLines = (rawLines) => {
  const result = [];
  let oldLineNum = 0;
  let newLineNum = 0;

  for (let i = 0; i < rawLines.length; i++) {
    const line = rawLines[i];

    if (line.startsWith('index ') || line.startsWith('new file mode') || line.startsWith('deleted file mode') || line.startsWith('similarity index') || line.startsWith('rename from') || line.startsWith('rename to')) {
      continue;
    }

    if (line.startsWith('--- a/') || line.startsWith('--- /dev/null') || line.startsWith('+++ b/') || line.startsWith('+++ /dev/null')) {
      continue;
    }

    if (line.startsWith('@@')) {
      const match = line.match(/^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
      if (match) {
        oldLineNum = parseInt(match[1], 10);
        newLineNum = parseInt(match[2], 10);
      }
      result.push({
        type: 'hunk',
        content: line,
        oldLineNum: null,
        newLineNum: null
      });
    } else if (line.startsWith('+') && !line.startsWith('+++')) {
      result.push({
        type: 'add',
        content: line,
        oldLineNum: null,
        newLineNum: newLineNum
      });
      newLineNum++;
    } else if (line.startsWith('-') && !line.startsWith('---')) {
      result.push({
        type: 'del',
        content: line,
        oldLineNum: oldLineNum,
        newLineNum: null
      });
      oldLineNum++;
    } else {
      result.push({
        type: 'normal',
        content: line,
        oldLineNum: oldLineNum,
        newLineNum: newLineNum
      });
      oldLineNum++;
      newLineNum++;
    }
  }

  return result;
};

const buildFileTree = (files) => {
  // ⚡ Bolt optimization: Pre-initialize root metrics to avoid NaN errors during aggregation
  const root = { name: 'root', path: '', isDirectory: true, children: [], additions: 0, deletions: 0 };
  // ⚡ Bolt optimization: Use an O(1) hash map lookup instead of O(N) array search on current.children
  // This reduces tree construction from O(N * D) to O(N), significantly speeding up large PR rendering
  const nodeMap = new Map();
  nodeMap.set('', root);
  
  files.forEach((file, index) => {
    const parts = file.path.split('/');
    let currentPath = '';
    
    // ⚡ Bolt optimization: Incrementally aggregate additions and deletions for the root
    // to avoid an O(N * D) post-traversal step
    root.additions += (file.additions || 0);
    root.deletions += (file.deletions || 0);

    parts.forEach((part, partIndex) => {
      const parentPath = currentPath;
      currentPath = currentPath ? `${currentPath}/${part}` : part;
      const isLast = partIndex === parts.length - 1;
      
      let child = nodeMap.get(currentPath);
      if (!child) {
        child = {
          name: part,
          path: currentPath,
          isDirectory: !isLast,
          children: [],
          fileIndex: isLast ? index : undefined,
          additions: 0,
          deletions: 0
        };
        const parent = nodeMap.get(parentPath);
        parent.children.push(child);
        nodeMap.set(currentPath, child);
      }
      
      // ⚡ Bolt optimization: Incrementally aggregate additions and deletions during tree construction
      // to avoid an O(N * D) post-traversal step
      child.additions = (child.additions || 0) + (file.additions || 0);
      child.deletions = (child.deletions || 0) + (file.deletions || 0);
    });
  });

  const sortTree = (node) => {
    node.children.sort((a, b) => {
      if (a.isDirectory && !b.isDirectory) return -1;
      if (!a.isDirectory && b.isDirectory) return 1;
      return a.name.localeCompare(b.name);
    });
    node.children.forEach(sortTree);
  };
  
  sortTree(root);
  return root.children;
};

