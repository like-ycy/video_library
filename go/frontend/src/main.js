import { call } from './api.js';
import { h, reportError, setBusy, toast } from './ui.js';
import { initTheme, cycleTheme, startThemeWatcher, themeLabel, seedThemeFromConfig } from './theme.js';
import { DEFAULT_ROUTE, NAV_GROUPS } from './nav.js';
import { createPlaceholderView, icon } from './components/shell.js';
import { createWatchView } from './views/WatchView.js';
import { createScrapeView } from './views/ScrapeView.js';
import { createHistoryView } from './views/HistoryView.js';
import { createIssuesView } from './views/IssuesView.js';
import { createTaskMonitorView } from './views/TaskMonitorView.js';
import { createSettingsView } from './views/SettingsView.js';
import { createOnboarding, needsOnboarding, resetOnboarding } from './views/Onboarding.js';

const state = {
  libraryId: '',
  libraries: [],
  route: DEFAULT_ROUTE,
  keyword: '',
  showOnboarding: false,
};

// ── 窗口控制 ───────────────────────────────────────────────────────────

// Wails runtime.Environment() 返回 GOOS；开发态浏览器打开时回退 UA。
async function detectPlatform() {
  try {
    const env = await globalThis.runtime?.Environment?.();
    if (env?.platform) return env.platform;
  } catch {
    /* browser preview / runtime not ready */
  }
  const ua = navigator.userAgent.toLowerCase();
  if (ua.includes('mac')) return 'darwin';
  if (ua.includes('win')) return 'windows';
  return 'linux';
}

// macOS 用系统红绿灯（Go 侧 Frameless=false），隐藏自绘按钮；
// Windows/Linux 用无边框 + 自绘按钮，API 为 Wails 英式拼写。
async function initWindowChrome() {
  const platform = await detectPlatform();
  document.documentElement.dataset.platform = platform;

  if (platform === 'darwin') {
    document.getElementById('win-ctl-group')?.setAttribute('hidden', '');
    return;
  }

  const rt = globalThis.runtime;
  document.getElementById('win-min')?.addEventListener('click', () => {
    rt?.WindowMinimise?.();
  });
  document.getElementById('win-max')?.addEventListener('click', async () => {
    if (!rt) return;
    if (typeof rt.WindowIsMaximised === 'function') {
      const maximised = await rt.WindowIsMaximised();
      if (maximised) rt.WindowUnmaximise?.();
      else rt.WindowMaximise?.();
    } else {
      rt.WindowToggleMaximise?.();
    }
  });
  document.getElementById('win-close')?.addEventListener('click', () => {
    // App.Close 是资源清理，不是关窗；关窗用 runtime.Quit。
    rt?.Quit?.();
  });
}

// ── 主题 ───────────────────────────────────────────────────────────────

function syncThemeIcon() {
  const el = document.getElementById('theme-toggle-icon');
  const btn = document.getElementById('theme-toggle');
  if (!el) return;
  const pref = document.documentElement.dataset.themePref || 'system';
  // light / dark / system 三态图标
  el.textContent = pref === 'light' ? 'light_mode' : pref === 'dark' ? 'dark_mode' : 'desktop_windows';
  if (btn) {
    btn.title = `${themeLabel(pref)}（点击切换）`;
    btn.setAttribute('aria-label', themeLabel(pref));
  }
}

function wireTheme() {
  document.getElementById('theme-toggle')?.addEventListener('click', () => {
    // cycleTheme 内部已派发 cinevault:theme-changed
    cycleTheme();
    syncThemeIcon();
  });
  window.addEventListener('cinevault:theme-changed', syncThemeIcon);
  startThemeWatcher();
  syncThemeIcon();
}

// ── 侧栏 ───────────────────────────────────────────────────────────────

function buildSidebar() {
  const nav = document.getElementById('sidebar-nav');
  if (!nav) return;
  nav.replaceChildren();

  for (const group of NAV_GROUPS) {
    const groupEl = h('div', { class: 'nav-group' }, [
      h('div', { class: 'nav-group-label', text: group.label }),
    ]);

    for (const item of group.items) {
      const btn = h('button', {
        type: 'button',
        class: 'nav-item',
        dataset: { route: item.id },
        onclick: () => navigate(item.id).catch((error) => reportError('切换页面失败', error)),
      }, [
        h('span', { class: 'nav-left' }, [
          icon(item.icon),
          h('span', { class: 'nav-label', text: item.label }),
        ]),
      ]);

      // 任务监控运行指示
      if (item.id === 'tasks' && state.taskMonitor?.running) {
        btn.append(h('span', { class: 'nav-dot', title: '任务进行中' }));
      }
      if (item.id === 'continue' && state.continueCount > 0) {
        btn.append(h('span', { class: 'nav-badge', text: String(state.continueCount) }));
      }

      groupEl.append(btn);
    }
    nav.append(groupEl);
  }

  document.querySelectorAll('.nav-item').forEach((el) => {
    el.classList.toggle('active', el.dataset.route === state.route);
  });
}

// ── 路由 ───────────────────────────────────────────────────────────────

function createViews() {
  const views = {
    actresses: createWatchView(state, { mode: 'actresses' }),
    movies: createWatchView(state, { mode: 'movies' }),
    continue: createHistoryView(state, 'continue'),
    recent: createHistoryView(state, 'recent'),
    scan: createScrapeView(state),
    tasks: createTaskMonitorView(state),
    issues: createIssuesView(state),
    'settings-library': createSettingsView(state, 'library'),
    'settings-scraper': createSettingsView(state, 'scraper'),
    'settings-player': createSettingsView(state, 'player'),
    'settings-env': createSettingsView(state, 'env'),
    'settings-appearance': createSettingsView(state, 'appearance'),
    'settings-about': createSettingsView(state, 'about'),
    onboarding: createOnboarding(state, {
      onDone: () => {
        state.showOnboarding = false;
        navigate(DEFAULT_ROUTE).catch((e) => reportError('导航失败', e));
      },
    }),
  };
  return views;
}

const views = createViews();

function mountView(route) {
  const view = views[route] || views[DEFAULT_ROUTE];
  const host = document.getElementById('content-host');
  if (!host || !view) return;
  host.replaceChildren(view.el);
}

async function navigate(route) {
  if (state.showOnboarding && route !== 'onboarding') {
    // 引导期间仍允许点侧栏退出引导
    state.showOnboarding = false;
  }
  if (!views[route]) route = DEFAULT_ROUTE;
  state.route = route;
  buildSidebar();
  mountView(route);
  await views[route]?.onActivate?.();
  buildSidebar();
}

// ── 库 ─────────────────────────────────────────────────────────────────

async function loadLibraries() {
  try {
    state.libraries = (await call('ListLibraries')) ?? [];
  } catch (error) {
    reportError('读取视频库列表失败', error);
    state.libraries = [];
  }
  renderPicker();
}

function renderPicker() {
  const picker = document.getElementById('library-picker');
  if (!picker) return;
  picker.replaceChildren();

  if (!state.libraries.length) {
    picker.append(h('option', { text: '尚未添加视频库' }));
    picker.disabled = true;
    return;
  }

  picker.disabled = false;
  for (const lib of state.libraries) {
    picker.append(h('option', {
      value: lib.id,
      text: lib.available ? shortLib(lib.root) : `${shortLib(lib.root)}（不可访问）`,
    }));
  }
  picker.value = state.libraryId;
}

function shortLib(root) {
  if (!root) return '';
  return root.length > 36 ? `…${root.slice(-34)}` : root;
}

async function selectLibrary(id) {
  if (!id) return;
  state.libraryId = id;
  const picker = document.getElementById('library-picker');
  if (picker) picker.value = id;
  await views[state.route]?.onLibraryChange?.();
  await refreshContinueCount();
  buildSidebar();
}

async function refreshContinueCount() {
  try {
    if (!state.libraryId) {
      state.continueCount = 0;
      return;
    }
    const items = (await call('ListContinueWatching', state.libraryId)) ?? [];
    state.continueCount = items.length;
  } catch {
    state.continueCount = 0;
  }
}

async function addLibrary() {
  setBusy(true);
  try {
    const rootPath = await call('PickLibraryRoot');
    if (!rootPath) return;
    const lib = await call('AddLibrary', rootPath);
    await loadLibraries();
    await selectLibrary(lib.id);
    toast(`已添加视频库：${lib.root}`, 'ok');
  } catch (error) {
    reportError('添加视频库失败', error);
  } finally {
    setBusy(false);
  }
}

async function quickScan() {
  if (!state.libraryId) {
    toast('请先添加并选择视频库', 'warn');
    return;
  }
  setBusy(true);
  try {
    const result = await call('ScanLibrary', state.libraryId);
    const n = result.candidates?.length ?? 0;
    toast(`扫描完成：发现 ${n} 个文件`, 'ok');
    await navigate('scan');
  } catch (error) {
    reportError('扫描失败', error);
  } finally {
    setBusy(false);
  }
}

// ── 搜索 ───────────────────────────────────────────────────────────────

function wireGlobalSearch() {
  const input = document.getElementById('global-search');
  if (!input) return;

  const commit = () => {
    state.keyword = input.value.trim();
    if (state.route === 'actresses' || state.route === 'movies') {
      views[state.route]?.onActivate?.();
    } else {
      navigate('movies').catch(() => {});
    }
  };

  input.addEventListener('change', commit);
  input.addEventListener('keydown', (event) => {
    if (event.key === 'Enter') commit();
    if (event.key === 'Escape') {
      input.value = '';
      state.keyword = '';
      commit();
    }
  });

  document.addEventListener('keydown', (event) => {
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
      event.preventDefault();
      input.focus();
      input.select();
    }
  });
}

// ── 启动 ───────────────────────────────────────────────────────────────

async function boot() {
  // 主题：localStorage 优先；config 仅在本地无偏好时迁移（见 seedThemeFromConfig）
  initTheme();
  await initWindowChrome();
  wireTheme();
  buildSidebar();
  wireGlobalSearch();

  document.getElementById('add-library')?.addEventListener('click', () => {
    addLibrary().catch((error) => reportError('添加视频库失败', error));
  });
  document.getElementById('library-picker')?.addEventListener('change', (event) => {
    selectLibrary(event.target.value).catch((error) => reportError('切换视频库失败', error));
  });
  document.getElementById('quick-scan')?.addEventListener('click', () => {
    quickScan().catch((error) => reportError('扫描失败', error));
  });

  window.addEventListener('cinevault:navigate', (event) => {
    navigate(event.detail).catch((error) => reportError('切换页面失败', error));
  });
  window.addEventListener('cinevault:libraries-changed', () => {
    loadLibraries().then(async () => {
      if (state.libraries.length && !state.libraryId) {
        const usable = state.libraries.find((lib) => lib.available) ?? state.libraries[0];
        await selectLibrary(usable.id);
      } else if (state.libraries.length) {
        const still = state.libraries.find((lib) => lib.id === state.libraryId);
        if (!still) {
          const usable = state.libraries.find((lib) => lib.available) ?? state.libraries[0];
          await selectLibrary(usable.id);
        } else {
          renderPicker();
          await views[state.route]?.onLibraryChange?.();
        }
      } else {
        state.libraryId = '';
        renderPicker();
      }
      buildSidebar();
    }).catch(() => {});
  });

  await loadLibraries();

  // 尝试从配置迁移主题（仅 localStorage 无值时生效，不覆盖本地选择）
  try {
    const cfg = await call('GetConfig');
    seedThemeFromConfig(cfg.theme);
    syncThemeIcon();
  } catch {
    /* no backend config yet */
  }

  if (state.libraries.length) {
    const usable = state.libraries.find((lib) => lib.available) ?? state.libraries[0];
    await selectLibrary(usable.id);
  }

  if (needsOnboarding() && !state.libraryId) {
    state.showOnboarding = true;
    await navigate('onboarding');
    return;
  }

  await refreshContinueCount();
  await navigate(DEFAULT_ROUTE);
}

boot().catch((error) => reportError('初始化失败', error));
