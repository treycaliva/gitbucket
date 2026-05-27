import { useEffect, useRef, useState } from 'react';
import { Check } from 'lucide-react';

/**
 * Custom drop button styled per the v2 handoff (.gb-select).
 * Renders a real <button> + popover menu — not a native <select> — so the
 * chevron and muted label prefix can be styled directly.
 *
 * Props:
 *   label    — muted prefix shown before the value, e.g. "Type:"
 *   value    — currently selected option value
 *   options  — [{ value, label }]
 *   onChange — (value) => void
 */
export default function Select({ label, value, options = [], onChange }) {
  const [open, setOpen] = useState(false);
  const wrapRef = useRef(null);

  const selected = options.find((o) => o.value === value);

  useEffect(() => {
    if (!open) return;
    const onDocClick = (e) => {
      if (wrapRef.current && !wrapRef.current.contains(e.target)) setOpen(false);
    };
    const onKey = (e) => {
      if (e.key === 'Escape') setOpen(false);
    };
    document.addEventListener('mousedown', onDocClick);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDocClick);
      document.removeEventListener('keydown', onKey);
    };
  }, [open]);

  return (
    <div className="gb-select-wrap" ref={wrapRef}>
      <button
        type="button"
        className="gb-select"
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        {label && <span className="label">{label}</span>}
        {selected ? selected.label : ''}
      </button>
      {open && (
        <div className="gb-select-menu" role="listbox">
          {options.map((o) => (
            <button
              key={o.value}
              type="button"
              role="option"
              aria-selected={o.value === value}
              className={o.value === value ? 'active' : ''}
              onClick={() => {
                onChange?.(o.value);
                setOpen(false);
              }}
            >
              <Check
                size={13}
                style={{ opacity: o.value === value ? 1 : 0, color: 'var(--gb-accent)' }}
              />
              {o.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
