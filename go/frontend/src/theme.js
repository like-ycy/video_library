/** 主题：仅切换 CSS 色彩变量，字体与度量不变。
 *
 * 偏好取值：system | dark | light。
 * - system：跟随操作系统 prefers-color-scheme，并实时响应系统切换。
 * - dark / light：用户手动选择，写入 localStorage 与后端 config.json 后跨启动沿用。
 * data-theme 始终是解析后的实际值（dark|light）；data-theme-pref 是用户偏好。
 */

const STORAGE_KEY = 'cinevault.theme';
const PREFERENCES = new Set(['system', 'dark', 'light']);

export function getThemePreference() {
  const saved = localStorage.getItem(STORAGE_KEY);
  return PREFERENCES.has(saved) ? saved : 'system';
}

function systemTheme() {
  return matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
}

export function resolveTheme(pref = getThemePreference()) {
  if (pref === 'system') return systemTheme();
  return pref === 'light' ? 'light' : 'dark';
}

export function applyTheme(pref) {
  const preference = PREFERENCES.has(pref) ? pref : 'system';
  localStorage.setItem(STORAGE_KEY, preference);
  const resolved = resolveTheme(preference);
  document.documentElement.dataset.theme = resolved;
  document.documentElement.dataset.themePref = preference;
  return resolved;
}

export function initTheme() {
  applyTheme(getThemePreference());
}

export function toggleTheme() {
  const current = document.documentElement.dataset.theme === 'light' ? 'light' : 'dark';
  return applyTheme(current === 'dark' ? 'light' : 'dark');
}

/** 仅在偏好为 system 时监听系统主题变化。 */
export function startThemeWatcher() {
  const mq = matchMedia('(prefers-color-scheme: light)');
  const onChange = () => {
    if (getThemePreference() !== 'system') return;
    applyTheme('system');
    window.dispatchEvent(new CustomEvent('cinevault:theme-changed', { detail: 'system' }));
  };
  mq.addEventListener('change', onChange);
}
