export default function Card({ children, className = '', style, ...rest }) {
  return (
    <div className={`gb-card ${className}`.trim()} style={style} {...rest}>
      {children}
    </div>
  );
}
