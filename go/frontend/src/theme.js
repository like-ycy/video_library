/** 主题：仅切换 CSS 色彩变量，字体与度量不变。 */

const STORAGE_KEY = 'cinevault.theme';

export function getTheme() {
  const saved = localStorage.getItem(STORAGE_KEY);
  if (saved === 'light' || saved === 'dark') return saved;
  return 'dark';
}

export function applyTheme(theme) {
  const value = theme === 'light' ? 'light' : 'dark';
  document.documentElement.dataset.theme = value;
  localStorage.setItem(STORAGE_KEY, value);
  return value;
}

export function initTheme() {
  applyTheme(getTheme());
}

export function toggleTheme() {
  return applyTheme(getTheme() === 'dark' ? 'light' : 'dark');
}
