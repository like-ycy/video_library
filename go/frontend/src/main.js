import { call } from './api.js';
import { clear, h, reportError, toast } from './ui.js';
import { createWatchView } from './views/WatchView.js';
import { createScrapeView } from './views/ScrapeView.js';

// 全局共享状态。两个视图都持有它的引用，因此切库时能立刻反映变化。
const state = { libraryId: '', libraries: [] };

const watch = createWatchView(state);
const scrape = createScrapeView(state);
const views = { watch, scrape };

function mount() {
  document.getElementById('view-watch').append(watch.el);
  document.getElementById('view-scrape').append(scrape.el);
}

async function activate(name) {
  for (const [key, view] of Object.entries(views)) {
    document.getElementById(`view-${key}`)?.classList.toggle('hidden', key !== name);
    document
      .querySelector(`.nav-item[data-view="${key}"]`)
      ?.classList.toggle('active', key === name);
  }
  await views[name]?.onActivate();
}

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
  clear(picker);

  if (!state.libraries.length) {
    picker.append(h('option', { text: '尚未添加视频库' }));
    picker.disabled = true;
    return;
  }

  picker.disabled = false;
  for (const lib of state.libraries) {
    picker.append(
      h('option', {
        value: lib.id,
        // 不可访问的库仍然列出：用户看到「（不可访问）」比看到它凭空消失
        // 更容易理解发生了什么（盘没插、路径被改了）。
        text: lib.available ? lib.root : `${lib.root}（不可访问）`,
      }),
    );
  }
  picker.value = state.libraryId;
}

async function selectLibrary(id) {
  if (!id) return;
  state.libraryId = id;
  const picker = document.getElementById('library-picker');
  if (picker) picker.value = id;
  await Promise.all([watch.onLibraryChange(), scrape.onLibraryChange()]);
}

async function addLibrary() {
  try {
    const root = await call('PickLibraryRoot');
    if (!root) return; // 用户取消了选择
    const lib = await call('AddLibrary', root);
    await loadLibraries();
    await selectLibrary(lib.id);
    toast(`已添加视频库：${lib.root}`);
  } catch (error) {
    reportError('添加视频库失败', error);
  }
}

async function boot() {
  mount();

  document.getElementById('add-library')?.addEventListener('click', addLibrary);
  document.getElementById('library-picker')?.addEventListener('change', (event) => {
    selectLibrary(event.target.value).catch((error) => reportError('切换视频库失败', error));
  });
  for (const item of document.querySelectorAll('.nav-item')) {
    item.addEventListener('click', () => {
      activate(item.dataset.view).catch((error) => reportError('切换页面失败', error));
    });
  }

  await loadLibraries();

  if (state.libraries.length) {
    // 优先选一个可访问的库，避免开局就因为盘没插而什么都不显示。
    const usable = state.libraries.find((lib) => lib.available) ?? state.libraries[0];
    await selectLibrary(usable.id);
    await activate('watch');
  } else {
    // 没有库时停在刮削页 —— 那是添加库的入口。
    await activate('scrape');
  }
}

boot().catch((error) => reportError('初始化失败', error));
