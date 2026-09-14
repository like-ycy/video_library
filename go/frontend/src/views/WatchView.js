import { call } from '../api.js';
import { clear, h, reportError, toast } from '../ui.js';
import {
  formatDate,
  formatDuration,
  formatPosition,
  formatSize,
  joinOrDash,
} from '../format.js';

// 播放中记录进度的最小间隔。每个 tick 都写库是没有意义的 IO ——
// 视频位置每秒变化多次，而用户真正在意的是「关掉之后能从哪儿接着看」。
const PROGRESS_SAVE_INTERVAL_MS = 10_000;

export function createWatchView(state) {
  const actressList = h('div', { class: 'sidebar' });
  const filters = h('div', { class: 'filters' });
  const grid = h('div', { class: 'grid' });
  const moreBar = h('div', { class: 'load-more' });
  const main = h('div', { class: 'watch-main' }, [filters, grid, moreBar]);
  const el = h('div', { class: 'watch-root' }, [actressList, main]);

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
  };

  let overlay = null;
  let lightbox = null;

  // ── 数据加载 ────────────────────────────────────────────────────────────

  async function reloadAll() {
    if (!state.libraryId) {
      renderPlaceholder();
      return;
    }
    view.page = 1;
    view.items = [];
    view.activeActress = '';
    view.selectedGenres.clear();
    view.keyword = '';
    view.favoriteOnly = false;

    await Promise.all([loadActresses(), loadGenres()]);
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
  }

  async function loadGenres() {
    try {
      view.genres = (await call('ListGenres', state.libraryId)) ?? [];
    } catch (error) {
      // 类别读取失败不该阻断浏览，静默降级为「没有筛选器」即可。
      console.error('读取类别失败', error);
      view.genres = [];
    }
    renderFilters();
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
    } catch (error) {
      reportError('查询视频失败', error);
    } finally {
      view.loading = false;
      renderMoreBar();
    }
  }

  function buildFilter() {
    return {
      Actress: view.activeActress,
      Keyword: view.keyword,
      Genres: [...view.selectedGenres],
      FavoriteOnly: view.favoriteOnly,
      IncludeMissing: false,
      MinDurationMs: 0,
    };
  }

  // ── 渲染 ────────────────────────────────────────────────────────────────

  function renderPlaceholder() {
    clear(actressList);
    clear(grid);
    clear(moreBar);
    grid.append(
      h('div', { class: 'empty' }, [
        '还没有添加视频库。',
        h('br'),
        '到「刮削」页添加一个库根目录（结构应为 ',
        h('code', { text: '演员名/视频文件.mp4' }),
        '）。',
      ]),
    );
  }

  function renderActresses() {
    clear(actressList);
    const total = view.actresses.reduce((sum, item) => sum + item.total, 0);

    actressList.append(
      h('div', {
        class: `actress-item${view.activeActress === '' ? ' active' : ''}`,
        onclick: () => selectActress(''),
      }, [
        h('span', { text: '全部' }),
        h('span', { class: 'count', text: String(total) }),
      ]),
    );

    for (const item of view.actresses) {
      const label = item.missing > 0 ? `${item.actress} ⚠` : item.actress;
      actressList.append(
        h('div', {
          class: `actress-item${view.activeActress === item.actress ? ' active' : ''}`,
          title: item.missing > 0 ? `有 ${item.missing} 个文件不在磁盘上` : '',
          onclick: () => selectActress(item.actress),
        }, [
          h('span', { text: label }),
          h('span', { class: 'count', text: String(item.total) }),
        ]),
      );
    }
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

    for (const genre of view.genres.slice(0, 24)) {
      filters.append(
        h('button', {
          type: 'button',
          class: `genre-chip${view.selectedGenres.has(genre) ? ' active' : ''}`,
          text: genre,
          onclick: () => {
            if (view.selectedGenres.has(genre)) view.selectedGenres.delete(genre);
            else view.selectedGenres.add(genre);
            view.page = 1;
            renderFilters();
            loadPage(true);
          },
        }),
      );
    }

    filters.append(h('span', { class: 'spacer' }));

    filters.append(
      h('button', {
        type: 'button',
        class: `btn small${view.favoriteOnly ? ' primary' : ''}`,
        text: '仅收藏',
        onclick: () => {
          view.favoriteOnly = !view.favoriteOnly;
          view.page = 1;
          renderFilters();
          loadPage(true);
        },
      }),
    );

    const sortSelect = h('select', { class: 'field' }, [
      h('option', { value: 'release_date', text: '按发布时间' }),
      h('option', { value: 'file_size', text: '按文件大小' }),
      h('option', { value: 'duration_ms', text: '按时长' }),
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

    filters.append(
      h('button', {
        type: 'button',
        class: 'btn small',
        text: view.desc ? '降序 ↓' : '升序 ↑',
        onclick: () => {
          view.desc = !view.desc;
          view.page = 1;
          renderFilters();
          loadPage(true);
        },
      }),
    );

    filters.append(h('span', { class: 'count-label', text: `共 ${view.total} 条` }));
  }

  function renderGrid(reset) {
    if (reset) clear(grid);
    clear(moreBar);

    if (!view.items.length) {
      grid.append(
        h('div', { class: 'empty' }, [
          '没有符合条件的视频。',
          h('br'),
          '如果刚添加了库，请到「刮削」页执行一次扫描与导入。',
        ]),
      );
      return;
    }

    for (const item of view.items) {
      grid.append(buildCard(item));
    }
  }

  function buildCard(item) {
    const thumb = item.coverUrl
      ? h('img', {
        class: 'thumb',
        src: item.coverUrl,
        alt: `${item.fanha} 封面`,
        loading: 'lazy',
        onerror: (event) => { event.target.style.visibility = 'hidden'; },
      })
      : h('div', { class: 'thumb' });

    const sub = [
      item.releaseDate || '-',
      formatSize(item.fileSize),
      item.durationMs > 0 ? formatDuration(item.durationMs) : '',
    ].filter(Boolean).join(' · ');

    return h('div', { class: 'card', onclick: () => openDetail(item) }, [
      thumb,
      h('div', { class: 'meta' }, [
        h('div', { class: 'fanha' }, [
          item.fanha,
          item.favorite ? h('span', { class: 'badge', text: ' ★ 收藏' }) : null,
          item.missing ? h('span', { class: 'badge danger', text: ' 文件缺失' }) : null,
        ]),
        h('div', { class: 'title', text: item.title || '（无标题）' }),
        h('div', { class: 'sub', text: sub }),
      ]),
    ]);
  }

  function renderMoreBar() {
    clear(moreBar);
    if (view.loading) {
      moreBar.append(h('span', { class: 'status', text: '加载中…' }));
      return;
    }
    if (view.items.length >= view.total) return;

    moreBar.append(
      h('button', {
        type: 'button',
        class: 'btn',
        text: `加载更多（已显示 ${view.items.length} / ${view.total}）`,
        onclick: () => {
          view.page += 1;
          loadPage(false);
        },
      }),
    );
  }

  // ── 详情 ────────────────────────────────────────────────────────────────

  function openDetail(item) {
    closeDetail();

    const player = item.playable && item.videoUrl
      ? h('div', {
        class: 'player',
        onclick: (event) => startPlay(event.currentTarget, item),
      }, [
        h('img', {
          src: item.coverUrl,
          alt: `${item.fanha} 封面`,
          onerror: (e) => { e.target.style.visibility = 'hidden'; },
        }),
        h('div', { class: 'play-mask', text: '▶' }),
      ])
      : h('img', {
        class: 'detail-cover',
        src: item.coverUrl,
        alt: `${item.fanha} 封面`,
        onerror: (e) => { e.target.style.visibility = 'hidden'; },
      });

    const resumeHint = item.watchPositionMs > 0
      ? `（上次看到 ${formatPosition(item.watchPositionMs)}）`
      : '';

    const actions = [
      h('button', {
        type: 'button',
        class: 'btn',
        text: item.favorite ? '★ 已收藏' : '☆ 收藏',
        onclick: async (event) => {
          try {
            const favorite = await call('ToggleFavorite', state.libraryId, item.id);
            item.favorite = favorite;
            event.currentTarget.textContent = favorite ? '★ 已收藏' : '☆ 收藏';
            await loadActresses();
          } catch (error) {
            reportError('切换收藏失败', error);
          }
        },
      }),
      h('button', {
        type: 'button',
        class: 'btn',
        text: `用外部播放器打开${resumeHint}`,
        onclick: async () => {
          try {
            await call('OpenInPlayer', state.libraryId, item.id, true);
            toast('已交给外部播放器');
          } catch (error) {
            reportError('打开外部播放器失败', error);
          }
        },
      }),
    ];

    const rows = [
      ['番号', item.fanha],
      ['发布时间', formatDate(item.releaseDate)],
      ['文件时长', formatDuration(item.durationMs)],
      ['站点标注', item.siteLengthMin ? `${item.siteLengthMin} 分钟` : '-'],
      ['分辨率', item.width > 0 ? `${item.width}×${item.height}` : '-'],
      ['编码', [item.vCodec, item.acodec].filter(Boolean).join(' / ') || '-'],
      ['类别', joinOrDash(item.genres)],
      ['演员', joinOrDash(item.cast)],
      ['文件名', item.stem],
      ['文件大小', formatSize(item.fileSize)],
      ['刮削时间', item.scrapedAt || '-'],
    ].map(([label, value]) => h('div', { class: 'detail-row' }, [
      `${label}：`,
      h('b', { text: String(value) }),
    ]));

    const shots = (item.shotUrls ?? []).map((url, index) => h('img', {
      src: url,
      alt: `截图 ${index + 1}`,
      loading: 'lazy',
      onerror: (e) => { e.target.style.visibility = 'hidden'; },
      onclick: () => openLightbox(item.shotUrls, index),
    }));

    const inner = h('div', { class: 'overlay-inner' }, [
      player,
      h('div', { class: 'detail-title', text: item.title || item.fanha }),
      h('div', { class: 'detail-actions' }, actions),
      item.missing
        ? h('div', { class: 'detail-row' }, [
          h('span', { class: 'badge danger', text: '文件已不在磁盘上' }),
          ' 索引里仍有记录。重新扫描后该条目会消失。',
        ])
        : null,
      h('div', { class: 'detail-rows' }, rows),
      shots.length ? h('div', { class: 'shots' }, shots) : null,
    ]);

    overlay = h('div', {
      class: 'overlay',
      onclick: (event) => { if (event.target === overlay) closeDetail(); },
    }, [
      h('button', {
        type: 'button',
        class: 'overlay-close',
        text: '×',
        onclick: closeDetail,
      }),
      inner,
    ]);

    document.body.append(overlay);
  }

  function startPlay(container, item) {
    const video = h('video', {
      src: item.videoUrl,
      controls: true,
      autoplay: true,
    });
    video.style.width = '100%';
    video.style.borderRadius = '8px';
    video.style.background = '#000';
    container.replaceWith(video);

    if (item.watchPositionMs > 0) {
      // 位置可能在可 seek 之前设置，等元数据就绪再跳。
      video.addEventListener('loadedmetadata', () => {
        if (item.watchPositionMs / 1000 < video.duration - 5) {
          video.currentTime = item.watchPositionMs / 1000;
        }
      }, { once: true });
    }

    let lastSaved = 0;
    const save = (force) => {
      const positionMs = Math.round(video.currentTime * 1000);
      if (!force && performance.now() - lastSaved < PROGRESS_SAVE_INTERVAL_MS) return;
      lastSaved = performance.now();
      call('SaveProgress', state.libraryId, item.id, positionMs).catch((error) => {
        console.error('保存播放进度失败', error);
      });
    };

    video.addEventListener('timeupdate', () => save(false));
    video.addEventListener('pause', () => save(true));
    video.addEventListener('ended', () => save(true));
  }

  function closeDetail() {
    if (!overlay) return;
    overlay.querySelectorAll('video').forEach((video) => {
      video.pause();
      video.removeAttribute('src');
      video.load();
    });
    overlay.remove();
    overlay = null;
    closeLightbox();
  }

  // ── 截图放大 ────────────────────────────────────────────────────────────

  let shots = [];
  let position = 0;

  function openLightbox(list, index) {
    shots = list ?? [];
    position = index;
    closeLightbox();

    lightbox = h('div', {
      class: 'lightbox',
      onclick: (event) => { if (event.target === lightbox) closeLightbox(); },
    }, [
      h('button', { type: 'button', class: 'nav prev', text: '‹', onclick: () => move(-1) }),
      h('img', { id: 'lightbox-img', src: shots[position], alt: '截图' }),
      h('button', { type: 'button', class: 'nav next', text: '›', onclick: () => move(1) }),
    ]);
    document.body.append(lightbox);
  }

  function move(step) {
    if (!shots.length) return;
    position = (position + step + shots.length) % shots.length;
    const img = document.getElementById('lightbox-img');
    if (img) img.src = shots[position];
  }

  function closeLightbox() {
    lightbox?.remove();
    lightbox = null;
  }

  function selectActress(name) {
    view.activeActress = name;
    view.page = 1;
    renderActresses();
    loadPage(true);
  }

  document.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape') return;
    if (lightbox) closeLightbox();
    else if (overlay) closeDetail();
  });

  return {
    el,
    async onActivate() {
      // 切回观影页时刷新演员计数：刮削刚刚可能改变了数据。
      if (!state.libraryId) {
        renderPlaceholder();
        return;
      }
      await loadActresses();
      await loadPage(true);
    },
    async onLibraryChange() {
      await reloadAll();
    },
  };
}
