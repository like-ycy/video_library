import { call, on } from '../api.js';
import { clear, h, setBusy, toast } from '../ui.js';
import { emptyState, pageHeader } from '../components/shell.js';

const STAGE_TEXT = {
  search: '搜索',
  detail: '详情页',
  download_cover: '下载封面',
  download_shots: '下载截图',
};

/** 任务监控：实时刮削进度（从全局 scrape 状态聚合） */
export function createTaskMonitorView(state) {
  const headerHost = h('div');
  const body = h('div', { class: 'page-body' });
  const root = h('div', { class: 'page', dataset: { view: 'tasks' } }, [headerHost, body]);

  // 由 ScrapeView 或自身事件写入
  if (!state.taskMonitor) {
    state.taskMonitor = {
      running: false,
      total: 0,
      done: 0,
      ok: 0,
      failed: 0,
      skipped: 0,
      canceled: false,
      finished: false,
      fatal: '',
      fatalMessage: '',
      results: [],
      log: [],
    };
  }
  const view = state.taskMonitor;

  on('scrape:progress', () => {
    // ScrapeView 是状态权威；这里只触发重绘，避免双份计数。
    render();
  });

  on('scrape:item-done', () => render());
  on('scrape:item-failed', () => render());

  on('scrape:finished', () => {
    setBusy(false);
    render();
  });

  on('index:done', () => {
    toast('索引已更新', 'ok');
  });

  on('index:failed', (payload) => {
    toast(`索引更新失败：${payload?.message || ''}`, 'error');
  });

  function updateHeader() {
    clear(headerHost);
    headerHost.append(pageHeader({
      title: '任务监控',
      sub: view.running
        ? `刮削进行中 ${view.done} / ${view.total || '?'}`
        : view.finished
          ? (view.canceled ? '上次任务已取消' : '上次任务已结束')
          : '等待任务…',
      actions: [
        view.running
          ? h('button', {
            type: 'button',
            class: 'btn danger small',
            onclick: async () => {
              await call('CancelScrape');
              toast('正在终止…', 'warn');
              render();
            },
          }, [h('span', { class: 'ms', text: 'stop_circle' }), h('span', { text: '取消任务' })])
          : null,
        h('button', {
          type: 'button',
          class: 'btn secondary small',
          onclick: async () => {
            view.log = (await call('GetScrapeLog')) ?? [];
            render();
          },
        }, [h('span', { class: 'ms', text: 'receipt_long' }), h('span', { text: '刷新日志' })]),
      ].filter(Boolean),
    }));
  }

  function render() {
    updateHeader();
    clear(body);

    const percent = view.total ? Math.round((view.done / view.total) * 100) : (view.finished ? 100 : 0);
    const fill = h('div', { style: `width:${percent}%` });

    body.append(h('div', { class: 'panel', style: 'margin-bottom:14px' }, [
      h('div', { style: 'display:flex;justify-content:space-between;margin-bottom:8px' }, [
        h('span', {
          text: view.running ? '运行中' : view.canceled ? '已取消' : view.finished ? '已完成' : '空闲',
          class: view.running ? 'badge running' : view.canceled ? 'badge missing-art' : view.finished ? 'badge scraped' : 'badge pending',
        }),
        h('span', { class: 'mono', text: `${percent}%` }),
      ]),
      h('div', { class: 'bar' }, [fill]),
      h('div', { class: 'counts', style: 'margin-top:10px' }, [
        h('span', { class: 'ok', text: `成功 ${view.ok}` }),
        h('span', { class: 'failed', text: `失败 ${view.failed}` }),
        h('span', { text: `跳过 ${view.skipped}` }),
        h('span', { text: `总 ${view.total || view.results.length}` }),
      ]),
      view.fatalMessage
        ? h('div', {
          style: 'margin-top:10px;padding:8px;border-radius:8px;background:var(--danger-bg);color:var(--danger);font-size:12px',
          text: `致命错误：${view.fatalMessage}`,
        })
        : null,
    ]));

    if (!view.results.length && !view.log.length) {
      body.append(emptyState({
        icon: 'dns',
        title: '暂无任务记录',
        desc: '在「扫描与待处理」勾选条目并开始刮削后，这里会显示逐条进度。',
        actions: [{
          label: '去开始刮削',
          primary: true,
          icon: 'manage_search',
          onClick: () => window.dispatchEvent(new CustomEvent('cinevault:navigate', { detail: 'scan' })),
        }],
      }));
      return;
    }

    const list = h('div', { class: 'item-list' });
    if (view.results.length) {
      for (const entry of view.results) {
        const stage = entry.status === 'running' && entry.stage
          ? `${STAGE_TEXT[entry.stage] ?? entry.stage} ${Math.round((entry.percent ?? 0) * 100)}%`
          : '';
        list.append(h('div', { class: `item ${entry.status}` }, [
          h('span', { class: 'f', text: entry.fanha }),
          h('span', { class: 'msg', text: stage || entry.message || '等待中' }),
        ]));
      }
    }

    body.append(h('div', { class: 'panel' }, [
      h('div', { class: 'panel-title', text: '逐条结果' }),
      list.children.length ? list : h('div', { class: 'panel-desc', text: '暂无逐条结果。' }),
    ]));

    if (view.log.length) {
      body.append(h('div', { class: 'panel', style: 'margin-top:12px' }, [
        h('div', { class: 'panel-title', text: '最近日志' }),
        h('div', { class: 'log', text: view.log.join('\n') }),
      ]));
    }
  }

  // ScrapeView 同步进度到共享状态时调用
  state.taskMonitorSync = () => {
    try {
      render();
    } catch {
      /* view may not be mounted */
    }
  };

  return {
    el: root,
    async onActivate() {
      if (typeof view.running === 'boolean') {
        try {
          view.running = await call('ScrapeStatus');
        } catch {
          /* ignore */
        }
      }
      render();
    },
    async onLibraryChange() {
      render();
    },
  };
}
