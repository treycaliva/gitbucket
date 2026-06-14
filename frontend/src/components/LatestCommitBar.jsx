import { useEffect, useState } from 'react';
import { History } from 'lucide-react';
import { apiClient } from '../apiClient';
import { formatRelative } from '../utils/relativeTime';
import { initials, colorFor } from '../utils/avatarColor';

export default function LatestCommitBar({ owner, repo, branch, onViewCommits }) {
  const [head, setHead] = useState(null);

  useEffect(() => {
    if (!branch) return;
    let cancelled = false;
    // Reset so a branch switch clears the prior commit instead of flashing it.
    setHead(null);
    apiClient
      .get(`/api/repos/${owner}/${repo}/refs/${encodeURIComponent(branch)}/head`)
      .then((data) => {
        if (!cancelled) setHead(data);
      })
      .catch(() => {}); // ponytail: on failure head stays null and we render nothing
    return () => {
      cancelled = true;
    };
  }, [owner, repo, branch]);

  if (!head || !head.sha) return null;

  return (
    <div className="latest-commit-bar">
      <div
        className="latest-commit-avatar"
        style={{ background: colorFor(head.authorEmail || head.authorName) }}
        title={head.authorName}
      >
        {initials(head.authorName)}
      </div>
      <span className="latest-commit-author">{head.authorName}</span>
      <span className="latest-commit-msg" title={head.message}>{head.message}</span>
      <span className="latest-commit-sha">{head.sha.substring(0, 7)}</span>
      <span className="latest-commit-time">· {formatRelative(head.date)}</span>
      <span
        className="latest-commit-count"
        onClick={onViewCommits}
        role="button"
        tabIndex={0}
        onKeyDown={(e) => { if (e.key === 'Enter') onViewCommits(); }}
      >
        <History size={14} style={{ verticalAlign: '-2px', marginRight: 4 }} />
        {head.totalCommits} Commits
      </span>
    </div>
  );
}
