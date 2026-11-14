const COPY_FEEDBACK_DURATION_MS = 1600;
const VISIBLE_EMAIL_CHARACTERS = 2;
const EMAIL_MASK = '•••';
const CODE_TOKEN_PATTERN = /'[^'\n]*'|--\s[^\n]*|(?<=^|\s)-{1,2}[A-Za-z][A-Za-z0-9-]*|[A-Za-z_][A-Za-z0-9_]*/gm;
const COMMAND_NAMES = new Set(['sqx']);
const SQL_KEYWORDS = new Set([
  'SELECT', 'FROM', 'WHERE', 'GROUP', 'BY', 'ORDER', 'HAVING', 'LIMIT', 'JOIN', 'LEFT', 'INNER', 'ON', 'AS',
  'AND', 'OR', 'NOT', 'IN', 'IS', 'NULL', 'LIKE', 'DISTINCT', 'CASE', 'WHEN', 'THEN', 'ELSE', 'END', 'INSERT',
  'INTO', 'VALUES', 'UPDATE', 'SET', 'DELETE', 'CREATE', 'TABLE', 'INDEX', 'UNIQUE', 'IF', 'EXISTS', 'PRIMARY',
  'KEY', 'INTEGER', 'TEXT', 'REAL', 'RETURNING', 'CONFLICT', 'DO', 'PRAGMA', 'DESC', 'ASC', 'REGEXP', 'WITH',
  'UNION', 'ALL', 'TRUE',
]);

bindMenuButton();
highlightCodeBlocks();
addCopyButtons();
bindAuthorEmailButtons();
highlightCurrentTocEntry();

function bindMenuButton() {
  const menuButton = document.querySelector('.menu-button');
  if (!menuButton) return;
  menuButton.addEventListener('click', () => {
    const isOpen = document.body.classList.toggle('is-menu-open');
    menuButton.setAttribute('aria-expanded', String(isOpen));
  });
  document.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape' || !document.body.classList.contains('is-menu-open')) return;
    document.body.classList.remove('is-menu-open');
    menuButton.setAttribute('aria-expanded', 'false');
    menuButton.focus();
  });
}

function highlightCodeBlocks() {
  for (const codeElement of document.querySelectorAll('pre:not(.output) > code')) {
    codeElement.innerHTML = highlightSource(codeElement.textContent);
  }
}

function highlightSource(source) {
  let highlighted = '';
  let position = 0;
  for (const match of source.matchAll(CODE_TOKEN_PATTERN)) {
    highlighted += escapeHtml(source.slice(position, match.index));
    highlighted += renderToken(match[0], source[match.index + match[0].length]);
    position = match.index + match[0].length;
  }
  return highlighted + escapeHtml(source.slice(position));
}

function renderToken(token, nextCharacter) {
  const tokenType = classifyToken(token, nextCharacter);
  if (!tokenType) return escapeHtml(token);
  return `<span class="token-${tokenType}">${escapeHtml(token)}</span>`;
}

function classifyToken(token, nextCharacter) {
  if (token.startsWith("'")) return 'string';
  if (/^--\s/.test(token)) return 'comment';
  if (token.startsWith('-')) return 'flag';
  if (COMMAND_NAMES.has(token) && (nextCharacter === undefined || /\s/.test(nextCharacter))) return 'command';
  if (nextCharacter === '(') return 'function';
  if (SQL_KEYWORDS.has(token)) return 'keyword';
  return null;
}

function escapeHtml(text) {
  return text.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;');
}

function addCopyButtons() {
  for (const codeBlock of document.querySelectorAll('pre:not(.output)')) {
    const copyButton = document.createElement('button');
    copyButton.type = 'button';
    copyButton.className = 'copy-button';
    copyButton.textContent = 'Copy';
    copyButton.setAttribute('aria-label', 'Copy command to clipboard');
    copyButton.addEventListener('click', () => copyCodeBlock(codeBlock, copyButton));
    const codeFrame = document.createElement('div');
    codeFrame.className = 'code-frame';
    codeBlock.replaceWith(codeFrame);
    codeFrame.append(codeBlock, copyButton);
  }
}

async function copyCodeBlock(codeBlock, copyButton) {
  const code = codeBlock.querySelector('code')?.innerText ?? '';
  try {
    await navigator.clipboard.writeText(code);
    copyButton.textContent = 'Copied';
  } catch {
    copyButton.textContent = 'Press Ctrl+C';
  }
  setTimeout(() => { copyButton.textContent = 'Copy'; }, COPY_FEEDBACK_DURATION_MS);
}

function bindAuthorEmailButtons() {
  for (const emailButton of document.querySelectorAll('.author-email')) {
    const { emailUser, emailDomain } = emailButton.dataset;
    const emailText = emailButton.querySelector('.author-email-text');
    if (emailText) emailText.textContent = maskEmail(emailUser, emailDomain);
    emailButton.addEventListener('click', () => {
      window.location.href = `mailto:${emailUser}@${emailDomain}`;
    });
  }
}

function maskEmail(user, domain) {
  const topLevelStart = domain.lastIndexOf('.');
  const domainName = topLevelStart > 0 ? domain.slice(0, topLevelStart) : domain;
  const topLevel = topLevelStart > 0 ? domain.slice(topLevelStart) : '';
  return `${user.slice(0, VISIBLE_EMAIL_CHARACTERS)}${EMAIL_MASK}@${domainName.slice(0, VISIBLE_EMAIL_CHARACTERS)}${EMAIL_MASK}${topLevel}`;
}

function highlightCurrentTocEntry() {
  const tocLinks = [...document.querySelectorAll('.toc a')];
  if (tocLinks.length === 0 || !('IntersectionObserver' in window)) return;
  const linkForHeading = new Map(tocLinks.map((link) => [decodeURIComponent(link.hash.slice(1)), link]));
  const observer = new IntersectionObserver((entries) => {
    const visibleEntry = entries.find((entry) => entry.isIntersecting);
    if (!visibleEntry) return;
    for (const link of tocLinks) link.classList.remove('is-active');
    linkForHeading.get(visibleEntry.target.id)?.classList.add('is-active');
  }, { rootMargin: '0px 0px -70% 0px' });
  for (const headingId of linkForHeading.keys()) {
    const heading = document.getElementById(headingId);
    if (heading) observer.observe(heading);
  }
}
