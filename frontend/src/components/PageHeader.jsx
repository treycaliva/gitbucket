// Standard page title + subtitle block. `icon` is optional (a lucide node);
// `children` is the subtitle (may contain markup).
export default function PageHeader({ icon, title, children, style }) {
  return (
    <div style={{ marginBottom: 24, ...style }}>
      <h1 style={{ fontSize: 20, fontWeight: 600, letterSpacing: '-0.015em', color: 'var(--gb-fg)', margin: 0, display: 'flex', alignItems: 'center', gap: 9 }}>
        {icon}{title}
      </h1>
      {children != null && (
        <p style={{ fontSize: 13, color: 'var(--gb-fg-3)', marginTop: 6, maxWidth: 760, lineHeight: 1.5 }}>
          {children}
        </p>
      )}
    </div>
  );
}
