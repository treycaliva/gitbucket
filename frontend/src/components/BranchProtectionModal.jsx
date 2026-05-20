import { useMemo, useState } from 'react';

/**
 * BranchProtectionModal
 *
 * Props:
 *   - mode: 'create' | 'edit'
 *   - initialRule: existing rule object when editing; null/undefined when creating
 *   - owner: repository owner username (string) — pre-selected in allowlists for new rules
 *   - collaborators: array of { uid, username, ... } — combined with owner for the multiselects
 *   - onClose(): close without saving
 *   - onSubmit(rule): persist (POST or PUT). Receives a Rule-shaped object. May throw.
 */
export default function BranchProtectionModal({
  mode = 'create',
  initialRule = null,
  owner,
  collaborators = [],
  onClose,
  onSubmit,
}) {
  const candidateUsernames = useMemo(() => {
    const set = new Set();
    if (owner) set.add(owner);
    for (const c of collaborators) {
      if (c && c.username) set.add(c.username);
    }
    return Array.from(set).sort((a, b) => a.localeCompare(b));
  }, [owner, collaborators]);

  const defaultForCreate = () => ({
    pattern: '',
    pushAllowlist: owner ? [owner] : [],
    mergeAllowlist: owner ? [owner] : [],
    requirePullRequest: false,
    requireApprovals: 0,
    requireCodeownerApproval: false,
    blockForcePush: false,
    blockDeletion: false,
  });

  const seed = mode === 'edit' && initialRule
    ? {
        id: initialRule.id,
        pattern: initialRule.pattern || '',
        pushAllowlist: Array.isArray(initialRule.pushAllowlist) ? [...initialRule.pushAllowlist] : [],
        mergeAllowlist: Array.isArray(initialRule.mergeAllowlist) ? [...initialRule.mergeAllowlist] : [],
        requirePullRequest: !!initialRule.requirePullRequest,
        requireApprovals: Number.isFinite(initialRule.requireApprovals) ? initialRule.requireApprovals : 0,
        requireCodeownerApproval: !!initialRule.requireCodeownerApproval,
        blockForcePush: !!initialRule.blockForcePush,
        blockDeletion: !!initialRule.blockDeletion,
      }
    : defaultForCreate();

  const [form, setForm] = useState(seed);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  const toggleInList = (key, username) => {
    setForm((prev) => {
      const cur = prev[key] || [];
      const next = cur.includes(username)
        ? cur.filter((u) => u !== username)
        : [...cur, username];
      return { ...prev, [key]: next };
    });
  };

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    if (!form.pattern.trim()) {
      setError('Pattern is required.');
      return;
    }
    if (form.requireApprovals < 0 || !Number.isFinite(Number(form.requireApprovals))) {
      setError('Required approvals must be ≥ 0.');
      return;
    }
    const payload = {
      ...(form.id ? { id: form.id } : {}),
      pattern: form.pattern.trim(),
      pushAllowlist: form.pushAllowlist,
      mergeAllowlist: form.mergeAllowlist,
      requirePullRequest: !!form.requirePullRequest,
      requireApprovals: Number(form.requireApprovals) | 0,
      requireCodeownerApproval: !!form.requireCodeownerApproval,
      blockForcePush: !!form.blockForcePush,
      blockDeletion: !!form.blockDeletion,
    };
    setSubmitting(true);
    try {
      await onSubmit(payload);
    } catch (err) {
      setError((err && err.message) || 'Failed to save rule.');
      setSubmitting(false);
      return;
    }
    setSubmitting(false);
  };

  const renderMultiselect = (key, label) => (
    <div className="form-group" style={{ marginBottom: '1rem' }}>
      <label className="form-label" style={{ display: 'block', marginBottom: '0.5rem', color: '#e2e8f0', fontSize: '0.9rem' }}>
        {label}
      </label>
      {candidateUsernames.length === 0 ? (
        <div style={{ color: '#64748b', fontSize: '0.85rem' }}>
          No candidates available. Add collaborators first.
        </div>
      ) : (
        <div style={{
          display: 'flex',
          flexWrap: 'wrap',
          gap: '0.4rem',
          padding: '0.5rem',
          border: '1px solid var(--border-color)',
          borderRadius: '6px',
          background: 'rgba(15, 23, 42, 0.4)',
          maxHeight: '120px',
          overflowY: 'auto',
        }}>
          {candidateUsernames.map((u) => {
            const selected = (form[key] || []).includes(u);
            return (
              <button
                key={u}
                type="button"
                onClick={() => toggleInList(key, u)}
                style={{
                  background: selected ? 'rgba(56, 189, 248, 0.2)' : 'rgba(255,255,255,0.04)',
                  border: `1px solid ${selected ? '#38bdf8' : 'var(--border-color)'}`,
                  color: selected ? '#38bdf8' : '#e2e8f0',
                  padding: '0.25rem 0.6rem',
                  borderRadius: '999px',
                  fontSize: '0.8rem',
                  cursor: 'pointer',
                  fontWeight: selected ? 600 : 400,
                }}
              >
                {selected ? '✓ ' : ''}{u}
              </button>
            );
          })}
        </div>
      )}
      <span style={{ fontSize: '0.75rem', color: '#64748b', display: 'block', marginTop: '0.25rem' }}>
        {(form[key] || []).length === 0
          ? 'Empty allowlist = nobody is allowed.'
          : `${(form[key] || []).length} selected.`}
      </span>
    </div>
  );

  return (
    <div className="modal-overlay" onClick={() => !submitting && onClose && onClose()}>
      <div
        className="glass-card modal-content"
        onClick={(e) => e.stopPropagation()}
        style={{ animation: 'none', maxWidth: '600px' }}
      >
        <h2 style={{ fontSize: '1.5rem', marginBottom: '1rem' }}>
          {mode === 'edit' ? 'Edit branch protection rule' : 'Add branch protection rule'}
        </h2>

        {error && (
          <div style={{
            background: 'rgba(244, 63, 94, 0.1)',
            border: '1px solid rgba(244, 63, 94, 0.2)',
            color: '#fb7185',
            padding: '0.75rem 1rem',
            borderRadius: '8px',
            fontSize: '0.85rem',
            marginBottom: '1rem',
          }}>
            {error}
          </div>
        )}

        <form onSubmit={handleSubmit}>
          <div className="form-group" style={{ marginBottom: '1rem' }}>
            <label className="form-label" style={{ display: 'block', marginBottom: '0.5rem', color: '#e2e8f0', fontSize: '0.9rem' }}>
              Branch pattern
            </label>
            <input
              type="text"
              className="text-input"
              placeholder="main, release/*, feature/**"
              value={form.pattern}
              onChange={(e) => setForm({ ...form, pattern: e.target.value })}
              disabled={submitting}
              style={{ width: '100%' }}
              required
            />
            <span style={{ fontSize: '0.75rem', color: '#64748b', display: 'block', marginTop: '0.25rem' }}>
              Glob pattern matched against the short branch name (e.g., <code>main</code>, <code>release/*</code>).
            </span>
          </div>

          {renderMultiselect('pushAllowlist', 'Push allowlist')}
          {renderMultiselect('mergeAllowlist', 'Merge allowlist')}

          <div className="form-group" style={{ marginBottom: '0.75rem' }}>
            <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', color: '#e2e8f0', fontSize: '0.9rem', cursor: 'pointer' }}>
              <input
                type="checkbox"
                checked={form.requirePullRequest}
                onChange={(e) => setForm({ ...form, requirePullRequest: e.target.checked })}
                disabled={submitting}
              />
              Require pull request
            </label>
          </div>

          <div className="form-group" style={{ marginBottom: '0.75rem' }}>
            <label className="form-label" style={{ display: 'block', marginBottom: '0.25rem', color: '#e2e8f0', fontSize: '0.9rem' }}>
              Required approvals
            </label>
            <input
              type="number"
              min={0}
              className="text-input"
              value={form.requireApprovals}
              onChange={(e) => setForm({ ...form, requireApprovals: Math.max(0, parseInt(e.target.value, 10) || 0) })}
              disabled={submitting}
              style={{ width: '120px' }}
            />
          </div>

          <div className="form-group" style={{ marginBottom: '0.75rem' }}>
            <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', color: '#e2e8f0', fontSize: '0.9rem', cursor: 'pointer' }}>
              <input
                type="checkbox"
                checked={form.requireCodeownerApproval}
                onChange={(e) => setForm({ ...form, requireCodeownerApproval: e.target.checked })}
                disabled={submitting}
              />
              Require code-owner approval
            </label>
          </div>

          <div className="form-group" style={{ marginBottom: '0.75rem' }}>
            <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', color: '#e2e8f0', fontSize: '0.9rem', cursor: 'pointer' }}>
              <input
                type="checkbox"
                checked={form.blockForcePush}
                onChange={(e) => setForm({ ...form, blockForcePush: e.target.checked })}
                disabled={submitting}
              />
              Block force push
            </label>
          </div>

          <div className="form-group" style={{ marginBottom: '1.5rem' }}>
            <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', color: '#e2e8f0', fontSize: '0.9rem', cursor: 'pointer' }}>
              <input
                type="checkbox"
                checked={form.blockDeletion}
                onChange={(e) => setForm({ ...form, blockDeletion: e.target.checked })}
                disabled={submitting}
              />
              Block branch deletion
            </label>
          </div>

          <div style={{ display: 'flex', gap: '1rem', justifyContent: 'flex-end' }}>
            <button
              type="button"
              className="btn btn-secondary"
              onClick={onClose}
              disabled={submitting}
            >
              Cancel
            </button>
            <button
              type="submit"
              className="btn btn-primary"
              disabled={submitting}
            >
              {submitting ? 'Saving…' : (mode === 'edit' ? 'Save rule' : 'Create rule')}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
