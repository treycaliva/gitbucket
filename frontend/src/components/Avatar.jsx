import { initials } from '../utils/avatarColor';

const PALETTES = [
  ['#8fd7f7', '#b394ea'],
  ['#a7f3d0', '#67e8f9'],
  ['#fde68a', '#fca5a5'],
  ['#c4b5fd', '#fbcfe8'],
  ['#86efac', '#fde68a'],
  ['#fda4af', '#c4b5fd'],
  ['#7dd3fc', '#a78bfa'],
  ['#f9a8d4', '#fdba74'],
];

export default function Avatar({ name, size = 22, style, ...rest }) {
  const seed = name || '?';
  const idx = seed.split('').reduce((a, c) => a + c.charCodeAt(0), 0) % PALETTES.length;
  const [a, b] = PALETTES[idx];
  return (
    <span
      className="gb-avatar"
      style={{
        width: size,
        height: size,
        background: `linear-gradient(140deg, ${a}, ${b})`,
        fontSize: size <= 18 ? 8 : size <= 24 ? 10 : 11,
        ...style,
      }}
      {...rest}
    >
      {initials(name)}
    </span>
  );
}
