// Minimal Markdown -> sanitized HTML for READMEs and PR descriptions.
export function renderReadme(markdown) {
  if (!markdown) return '';
  let html = markdown
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');

  // Headers
  html = html.replace(/^# (.*?)$/gm, '<h1 style="font-size: 1.75rem; border-bottom: 1px solid var(--border-color); padding-bottom: 0.35rem; margin-top: 1.5rem; margin-bottom: 1rem;">$1</h1>');
  html = html.replace(/^## (.*?)$/gm, '<h2 style="font-size: 1.4rem; border-bottom: 1px solid var(--border-color); padding-bottom: 0.3rem; margin-top: 1.5rem; margin-bottom: 0.85rem;">$1</h2>');
  html = html.replace(/^### (.*?)$/gm, '<h3 style="font-size: 1.15rem; margin-top: 1.25rem; margin-bottom: 0.75rem;">$1</h3>');

  // Code blocks
  html = html.replace(/```([\s\S]*?)```/gm, '<pre style="background: rgba(0,0,0,0.4); border: 1px solid var(--border-color); padding: 1rem; border-radius: 6px; font-family: var(--font-mono); margin-bottom: 1rem; overflow-x: auto;"><code>$1</code></pre>');

  // Inline code
  html = html.replace(/`([^`]+)`/g, '<code style="background: rgba(255,255,255,0.08); padding: 0.15rem 0.35rem; border-radius: 4px; font-family: var(--font-mono); font-size: 0.9em;">$1</code>');

  // Bold
  html = html.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');

  // Italic: *text* and _text_ (underscore form ignores snake_case)
  html = html.replace(/\*([^*\n]+)\*/g, '<em>$1</em>');
  html = html.replace(/(^|[^A-Za-z0-9])_([^_\n]+)_(?![A-Za-z0-9])/g, '$1<em>$2</em>');

  // Links: [text](url) — restrict to safe schemes; leave others as raw text
  html = html.replace(/\[([^\]\n]+)\]\(([^)\s]+)\)/g, (match, text, url) => {
    if (!/^(https?:\/\/|mailto:|\/|#)/i.test(url)) return match;
    const href = url.replace(/"/g, '&quot;');
    return `<a href="${href}" target="_blank" rel="noopener noreferrer" style="color: #38bdf8; text-decoration: none;">${text}</a>`;
  });

  // Unordered lists
  html = html.replace(/^\* (.*?)$/gm, '<li style="margin-left: 1.5rem; margin-bottom: 0.35rem; color: var(--text-secondary);">$1</li>');
  html = html.replace(/^- (.*?)$/gm, '<li style="margin-left: 1.5rem; margin-bottom: 0.35rem; color: var(--text-secondary);">$1</li>');

  // Paragraphs / Linebreaks
  html = html.split('\n').map(line => {
    if (line.trim().startsWith('<h') || line.trim().startsWith('<pre') || line.trim().startsWith('<li') || line.trim() === '') {
      return line;
    }
    return `<p style="margin-bottom: 0.85rem; color: var(--text-secondary); line-height: 1.6;">${line}</p>`;
  }).join('\n');

  return html;
}
