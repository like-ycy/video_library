import { call } from '../api.js';
import { clear, h, reportError, toast } from '../ui.js';
import { emptyState, pageHeader, statusBadge } from '../components/shell.js';
import { createDetailLayer } from '../components/detail.js';

const KIND_META = {
  unrecognized: {
    label: '番号无法识别',
    icon: 'help_outline',
    tip: '按 <字母串>-<数字> 规范文件名，例如 IPX-001.mp4',
  },
  sidecar_corrupt: {
    label: '边车 JSON 损坏',
    icon: 'broken_image',
    tip: '检查 <演员>/meta/<演员>.json 是否为合法 JSON',
  },
  missing_file: {
    label: '文件缺失',
    icon: 'folder_off',
    tip: '重新连接硬盘或重新扫描索引',
  },
  sidecar_missing: {
    label: '缺少边车元数据',
    icon: 'data_object',
    tip: '在扫描页勾选后开始刮削',
  },
};

/** 异常与修复：合并扫描 issues + 失败项启发提示 */
export function createIssuesView(state) {
  const headerHost = h('div');
  const body = h('div', { class: 'page-body' });
  const root = h('div', { class: 'page', dataset: { view: 'issues' } }, [headerHost, body]);

  const view = {
    issues: [],
    candidates: [],
    loading: false,
    filterKind: '',
  };

  function updateHeader() {
    clear(headerHost);
    headerHost.append(pageHeader({
      title: '异常与修复',
      sub: '按问题类型分组，附带修复建议',
      actions: [
        h('button', {
          type: 'button',
          class: 'btn primary small',
          onclick: () => refresh().catch((e) => reportError('刷新失败', e)),
        }, [h('span', { class: 'ms', text: 'refresh' }), h('span', { text: '重新扫描问题' })]),
        h('button', {
          type: 'button',
          class: 'btn secondary small',
          onclick: () => window.dispatchEvent(new CustomEvent('cinevault:navigate', { detail: 'scan' })),
        }, [h('span', { class: 'ms', text: 'manage_search' }), h('span', { text: '去扫描' })]),
      ],
    }));
  }

  async function refresh() {
    updateHeader();
    clear(body);
    if (!state.libraryId) {
      body.append(emptyState({
        icon: 'video_library',
        title: '没有视频库',
        desc: '先添加视频库再检查异常。',
      }));
      return;
    }

    view.loading = true;
    try {
      const result = await call('ScanLibrary', state.libraryId);
      view.issues = result.issues ?? [];
      view.candidates = result.candidates ?? [];
    } catch (error) {
      reportError('扫描失败', error);
      view.issues = [];
      view.candidates = [];
    } finally {
      view.loading = false;
    }
    render();
  }

  function groupIssues() {
    const groups = new Map();
    const push = (kind, row) => {
      if (!groups.has(kind)) groups.set(kind, []);
      groups.get(kind).push(row);
    };

    for (const issue of view.issues) {
      push(issue.kind || 'unknown', issue);
    }

    // 候选中缺图 / 未刮削也归到问题列表，方便一键处理
    for (const c of view.candidates) {
      if (!c.scraped) {
        push('unscraped', {
          kind: 'unscraped',
          actress: c.actress,
          subject: c.fanha,
          message: '尚未刮削元数据',
          stem: c.stem,
          fanha: c.fanha,
        });
      } else if (c.missingArt) {
        push('missing_art', {
          kind: 'missing_art',
          actress: c.actress,
          subject: c.fanha,
          message: '封面或截图缺失',
          stem: c.stem,
          fanha: c.fanha,
        });
      }
    }
    return groups;
  }

  function render() {
    clear(body);
    const groups = groupIssues();

    if (!groups.size) {
      body.append(emptyState({
        icon: 'check_circle',
        title: '没有发现问题',
        desc: '当前视频库扫描未发现异常或待处理条目。',
      }));
      return;
    }

    const knownOrder = ['unrecognized', 'sidecar_corrupt', 'missing_file', 'sidecar_missing', 'unscraped', 'missing_art'];
    const keys = [
      ...knownOrder.filter((k) => groups.has(k)),
      ...[...groups.keys()].filter((k) => !knownOrder.includes(k)),
    ];

    const summary = h('div', { class: 'issue-summary' });
    for (const key of keys) {
      const meta = KIND_META[key] || { label: key, icon: 'priority_high', tip: '' };
      summary.append(h('div', { class: 'panel issue-group-card' }, [
        h('div', { class: 'issue-group-head' }, [
          h('span', { class: 'ms', text: meta.icon }),
          h('strong', { text: meta.label }),
          h('span', { class: 'mono', style: 'margin-left:auto;color:var(--primary)', text: String(groups.get(key).length) }),
        ]),
        meta.tip ? h('div', { class: 'panel-desc', text: meta.tip }) : null,
      ]));
    }
    body.append(summary);

    for (const key of keys) {
      const meta = KIND_META[key] || { label: key, icon: 'priority_high', tip: '' };
      const list = groups.get(key);
      body.append(h('section', { class: 'panel issue-section' }, [
        h('div', { class: 'panel-title', style: 'display:flex;align-items:center;gap:8px' }, [
          h('span', { class: 'ms', text: meta.icon }),
          h('span', { text: `${meta.label}（${list.length}）` }),
        ]),
        h('div', { class: 'issue-list' }, list.map((row) => h('div', { class: 'issue-row' }, [
          h('div', { class: 'issue-main' }, [
            h('div', {}, [
              h('span', { class: 'mono', style: 'color:var(--primary);font-weight:600', text: row.subject || row.fanha || '—' }),
              row.actress ? h('span', { style: 'margin-left:8px;color:var(--muted)', text: row.actress }) : null,
            ]),
            h('div', { class: 'panel-desc', text: row.message || '' }),
            meta.tip ? h('div', { class: 'issue-tip', text: `修复建议：${meta.tip}` }) : null,
          ]),
          key === 'unscraped' || key === 'missing_art'
            ? h('button', {
              type: 'button',
              class: 'btn primary small',
              onclick: () => {
                window.dispatchEvent(new CustomEvent('cinevault:navigate', { detail: 'scan' }));
                toast(`请在扫描页处理 ${row.subject}`, 'info');
              },
            }, [h('span', { class: 'ms', text: 'auto_fix_high' }), h('span', { text: '去处理' })])
            : null,
        ]))),
      ]));
    }
  }

  return {
    el: root,
    async onActivate() {
      await refresh();
    },
    async onLibraryChange() {
      await refresh();
    },
  };
}
