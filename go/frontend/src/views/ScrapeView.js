import { call, on } from '../api.js';
import { clear, h, reportError, setStatus, toast } from '../ui.js';
import { formatSize } from '../format.js';

const STAGE_TEXT = {
  search: '搜索',
  detail: '详情页',
  download_cover: '下载封面',
  download_shots: '下载截图',
};

export function createScrapeView(state) {
  const bar = h('div', { class: 'scrape-bar' });
  const table = h('table', { class: 'candidates' });
  const tableWrap = h('div', { class: 'table-wrap' }, [table]);
  const progressPanel = h('div', { class: 'progress-panel' });
  const body = h('div', { class: 'scrape-body' }, [tableWrap, progressPanel]);
  const el = h('div', { class: 'scrape-main' }, [bar, body]);

  const view = {
    candidates: [],
    issues: [],
    selected: new Set(),
    onlyUnscraped: true,
    force: false,
    scraping: false,
    running: false,
    total: 0,
    done: 0,
    ok: 0,
    failed: 0,
    skipped: 0,
    results: [],
    indexing: false,
    showLog: false,
    log: [],
    health: null,
  };

  // ── 事件订阅 ────────────────────────────────────────────────────────────

  on('scrape:progress', (payload) => {
    const entry = ensureResult(payload.fanha);
    entry.stage = payload.stage;
    entry.percent = payload.percent;
    entry.status = 'running';
    renderProgress();
  });

  on('scrape:item-done', (payload) => {
    const entry = ensureResult(payload.fanha);
    entry.status = 'ok';
    entry.message = payload.title
      ? payload.title
      : `完成（截图 ${payload.shots}/${payload.totalShots}）`;
    if (payload.totalShots > 0 && payload.shots < payload.totalShots) {
      entry.message += `（${payload.shots}/${payload.totalShots} 张）`;
    }
    view.ok += 1;
    view.done += 1;
    renderProgress();
  });

  on('scrape:item-failed', (payload) => {
    const entry = ensureResult(payload.fanha);
    entry.status = 'failed';
    entry.message = payload.detail ? `${payload.message}：${payload.detail}` : payload.message;
    entry.retryable = payload.retryable;
    entry.reason = payload.reason;
    view.failed += 1;
    view.done += 1;
    renderProgress();
  });

  on('scrape:finished', (payload) => {
    view.running = false;
    view.skipped = payload.skipped ?? 0;
    if (payload.fatalMessage) {
      toast(payload.fatalMessage, 'error');
    } else if (payload.error) {
      toast(`刮削结束但有错误：${payload.error}`, 'error');
    } else if (payload.canceled) {
      toast(`已取消：成功 ${payload.ok}，失败 ${payload.failed}，未处理 ${payload.skipped}`);
    } else {
      toast(`完成：成功 ${payload.ok}，失败 ${payload.failed}`);
    }
    renderBar();
    renderProgress();
  });

  on('index:progress', (payload) => {
    view.indexing = true;
    setStatus(`更新索引 ${payload.done}/${payload.total}`);
  });

  on('index:done', () => {
    view.indexing = false;
    setStatus('');
    toast('索引已更新');
    refreshCandidates();
  });

  on('index:failed', (payload) => {
    view.indexing = false;
    setStatus('');
    reportError('更新索引失败', payload.message);
  });

  // ── 数据 ────────────────────────────────────────────────────────────────

  async function refreshCandidates() {
    if (!state.libraryId) {
      renderAll();
      return;
    }
    try {
      const result = await call('ScanLibrary', state.libraryId);
      view.candidates = result.candidates ?? [];
      view.issues = result.issues ?? [];
    } catch (error) {
      reportError('扫描视频库失败', error);
      view.candidates = [];
      view.issues = [];
    }
    // 选中集合里剔除已经不存在的条目。
    const existing = new Set(view.candidates.map((item) => item.stem));
    for (const stem of [...view.selected]) {
      if (!existing.has(stem)) view.selected.delete(stem);
    }
    renderAll();
  }

  async function checkHealth() {
    try {
      view.health = await call('ScraperHealth');
    } catch (error) {
      // 拿不到环境信息不是致命错误：可能只是还没打包刮削器。
      view.health = null;
      view.healthError = error?.message ?? String(error);
    }
    renderBar();
  }

  async function importIndex() {
    if (!state.libraryId || view.indexing) return;
    setStatus('扫描并更新索引…');
    view.indexing = true;
    try {
      const stats = await call('ImportLibrary', state.libraryId);
      toast(
        `索引已更新：${stats.Found} 条，探测 ${stats.Probed} 条，` +
        `复用缓存 ${stats.MediaCached} 条${stats.Vanished ? `，${stats.Vanished} 条文件缺失` : ''}`,
      );
      if (stats.Issues?.length) {
        toast(`有 ${stats.Issues.length} 个条目需要注意，见下方问题列表`, 'error');
      }
      await refreshCandidates();
    } catch (error) {
      reportError('更新索引失败', error);
    } finally {
      view.indexing = false;
      setStatus('');
      renderAll();
    }
  }

  async function rebuildIndex() {
    if (!state.libraryId || view.indexing) return;
    view.indexing = true;
    setStatus('重建索引…');
    try {
      const stats = await call('RebuildIndex', state.libraryId);
      toast(`索引已重建：${stats.Found} 条`);
      await refreshCandidates();
    } catch (error) {
      reportError('重建索引失败', error);
    } finally {
      view.indexing = false;
      setStatus('');
      renderAll();
    }
  }

  async function startScrape() {
    if (!state.libraryId) return;
    const stems = [...view.selected];
    if (!stems.length) {
      toast('请先勾选要刮削的条目', 'error');
      return;
    }

    // 开始前检查环境：Chrome 缺失时直接给出指引，比让用户等一堆失败更好。
    await checkHealth();
    if (view.health && !(view.health.chrome?.found)) {
      toast('未找到 Chrome 浏览器，刮削无法进行。请先安装 Chrome。', 'error');
      return;
    }
    if (view.healthError) {
      toast(`刮削器不可用：${view.healthError}`, 'error');
      return;
    }

    view.running = true;
    view.total = stems.length;
    view.done = 0;
    view.ok = 0;
    view.failed = 0;
    view.skipped = 0;
    view.results = [];
    renderAll();

    try {
      await call('StartScrape', state.libraryId, stems, view.force);
      toast(`已开始刮削 ${stems.length} 条`);
    } catch (error) {
      view.running = false;
      reportError('启动刮削失败', error);
      renderAll();
    }
  }

  async function cancelScrape() {
    try {
      const canceled = await call('CancelScrape');
      toast(canceled ? '正在终止刮削器…' : '当前没有进行中的刮削');
    } catch (error) {
      reportError('取消刮削失败', error);
    }
  }

  async function loadLog() {
    try {
      view.log = (await call('GetScrapeLog')) ?? [];
    } catch (error) {
      reportError('读取日志失败', error);
      view.log = [];
    }
    renderProgress();
  }

  // ── 渲染 ────────────────────────────────────────────────────────────────

  function visibleCandidates() {
    if (!view.onlyUnscraped) return view.candidates;
    return view.candidates.filter((item) => !item.scraped || item.missingArt);
  }

  function renderAll() {
    renderBar();
    renderTable();
    renderProgress();
  }

  function renderBar() {
    clear(bar);

    if (!state.libraryId) {
      bar.append(h('span', { class: 'status', text: '请先添加一个视频库' }));
      return;
    }

    const busy = view.indexing || view.running;

    bar.append(
      h('button', {
        type: 'button', class: 'btn', text: '扫描',
        disabled: busy, onclick: refreshCandidates,
      }),
      h('button', {
        type: 'button', class: 'btn', text: '更新索引',
        disabled: busy, onclick: importIndex,
      }),
      h('button', {
        type: 'button', class: 'btn small', text: '重建索引',
        disabled: busy, onclick: rebuildIndex,
      }),
      h('label', { class: 'status' }, [
        h('input', {
          type: 'checkbox',
          checked: view.onlyUnscraped,
          onchange: (event) => {
            view.onlyUnscraped = event.target.checked;
            renderTable();
          },
        }),
        ' 仅显示未刮削/缺图',
      ]),
      h('label', { class: 'status' }, [
        h('input', {
          type: 'checkbox',
          checked: view.force,
          onchange: (event) => { view.force = event.target.checked; },
        }),
        ' 重新下载图片',
      ]),
      h('button', {
        type: 'button', class: 'btn small', text: '全选可见',
        disabled: busy, onclick: () => {
          for (const item of visibleCandidates()) view.selected.add(item.stem);
          renderTable();
        },
      }),
      h('button', {
        type: 'button', class: 'btn small', text: '清空选择',
        onclick: () => { view.selected.clear(); renderTable(); },
      }),
      h('span', { class: 'spacer' }),
    );

    if (view.healthError) {
      bar.append(h('span', { class: 'badge danger', text: '刮削器不可用' }));
    } else if (view.health?.chrome?.found === false) {
      bar.append(h('span', { class: 'badge danger', text: '缺少 Chrome' }));
    } else if (view.health?.driver?.ready !== true) {
      // 驱动未就绪是正常状态（首次刮削时 seleniumbase 会自己下载），只有
      // 「落地目录不可写」才需要用户处理 —— 那通常意味着产物装在受保护位置，
      // 且重定向没能生效。把目录和可写性一起放进 tooltip，出问题时一眼能判断。
      const driver = view.health?.driver ?? {};
      bar.append(h('span', {
        class: driver.writable ? 'badge warn' : 'badge danger',
        title: `驱动目录：${driver.dir || '未知'}（可写：${driver.writable ? '是' : '否'}）`,
        text: driver.writable ? '驱动将在首次刮削时下载' : '驱动目录不可写',
      }));
    }

    bar.append(
      h('button', {
        type: 'button', class: 'btn', text: `开始刮削（${view.selected.size}）`,
        disabled: busy || view.selected.size === 0,
        onclick: startScrape,
      }),
      h('button', {
        type: 'button', class: 'btn danger', text: '取消',
        disabled: !view.running,
        onclick: cancelScrape,
      }),
    );
  }

  function renderTable() {
    clear(table);

    if (!state.libraryId) {
      table.append(h('tbody', {}, [
        h('tr', {}, [h('td', { class: 'empty', text: '请先在右上角添加一个视频库' })]),
      ]));
      return;
    }

    const rows = visibleCandidates();
    table.append(h('thead', {}, [
      h('tr', {}, [
        h('th', { style: 'width:34px' }),
        h('th', { text: '演员' }),
        h('th', { text: '番号' }),
        h('th', { text: '文件' }),
        h('th', { text: '大小' }),
        h('th', { text: '状态' }),
      ]),
    ]));

    const tbody = h('tbody');
    for (const item of rows) {
      tbody.append(h('tr', {}, [
        h('td', {}, [
          h('input', {
            type: 'checkbox',
            checked: view.selected.has(item.stem),
            onchange: (event) => {
              if (event.target.checked) view.selected.add(item.stem);
              else view.selected.delete(item.stem);
              renderBar();
            },
          }),
        ]),
        h('td', { text: item.actress }),
        h('td', { text: item.fanha }),
        h('td', { text: item.videoFile }),
        h('td', { text: formatSize(item.fileSize) }),
        h('td', {}, [statusBadge(item)]),
      ]));
    }
    table.append(tbody);

    if (!rows.length) {
      tbody.append(h('tr', {}, [
        h('td', {
          class: 'empty',
          colspan: 6,
          text: view.candidates.length
            ? '全部条目都已刮削。取消勾选「仅显示未刮削」可以看到它们。'
            : '没有扫描到视频文件。确认目录结构是「演员名/视频文件.mp4」。',
        }),
      ]));
    }

    if (view.issues.length) {
      const list = h('div', { class: 'item-list' });
      for (const issue of view.issues) {
        list.append(h('div', { class: 'item failed' }, [
          h('span', { class: 'f', text: issue.actress || '-' }),
          h('span', { class: 'msg', text: `${issue.subject}：${issue.message}` }),
        ]));
      }
      tbody.append(h('tr', {}, [
        h('td', { colspan: 6 }, [
          h('div', { class: 'detail-row', text: `扫描发现 ${view.issues.length} 个问题：` }),
          list,
        ]),
      ]));
    }
  }

  function statusBadge(item) {
    if (!item.scraped) return h('span', { class: 'badge', text: '未刮削' });
    if (item.missingArt) return h('span', { class: 'badge warn', text: '缺图' });
    return h('span', { class: 'badge ok', text: '已刮削' });
  }

  function ensureResult(fanha) {
    let entry = view.results.find((item) => item.fanha === fanha);
    if (!entry) {
      entry = { fanha, status: 'pending', message: '', percent: 0, stage: '' };
      view.results.push(entry);
    }
    return entry;
  }

  function renderProgress() {
    clear(progressPanel);

    const percent = view.total ? Math.round((view.done / view.total) * 100) : 0;
    const barFill = h('div');
    barFill.style.width = `${percent}%`;

    progressPanel.append(h('div', { class: 'panel' }, [
      h('div', { class: 'detail-row' }, [
        view.running ? '刮削进行中…' : '就绪',
        view.total ? `  ${view.done} / ${view.total}` : '',
      ]),
      h('div', { class: 'bar' }, [barFill]),
      h('div', { class: 'counts' }, [
        h('span', { class: 'ok', text: `成功 ${view.ok}` }),
        h('span', { class: 'failed', text: `失败 ${view.failed}` }),
        h('span', { text: `未处理 ${view.skipped}` }),
      ]),
      h('div', { class: 'detail-actions' }, [
        h('button', {
          type: 'button', class: 'btn small',
          text: view.showLog ? '显示逐条结果' : '显示日志',
          onclick: async () => {
            view.showLog = !view.showLog;
            if (view.showLog) await loadLog();
            else renderProgress();
          },
        }),
      ]),
    ]));

    const scroll = h('div', { class: 'panel scroll' });

    if (view.showLog) {
      scroll.append(h('div', { class: 'log', text: view.log.join('\n') || '（暂无日志）' }));
    } else if (!view.results.length) {
      scroll.append(h('div', { class: 'detail-row', text: '勾选条目后点「开始刮削」。' }));
    } else {
      const list = h('div', { class: 'item-list' });
      for (const entry of view.results) {
        const stage = entry.status === 'running' && entry.stage
          ? `${STAGE_TEXT[entry.stage] ?? entry.stage} ${Math.round((entry.percent ?? 0) * 100)}%`
          : '';
        list.append(h('div', { class: `item ${entry.status}` }, [
          h('span', { class: 'f', text: entry.fanha }),
          h('span', { class: 'msg', text: stage || entry.message || '等待中' }),
        ]));
      }
      scroll.append(list);
    }
    progressPanel.append(scroll);
  }

  return {
    el,
    async onActivate() {
      await checkHealth();
      await refreshCandidates();
    },
    async onLibraryChange() {
      view.selected.clear();
      view.results = [];
      view.total = 0;
      view.done = 0;
      view.ok = 0;
      view.failed = 0;
      view.skipped = 0;
      view.showLog = false;
      view.log = [];
      await checkHealth();
      await refreshCandidates();
    },
  };
}
