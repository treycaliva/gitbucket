export default function Chip({ variant = 'default', icon = null, dot = false, children, className = '', ...rest }) {
  const cls = ['gb-chip', variant !== 'default' ? variant : '', dot ? 'dot' : '', className]
    .filter(Boolean)
    .join(' ');
  return (
    <span className={cls} {...rest}>
      {icon}
      {children}
    </span>
  );
}
