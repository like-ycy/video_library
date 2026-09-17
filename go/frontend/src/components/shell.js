import { h } from '../ui.js';

/** Material Symbols 图标节点。禁止 emoji。 */
export function icon(name, className = '') {
  return h('span', { class: ['ms', className].filter(Boolean).join(' '), text: name });
}

/** 状态徽标：未刮削 / 已刮削 / 缺图 / 文件异常。 */
export function statusBadge(state) {
  const map = {
    pending: { cls: 'pending', icon: 'help_outline', label: '未刮削' },
    scraped: { cls: 'scraped', icon: 'check_circle', label: '已刮削' },
    missing_art: { cls: 'missing-art', icon: 'image_not_supported', label: '缺图' },
    broken: { cls: 'broken', icon: 'broken_image', label: '文件异常' },
    running: { cls: 'running', icon: 'sync', label: '进行中' },
    neutral: { cls: 'neutral', icon: 'circle', label: '—' },
  };
  const spec = map[state] ?? map.neutral;
  return h('span', { class: `badge ${spec.cls}` }, [icon(spec.icon), h('span', { text: spec.label })]);
}

/**
 * 空状态。
 * opts: { icon, title, desc, hint, actions: [{label, primary?, onClick}] }
 */
export function emptyState(opts = {}) {
  const children = [h('div', { class: 'empty-icon' }, [icon(opts.icon || 'inbox')])];
  if (opts.title) children.push(h('div', { class: 'empty-title', text: opts.title }));
  if (opts.desc) children.push(h('div', { class: 'empty-desc', text: opts.desc }));
  if (opts.hint) children.push(h('div', { class: 'empty-hint mono', text: opts.hint }));
  if (opts.actions?.length) {
    const row = h('div', { class: 'empty-actions' });
    for (const action of opts.actions) {
      row.append(
        h('button', {
          type: 'button',
          class: `btn ${action.primary ? 'primary' : 'secondary'}`,
          onclick: action.onClick,
        }, [action.icon ? icon(action.icon) : null, h('span', { text: action.label })]),
      );
    }
    children.push(row);
  }
  return h('div', { class: 'empty' }, children);
}

/** 页面头。title / sub / actions 节点数组 */
export function pageHeader({ title, sub, actions } = {}) {
  return h('header', { class: 'page-header' }, [
    h('div', {}, [
      h('h1', { text: title || '' }),
      sub ? h('div', { class: 'page-sub', text: sub }) : null,
    ]),
    actions?.length ? h('div', { class: 'page-header-actions' }, actions) : null,
  ]);
}

/** 简易占位页（B–G 阶段替换为真实实现）。 */
export function createPlaceholderView({ key, title, sub, icon: iconName, desc, hint }) {
  const body = h('div', { class: 'page-body' });
  const root = h('div', { class: 'page placeholder-page', dataset: { view: key } }, [
    pageHeader({ title, sub }),
    body,
  ]);

  function render() {
    body.replaceChildren(
      emptyState({
        icon: iconName,
        title: '此页面开发中',
        desc: desc || `「${title}」将在后续阶段接入。当前应用壳与设计 token 已就绪。`,
        hint: hint || `route: ${key}`,
      }),
    );
  }

  render();

  return {
    el: root,
    async onActivate() {},
    async onLibraryChange() {},
  };
}
