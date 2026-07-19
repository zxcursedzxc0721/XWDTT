// Простой markdown рендерер для changelog
// Поддерживает: # заголовки, **жирный**, - списки, `код`, ссылки

export function renderMarkdown(text: string): string {
  if (!text) return '';

  let html = escapeHtml(text);

  // Заголовки: ## Header → <h3>
  html = html.replace(/^### (.+)$/gm, '<h3>$1</h3>');
  html = html.replace(/^## (.+)$/gm, '<h3>$1</h3>');
  html = html.replace(/^# (.+)$/gm, '<h3>$1</h3>');

  // Жирный: **text** → <strong>
  html = html.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');

  // Код: `text` → <code>
  html = html.replace(/`([^`]+)`/g, '<code>$1</code>');

  // Списки: - item → <li>
  html = html.replace(/^- (.+)$/gm, '<li>$1</li>');
  html = html.replace(/(<li>.*<\/li>\n?)+/g, '<ul>$&</ul>');

  // Ссылки: [text](url) → <a> (только http/https)
  html = html.replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_match, linkText, url) => {
    if (isSafeURL(url)) {
      return `<a href="${escapeHtml(url)}" target="_blank" rel="noopener noreferrer">${linkText}</a>`;
    }
    return linkText;
  });

  // Переносы строк
  html = html.replace(/\n/g, '<br>');

  return html;
}

function isSafeURL(url: string): boolean {
  const lower = url.toLowerCase().trim();
  return lower.startsWith('http://') || lower.startsWith('https://');
}

function escapeHtml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}
