import Card from './Card';
import Chip from './Chip';
import SectionHead from './SectionHead';
import Avatar from './Avatar';
import { formatRelative } from '../utils/relativeTime';
import {
  GitBranch,
  GitPullRequest,
  Users,
  GitCommit,
  Tag,
  ShieldCheck,
} from 'lucide-react';

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
  // 50 = COMMITS_PAGE_SIZE in Repository.jsx; at the cap we show "50+".
  const commitLabel = commitCount >= 50 ? `${commitCount}+` : `${commitCount}`;

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

export default InsightsTab;
