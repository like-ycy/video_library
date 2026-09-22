/** 主题：偏好 light | dark | system；data-theme 始终是解析后的 dark|light。
 *
 * 与 video_cut 对齐：
 * - localStorage 是偏好权威（默认 system = 跟随系统）
 * - system 优先原生 GetSystemAppearance（macOS WKWebView 的 prefers-color-scheme 不可靠），
 *   拿不到再回退 matchMedia；system 模式下轮询纠正
 * - config.json 的 theme 在启动时仅作 localStorage 缺失时的迁移来源
 */

import { call } from './api.js';

const STORAGE_KEY = 'cinevault.theme';
const PREFERENCES = new Set(['system', 'dark', 'light']);

export function getThemePreference() {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (PREFERENCES.has(raw)) return raw;
  } catch {
    /* 隐私模式等场景忽略 */
  }
  return 'system';
}

export function saveThemePreference(pref) {
  try {
    localStorage.setItem(STORAGE_KEY, PREFERENCES.has(pref) ? pref : 'system');
  } catch {
    /* ignore */
  }
}

function darkQuery() {
  return typeof matchMedia === 'function'
    ? matchMedia('(prefers-color-scheme: dark)')
    : null;
}

/** 显式 light/dark 直接返回；system 同步阶段只能用 media query 猜。 */
export function resolveThemeSync(pref = getThemePreference()) {
  if (pref === 'light' || pref === 'dark') return pref;
  return darkQuery()?.matches ? 'dark' : 'light';
}

/** 解析系统外观：优先原生，失败回退 matchMedia。 */
export async function resolveSystemTheme() {
  try {
    const native = await call('GetSystemAppearance');
    if (native === 'dark' || native === 'light') return native;
  } catch {
    /* 浏览器预览 / 后端不可用 → matchMedia */
  }
  return resolveThemeSync('system');
}

export function applyTheme(resolved) {
  const value = resolved === 'light' ? 'light' : 'dark';
  document.documentElement.dataset.theme = value;
  document.documentElement.style.colorScheme = value;
  return value;
}

/** 立刻套用偏好（system 可能先猜错，随后由原生结果纠正）。 */
export function applyPreference(pref) {
  const preference = PREFERENCES.has(pref) ? pref : 'system';
  document.documentElement.dataset.themePref = preference;
  applyTheme(resolveThemeSync(preference));
  return preference;
}

export function setThemePreference(pref) {
  const preference = PREFERENCES.has(pref) ? pref : 'system';
  saveThemePreference(preference);
  applyPreference(preference);
  window.dispatchEvent(new CustomEvent('cinevault:theme-changed', { detail: preference }));
  return preference;
}

export function initTheme() {
  return applyPreference(getThemePreference());
}

/** 启动时：localStorage 无值才用 config 迁移，避免旧 config 覆盖用户本地选择。 */
export function seedThemeFromConfig(cfgTheme) {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (PREFERENCES.has(raw)) return getThemePreference();
  } catch {
    /* fall through */
  }
  if (PREFERENCES.has(cfgTheme)) {
    saveThemePreference(cfgTheme);
  }
  return applyPreference(getThemePreference());
}

/** 顶栏三态：light → dark → system → light。 */
export function cycleTheme() {
  const pref = getThemePreference();
  if (pref === 'light') return setThemePreference('dark');
  if (pref === 'dark') return setThemePreference('system');
  return setThemePreference('light');
}

export function themeLabel(pref = getThemePreference()) {
  const resolved = document.documentElement.dataset.theme === 'light' ? '浅色' : '深色';
  if (pref === 'system') return `跟随系统（当前${resolved}）`;
  return pref === 'light' ? '亮色模式' : '暗色模式';
}

let stopWatcher = null;

/** 跟随系统：轮询原生外观 + 监听 media query，避免 WKWebView 事件不可靠。 */
export function startThemeWatcher() {
  if (typeof stopWatcher === 'function') stopWatcher();

  let cancelled = false;
  let timer = 0;
  let mq = null;
  let syncing = false;

  const onMq = () => { void sync(); };

  const clearWatch = () => {
    if (timer) {
      window.clearInterval(timer);
      timer = 0;
    }
    if (mq) {
      mq.removeEventListener?.('change', onMq);
      mq = null;
    }
  };

  const sync = async () => {
    // 关键：禁止重入，且仅在解析结果变化时派发，避免 sync→事件→sync 死循环卡死 UI
    if (cancelled || syncing || getThemePreference() !== 'system') return;
    syncing = true;
    try {
      const next = await resolveSystemTheme();
      if (cancelled || getThemePreference() !== 'system') return;
      const prev = document.documentElement.dataset.theme;
      applyTheme(next);
      if (document.documentElement.dataset.theme !== prev) {
        window.dispatchEvent(new CustomEvent('cinevault:theme-changed', { detail: 'system' }));
      }
    } finally {
      syncing = false;
    }
  };

  const ensureWatch = () => {
    if (cancelled || getThemePreference() !== 'system') {
      clearWatch();
      return;
    }
    if (!timer) timer = window.setInterval(() => { void sync(); }, 1500);
    if (!mq) {
      mq = darkQuery();
      mq?.addEventListener?.('change', onMq);
    }
  };

  const onPrefChanged = () => {
    ensureWatch();
    if (getThemePreference() === 'system') void sync();
  };

  ensureWatch();
  if (getThemePreference() === 'system') void sync();

  window.addEventListener('cinevault:theme-changed', onPrefChanged);

  stopWatcher = () => {
    cancelled = true;
    clearWatch();
    window.removeEventListener('cinevault:theme-changed', onPrefChanged);
    stopWatcher = null;
  };
  return stopWatcher;
}
