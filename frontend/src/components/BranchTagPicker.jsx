import { useEffect, useMemo, useRef, useState } from 'react';
import { GitBranch, Check, X } from 'lucide-react';
import apiClient from '../apiClient';

export default function BranchTagPicker({
  owner,
  repo,
  branches = [],
  defaultBranch,
  currentBranch,
  onChange,
}) {
  const [open, setOpen] = useState(false);
  const [tab, setTab] = useState('branches');
  const [query, setQuery] = useState('');
  const [tags, setTags] = useState(null);
  const [tagsError, setTagsError] = useState('');
  const rootRef = useRef(null);
  const searchRef = useRef(null);

  useEffect(() => {
    if (!open) return;
    const onClick = (e) => {
      if (rootRef.current && !rootRef.current.contains(e.target)) setOpen(false);
    };
    const onKey = (e) => {
      if (e.key === 'Escape') setOpen(false);
    };
    document.addEventListener('mousedown', onClick);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onClick);
      document.removeEventListener('keydown', onKey);
    };
  }, [open]);

  useEffect(() => {
    if (open && searchRef.current) searchRef.current.focus();
  }, [open]);

  useEffect(() => {
    if (!open || tab !== 'tags' || tags !== null) return;
    apiClient.get(`/api/repos/${owner}/${repo}/tags`)
      .then((data) => setTags(Array.isArray(data) ? data : []))
      .catch((err) => {
        setTagsError(err.message || 'Failed to load tags');
        setTags([]);
      });
  }, [open, tab, tags, owner, repo]);

  const filteredBranches = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return branches;
    return branches.filter((b) => b.toLowerCase().includes(q));
  }, [branches, query]);

  const filteredTags = useMemo(() => {
    if (!tags) return [];
    const q = query.trim().toLowerCase();
    if (!q) return tags;
    return tags.filter((t) => t.name.toLowerCase().includes(q));
  }, [tags, query]);

  const select = (ref, type) => {
    setOpen(false);
    setQuery('');
    onChange(ref, type);
  };

  return (
    <div className="btpicker" ref={rootRef}>
      <button
        type="button"
        className="btpicker-trigger"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        <GitBranch size={16} style={{ color: '#38bdf8' }} />
        <span>{currentBranch || defaultBranch}</span>
        <span style={{ color: '#64748b' }}>▾</span>
      </button>

      {open && (
        <div className="btpicker-popover" role="dialog" aria-label="Switch branches or tags">
          <div className="btpicker-header">
            <span>Switch branches/tags</span>
            <button type="button" className="btpicker-close" onClick={() => setOpen(false)} aria-label="Close">
              <X size={16} />
            </button>
          </div>

          <input
            ref={searchRef}
            type="text"
            className="btpicker-search"
            placeholder={tab === 'branches' ? 'Find a branch...' : 'Find a tag...'}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />

          <div className="btpicker-tabs" role="tablist">
            <button
              type="button"
              role="tab"
              aria-selected={tab === 'branches'}
              className={`btpicker-tab ${tab === 'branches' ? 'active' : ''}`}
              onClick={() => setTab('branches')}
            >
              Branches
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={tab === 'tags'}
              className={`btpicker-tab ${tab === 'tags' ? 'active' : ''}`}
              onClick={() => setTab('tags')}
            >
              Tags
            </button>
          </div>

          <div className="btpicker-list">
            {tab === 'branches' && (
              filteredBranches.length === 0 ? (
                <div className="btpicker-empty">No branches match.</div>
              ) : (
                filteredBranches.map((b) => (
                  <div
                    key={b}
                    className={`btpicker-row ${b === currentBranch ? 'current' : ''}`}
                    onClick={() => select(b, 'branch')}
                  >
                    <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.5rem' }}>
                      {b === currentBranch ? <Check size={14} /> : <span style={{ width: 14 }} />}
                      {b}
                    </span>
                    {b === defaultBranch && <span className="btpicker-default">default</span>}
                  </div>
                ))
              )
            )}

            {tab === 'tags' && (
              tags === null ? (
                <div className="btpicker-empty">Loading tags...</div>
              ) : tagsError ? (
                <div className="btpicker-empty" style={{ color: '#ef4444' }}>{tagsError}</div>
              ) : filteredTags.length === 0 ? (
                <div className="btpicker-empty">{tags.length === 0 ? 'No tags.' : 'No tags match.'}</div>
              ) : (
                filteredTags.map((t) => (
                  <div
                    key={t.name}
                    className={`btpicker-row ${t.name === currentBranch ? 'current' : ''}`}
                    onClick={() => select(t.name, 'tag')}
                  >
                    <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.5rem' }}>
                      {t.name === currentBranch ? <Check size={14} /> : <span style={{ width: 14 }} />}
                      {t.name}
                    </span>
                  </div>
                ))
              )
            )}
          </div>
        </div>
      )}
    </div>
  );
}
