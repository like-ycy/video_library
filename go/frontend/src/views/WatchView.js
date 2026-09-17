import { call } from '../api.js';
import { clear, h, reportError, setBusy, toast } from '../ui.js';
import { emptyState, pageHeader } from '../components/shell.js';
import { videoCard } from '../components/card.js';
import { createDetailLayer } from '../components/detail.js';

/**
 * 观影模块：演员首页 / 全部影片。
 * mode: 'actresses' | 'movies'
 */
export function createWatchView(state, options = {}) {
  const mode = options.mode || 'actresses';

  const actorPanel = h('aside', { class: 'actor-panel' });
  const filters = h('div', { class: 'filters' });
  const grid = h('div', { class: 'grid' });
  const moreBar = h('div', { class: 'load-more' });
  const main = h('div', { class: 'watch-main' }, [filters, grid, moreBar]);
  const body = h('div', { class: 'watch-root' }, [actorPanel, main]);
  const headerHost = h('div');
  const root = h('div', { class: 'page', dataset: { view: mode } }, [headerHost, body]);

  const detail = createDetailLayer(state);

  const view = {
    actresses: [],
    genres: [],
    activeActress: '',
    keyword: '',
    selectedGenres: new Set(),
    favoriteOnly: false,
    sort: 'release_date',
    desc: true,
    page: 1,
    pageSize: 120,
    total: 0,
    items: [],
    loading: false,
    showActors: mode === 'actresses',
  };

  // ── 加载 ───────────────────────────────────────────────────────────────

  async function reloadAll() {
    if (!state.libraryId) {
      renderNoLibrary();
      return;
    }
    view.page = 1;
    view.items = [];
    view.activeActress = '';
    view.selectedGenres.clear();
    view.keyword = state.keyword || '';
    view.favoriteOnly = false;

    await Promise.all([
      mode === 'actresses' ? loadActresses() : Promise.resolve(),
      loadGenres(),
    ]);
    await loadPage(true);
  }

  async function loadActresses() {
    try {
      view.actresses = (await call('ListActresses', state.libraryId)) ?? [];
    } catch (error) {
      reportError('读取演员列表失败', error);
      view.actresses = [];
    }
    renderActresses();
    updateHeader();
  }

  async function loadGenres() {
    try {
      view.genres = (await call('ListGenres', state.libraryId)) ?? [];
    } catch (error) {
      console.error('读取类别失败', error);
      view.genres = [];
    }
    renderFilters();
  }

  function buildFilter() {
    return {
      Actress: mode === 'actresses' ? view.activeActress : '',
      Keyword: view.keyword,
      Genres: [...view.selectedGenres],
      FavoriteOnly: view.favoriteOnly,
      IncludeMissing: false,
      MinDurationMs: 0,
    };
  }

  async function loadPage(reset) {
    if (!state.libraryId || view.loading) return;
    view.loading = true;
    renderMoreBar();

    try {
      const page = await call('QueryVideos', state.libraryId, buildFilter(), {
        Field: view.sort,
        Desc: view.desc,
      }, view.page, view.pageSize);

      view.items = reset ? page.items : view.items.concat(page.items);
      view.total = page.total;
      renderGrid(reset);
      updateHeader();
    } catch (error) {
      reportError('查询视频失败', error);
    } finally {
      view.loading = false;
      renderMoreBar();
    }
  }

  // ── 渲染 ───────────────────────────────────────────────────────────────

  function updateHeader() {
    clear(headerHost);
    const lib = state.libraries.find((l) => l.id === state.libraryId);
    const unavailable = lib && !lib.available;

    headerHost.append(
      pageHeader({
        title: mode === 'actresses' ? '演员' : '全部影片',
        sub: unavailable
          ? `视频库不可访问：${lib?.root || ''}`
          : mode === 'actresses'
            ? `按演员浏览 · ${view.actresses.length} 位演员`
            : `全部已索引视频 · 共 ${view.total} 条`,
        actions: [
          h('button', {
            type: 'button',
            class: 'btn secondary small',
            onclick: () => reloadAll().catch((e) => reportError('刷新失败', e)),
          }, [h('span', { class: 'ms', text: 'refresh' }), h('span', { text: '刷新' })]),
          mode === 'actresses'
            ? h('button', {
              type: 'button',
              class: `btn small${view.showActors ? ' primary' : 'secondary'}`,
              onclick: () => {
                view.showActors = !view.showActors;
                body.classList.toggle('hide-actors', !view.showActors);
                updateHeader();
              },
            }, [h('span', { class: 'ms', text: 'people' }), h('span', { text: view.showActors ? '隐藏演员栏' : '显示演员栏' })])
            : null,
        ].filter(Boolean),
      }),
    );
  }

  function renderNoLibrary() {
    clear(actorPanel);
    clear(grid);
    clear(moreBar);
    clear(filters);
    body.classList.add('hide-actors');
    updateHeader();
    grid.append(emptyState({
      icon: 'video_library',
      title: '还没有添加视频库',
      desc: '添加一个目录，结构为 演员名/视频文件.mp4，即可开始浏览与播放。',
      hint: 'D:\\Videos\\Library\\<演员>\\<视频>.mp4',
      actions: [
        {
          label: '添加视频库',
          primary: true,
          icon: 'folder_open',
          onClick: () => {
            document.getElementById('add-library')?.click();
          },
        },
      ],
    }));
  }

  function renderLibraryOffline(lib) {
    clear(actorPanel);
    clear(filters);
    clear(moreBar);
    body.classList.add('hide-actors');
    updateHeader();
    clear(grid);
    grid.append(emptyState({
      icon: 'hard_drive',
      title: '视频库不可用',
      desc: '硬盘可能未连接，或目录已被移动。索引与收藏/评分/进度仍保留。',
      hint: lib?.root || '',
      actions: [
        {
          label: '打开视频库管理',
          primary: true,
          icon: 'video_library',
          onClick: () => {
            location.hash = '#settings-library';
            // 通过自定义事件让 main 路由
            window.dispatchEvent(new CustomEvent('cinevault:navigate', { detail: 'settings-library' }));
          },
        },
        {
          label: '重试',
          icon: 'refresh',
          onClick: () => reloadAll().catch(() => {}),
        },
      ],
    }));
  }

  function renderActresses() {
    clear(actorPanel);
    if (mode !== 'actresses') {
      body.classList.add('hide-actors');
      return;
    }
    body.classList.toggle('hide-actors', !view.showActors);

    const list = h('div', { class: 'actor-list' });
    list.append(
      h('button', {
        type: 'button',
        class: `actor-item${view.activeActress === '' ? ' active' : ''}`,
        onclick: () => selectActress(''),
      }, [
        h('span', { class: 'actor-name', text: '全部演员' }),
        h('span', { class: 'actor-count', text: `${view.actresses.length}位` }),
      ]),
    );
    list.append(h('div', { class: 'actor-divider' }));

    for (const item of view.actresses) {
      list.append(
        h('button', {
          type: 'button',
          class: `actor-item${view.activeActress === item.actress ? ' active' : ''}`,
          title: item.missing > 0 ? `有 ${item.missing} 个文件不在磁盘上` : '',
          onclick: () => selectActress(item.actress),
        }, [
          h('span', { class: 'actor-name', text: item.actress }),
          h('span', { style: 'display:flex;align-items:center;gap:6px;flex-shrink:0' }, [
            item.missing > 0
              ? h('span', { class: 'warn-dot', title: '有缺失文件', style: 'width:8px;height:8px;border-radius:50%;background:var(--warn)' })
              : null,
            h('span', { class: 'actor-count', text: `${item.total}部` }),
          ]),
        ]),
      );
    }

    const search = h('input', { class: 'field', type: 'search', placeholder: '搜索演员…' });
    search.addEventListener('input', () => {
      const q = search.value.trim().toLowerCase();
      list.querySelectorAll('.actor-item').forEach((btn, index) => {
        if (index === 0) return;
        const name = btn.querySelector('.actor-name')?.textContent?.toLowerCase() ?? '';
        btn.classList.toggle('hidden', Boolean(q) && !name.includes(q));
      });
    });

    actorPanel.append(
      h('div', { class: 'actor-panel-head' }, [
        h('div', { class: 'search-field' }, [
          h('span', { class: 'ms', text: 'search' }),
          search,
        ]),
      ]),
      list,
    );
  }

  function renderFilters() {
    clear(filters);

    const keyword = h('input', {
      class: 'field',
      type: 'search',
      placeholder: '搜索番号或标题',
      value: view.keyword,
      style: 'width:220px',
    });
    let debounce = null;
    keyword.addEventListener('input', () => {
      view.keyword = keyword.value.trim();
      window.clearTimeout(debounce);
      debounce = window.setTimeout(() => {
        view.page = 1;
        loadPage(true);
      }, 250);
    });
    filters.append(keyword);

    for (const genre of view.genres.slice(0, 20)) {
      filters.append(h('button', {
        type: 'button',
        class: `chip${view.selectedGenres.has(genre) ? ' active' : ''}`,
        text: genre,
        onclick: () => {
          if (view.selectedGenres.has(genre)) view.selectedGenres.delete(genre);
          else view.selectedGenres.add(genre);
          view.page = 1;
          renderFilters();
          loadPage(true);
        },
      }));
    }

    filters.append(h('span', { class: 'spacer' }));
    filters.append(h('button', {
      type: 'button',
      class: `btn small${view.favoriteOnly ? ' primary' : 'secondary'}`,
      onclick: () => {
        view.favoriteOnly = !view.favoriteOnly;
        view.page = 1;
        renderFilters();
        loadPage(true);
      },
    }, [h('span', { class: `ms${view.favoriteOnly ? ' fill' : ''}`, text: 'favorite' }), h('span', { text: '仅收藏' })]));

    const sortSelect = h('select', { class: 'select' }, [
      h('option', { value: 'release_date', text: '按发布时间' }),
      h('option', { value: 'file_size', text: '按文件大小' }),
      h('option', { value: 'duration_ms', text: '按真实时长' }),
      h('option', { value: 'title', text: '按标题' }),
      h('option', { value: 'fanha', text: '按番号' }),
      h('option', { value: 'actress', text: '按演员' }),
      h('option', { value: 'scraped_at', text: '按刮削时间' }),
    ]);
    sortSelect.value = view.sort;
    sortSelect.addEventListener('change', () => {
      view.sort = sortSelect.value;
      view.page = 1;
      loadPage(true);
    });
    filters.append(sortSelect);

    filters.append(h('button', {
      type: 'button',
      class: 'btn small secondary',
      text: view.desc ? '降序' : '升序',
      onclick: () => {
        view.desc = !view.desc;
        view.page = 1;
        renderFilters();
        loadPage(true);
      },
    }));

    filters.append(h('span', { class: 'count-label', text: `共 ${view.total} 条` }));
  }

  function renderGrid(reset) {
    if (reset) clear(grid);
    clear(moreBar);

    if (!view.items.length) {
      grid.append(emptyState({
        icon: view.keyword || view.favoriteOnly || view.selectedGenres.size ? 'search_off' : 'movie',
        title: view.keyword || view.favoriteOnly || view.selectedGenres.size
          ? '没有符合条件的视频'
          : '索引中还没有视频',
        desc: view.keyword || view.favoriteOnly || view.selectedGenres.size
          ? '试着放宽筛选条件，或清除搜索词。'
          : '如果刚添加了库，请到「扫描与待处理」执行扫描与导入。',
        actions: [
          {
            label: '去扫描入库',
            primary: true,
            icon: 'manage_search',
            onClick: () => window.dispatchEvent(new CustomEvent('cinevault:navigate', { detail: 'scan' })),
          },
        ],
      }));
      return;
    }

    for (const item of view.items) {
      grid.append(videoCard(item, {
        onOpen: (cardItem) => {
          cardItem.onFavoriteChange = async () => {
            await loadPage(true);
          };
          detail.open(cardItem);
        },
        onPlay: (cardItem) => {
          cardItem.onFavoriteChange = async () => {
            await loadPage(true);
          };
          detail.open(cardItem);
          // 自动播放
          setTimeout(() => {
            document.querySelector('.overlay .player')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
          }, 50);
        },
      }));
    }
  }

  function renderMoreBar() {
    clear(moreBar);
    if (view.loading) {
      moreBar.append(h('span', { class: 'count-label', text: '加载中…' }));
      return;
    }
    if (view.items.length >= view.total) return;
    moreBar.append(h('button', {
      type: 'button',
      class: 'btn secondary',
      text: `加载更多（已显示 ${view.items.length} / ${view.total}）`,
      onclick: () => {
        view.page += 1;
        loadPage(false);
      },
    }));
  }

  function selectActress(name) {
    view.activeActress = name;
    view.page = 1;
    renderActresses();
    loadPage(true);
  }

  return {
    el: root,
    async onActivate() {
      if (!state.libraryId) {
        renderNoLibrary();
        return;
      }
      const lib = state.libraries.find((l) => l.id === state.libraryId);
      if (lib && !lib.available) {
        renderLibraryOffline(lib);
        return;
      }
      if (state.keyword && state.keyword !== view.keyword) {
        view.keyword = state.keyword;
      }
      await loadActresses();
      await loadGenres();
      renderFilters();
      await loadPage(true);
    },
    async onLibraryChange() {
      detail.close();
      await reloadAll();
    },
  };
}
