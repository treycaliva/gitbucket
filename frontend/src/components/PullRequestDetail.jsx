import { useState, useEffect, useMemo } from 'react';
import { apiClient } from '../apiClient';
import Card from './Card';
import Chip from './Chip';
import SectionHead from './SectionHead';
import Avatar from './Avatar';
import { renderReadme } from '../utils/markdown';
import {
  Folder,
  FileCode,
  Clock,
  ArrowLeft,
  FileText,
  Trash2,
  AlertTriangle,
  GitPullRequest,
  MessageSquare,
  GitMerge,
  ChevronRight,
  ChevronDown,
  Search,
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
  const sorted = [...rules].sort((a, b) => {
    const wa = wildcardCount(a.pattern), wb = wildcardCount(b.pattern);
    if (wa !== wb) return wa - wb;                       // fewer wildcards = more specific
    if (a.pattern.length !== b.pattern.length) return b.pattern.length - a.pattern.length; // longer = more specific
    return a.pattern < b.pattern ? -1 : 1;              // lexicographic tiebreak
  });
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

// Wrapper to isolate dangerouslySetInnerHTML (content is sanitized by renderReadme)
function ReadmeBody({ content }) {
  const html = renderReadme(content);
  return <div className="readme-body" dangerouslySetInnerHTML={{ __html: html }} />;
}

// Terminal-state merge banner (merged / closed). `box` styles the outer card,
// `indicator` the circle; `children` is an optional right-aligned action.
function MergeResultBox({ box, indicator, indicatorClass, icon, title, desc, children }) {
  return (
    <div className="pr-merge-box" style={{
      display: 'flex', alignItems: 'center', gap: '1.25rem', padding: '1.25rem',
      borderRadius: 'var(--border-radius)', marginTop: '1rem', ...box,
    }}>
      <div className={`merge-status-indicator ${indicatorClass}`} style={{
        width: '40px', height: '40px', borderRadius: '50%',
        display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0, ...indicator,
      }}>
        {icon}
      </div>
      <div className="merge-box-content" style={{ flex: 1 }}>
        <h3 className="merge-box-title" style={{ color: '#f8fafc', margin: 0, fontSize: '1rem', fontWeight: 600 }}>{title}</h3>
        <div className="merge-box-desc" style={{ color: '#94a3b8', fontSize: '0.85rem' }}>{desc}</div>
      </div>
      {children}
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

    const checkMatch = (node) => {
      if (!node.isDirectory) {
        return node.name.toLowerCase().includes(filterLower) || node.path.toLowerCase().includes(filterLower);
      }
      const matchedChildren = node.children.filter(checkMatch);
      return matchedChildren.length > 0;
    };

    const copyAndFilter = (nodeList) => {
      return nodeList
        .filter(checkMatch)
        .map(node => {
          if (node.isDirectory) {
            return {
              ...node,
              children: copyAndFilter(node.children)
            };
          }
          return node;
        });
    };

    return copyAndFilter(fileTree);
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

  const toggleFileCollapse = (path) => {
    setCollapsedFiles(prev => ({
      ...prev,
      [path]: !prev[path]
    }));
  };

  const toggleViewed = (path) => {
    setViewedFiles(prev => {
      const next = { ...prev, [path]: !prev[path] };
      if (next[path]) {
        setCollapsedFiles(fPrev => ({ ...fPrev, [path]: true }));
      } else {
        setCollapsedFiles(fPrev => ({ ...fPrev, [path]: false }));
      }
      return next;
    });
  };

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
  // Merge-state visuals for the open PR banner (collapses the 3-way mergeable branches).
  const ms = pr.mergeable === true
    ? { bg: 'rgba(16, 185, 129, 0.1)', border: '1px solid rgba(16, 185, 129, 0.2)', color: '#10b981', icon: <GitMerge size={20} />, title: 'This branch has no conflicts', desc: 'Merging can be performed automatically.' }
    : pr.mergeable === false
      ? { bg: 'rgba(239, 68, 68, 0.1)', border: '1px solid rgba(239, 68, 68, 0.2)', color: '#ef4444', icon: <AlertTriangle size={20} />, title: 'This branch has conflicts that must be resolved', desc: 'Conflicts must be resolved before this pull request can be merged.' }
      : { bg: 'rgba(148, 163, 184, 0.1)', border: '1px solid rgba(148, 163, 184, 0.2)', color: '#94a3b8', icon: <span className="loader" style={{ width: '18px', height: '18px', borderWidth: '2px' }}></span>, title: 'Checking mergeability...', desc: "We're checking if this branch can be merged cleanly." };

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
                    background: ms.bg,
                    border: ms.border,
                    color: ms.color,
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    flexShrink: 0
                  }}>
                    {ms.icon}
                  </div>
                  <div className="merge-box-content" style={{ flex: 1 }}>
                    <h3 className="merge-box-title" style={{ color: 'var(--gb-fg)', margin: 0, fontSize: 14, fontWeight: 600 }}>
                      {ms.title}
                    </h3>
                    <div className="merge-box-desc" style={{ color: 'var(--gb-fg-3)', fontSize: 12 }}>
                      {ms.desc}
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
              <MergeResultBox
                box={{ background: 'rgba(168, 85, 247, 0.05)', border: '1px solid rgba(168, 85, 247, 0.2)' }}
                indicator={{ background: 'rgba(168, 85, 247, 0.1)', border: '1px solid rgba(168, 85, 247, 0.2)', color: '#a855f7' }}
                indicatorClass="merged"
                icon={<GitMerge size={20} />}
                title="Pull request successfully merged"
                desc={<>Commits are now integrated into <code style={{ fontFamily: 'var(--font-mono)' }}>{pr.targetBranch}</code>.</>}
              >
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
              </MergeResultBox>
            )}

            {pr.status === 'closed' && (
              <MergeResultBox
                box={{ background: 'rgba(244, 63, 94, 0.05)', border: '1px solid rgba(244, 63, 94, 0.2)' }}
                indicator={{ background: 'rgba(244, 63, 94, 0.1)', border: '1px solid rgba(244, 63, 94, 0.2)', color: '#f43f5e' }}
                indicatorClass="error"
                icon={<AlertTriangle size={20} />}
                title="Pull request closed"
                desc="This pull request was closed without merging changes."
              />
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
                  <div
                    key={file.path}
                    id={`diff-file-${file.path}`}
                    className={`diff-file-card ${isViewed ? 'viewed' : ''} ${isHighlight ? 'scroll-highlight' : ''}`}
                  >
                    <div className="diff-file-card-header">
                      <div
                        className="diff-file-card-header-left"
                        onClick={() => toggleFileCollapse(file.path)}
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
                            onNavigate('blob', { owner, repo, branch: pr.sourceBranch || 'main', path: file.path });
                          }}
                        >
                          View file
                        </button>
                        <label className={`diff-viewed-label ${isViewed ? 'checked' : ''}`} onClick={(e) => e.stopPropagation()}>
                          <input
                            type="checkbox"
                            checked={isViewed}
                            onChange={() => toggleViewed(file.path)}
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
              })
            )}
          </div>
        </div>
      )}
    </div>
  );
}

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
  const root = { name: 'root', path: '', isDirectory: true, children: [] };
  
  files.forEach((file, index) => {
    const parts = file.path.split('/');
    let current = root;
    let currentPath = '';
    
    parts.forEach((part, partIndex) => {
      currentPath = currentPath ? `${currentPath}/${part}` : part;
      const isLast = partIndex === parts.length - 1;
      
      let child = current.children.find(c => c.name === part && c.isDirectory === !isLast);
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
        current.children.push(child);
      }
      
      if (isLast) {
        child.additions = file.additions;
        child.deletions = file.deletions;
      }
      
      current = child;
    });
  });

  const calculateTreeStats = (nodes) => {
    nodes.forEach(node => {
      if (node.isDirectory) {
        calculateTreeStats(node.children);
        node.additions = node.children.reduce((sum, child) => sum + child.additions, 0);
        node.deletions = node.children.reduce((sum, child) => sum + child.deletions, 0);
      }
    });
  };

  const sortTree = (node) => {
    node.children.sort((a, b) => {
      if (a.isDirectory && !b.isDirectory) return -1;
      if (!a.isDirectory && b.isDirectory) return 1;
      return a.name.localeCompare(b.name);
    });
    node.children.forEach(sortTree);
  };
  
  calculateTreeStats(root.children);
  sortTree(root);
  return root.children;
};

export default PullRequestDetail;
