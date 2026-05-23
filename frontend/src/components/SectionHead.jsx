export default function SectionHead({ kicker, title, right }) {
  return (
    <div className="gb-section-title">
      {kicker && <span className="kicker">{kicker}</span>}
      <span>{title}</span>
      {right && <span className="right">{right}</span>}
    </div>
  );
}
