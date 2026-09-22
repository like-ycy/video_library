// 通用 UI 工具。

/**
 * 创建元素。
 *
 * 文本一律走 textContent，绝不拼 innerHTML：标题、类别这些字段来自被刮削的
 * 站点，属于外部输入。用 innerHTML 拼接等于把站点内容当代码执行 ——
 * 这不是理论风险，站点标题里出现尖括号是完全正常的事。
 */
export function h(tag, props = {}, children = []) {
  const node = document.createElement(tag);

  for (const [key, value] of Object.entries(props)) {
    if (value === undefined || value === null || value === false) continue;
    if (key === 'class') node.className = value;
    else if (key === 'text') node.textContent = value;
    else if (key.startsWith('on')) node.addEventListener(key.slice(2).toLowerCase(), value);
    else if (key === 'dataset') Object.assign(node.dataset, value);
    else if (key === 'checked' || key === 'disabled' || key === 'selected') node[key] = Boolean(value);
    else node.setAttribute(key, value);
  }

  for (const child of [].concat(children)) {
    if (child === null || child === undefined || child === false) continue;
    node.append(child instanceof Node ? child : document.createTextNode(String(child)));
  }
  return node;
}

export function clear(node) {
  while (node.firstChild) node.removeChild(node.firstChild);
}

const TOAST_ICONS = {
  info: 'info',
  error: 'error',
  warn: 'warning',
  ok: 'check_circle',
};

/** 多条 toast 叠加显示；error 停留更久。 */
export function toast(message, kind = 'info') {
  const host = document.getElementById('toast-host');
  if (!host) return;

  const iconName = TOAST_ICONS[kind] || TOAST_ICONS.info;
  const el = h('div', { class: `toast ${kind}` }, [
    h('span', { class: 'ms', text: iconName }),
    h('span', { text: message }),
  ]);

  // 最多同时 4 条
  while (host.children.length >= 4) host.firstChild.remove();

  host.append(el);
  const ms = kind === 'error' ? 8000 : 3200;
  window.setTimeout(() => {
    el.remove();
  }, ms);
}

/**
 * 统一上报错误。
 *
 * Wails 把 Go 的 error 转成 Promise 拒绝，值可能不是 Error 实例（有时是字符串），
 * 所以这里两种形状都要处理，否则用户只看到 "undefined"。
 */
export function reportError(context, error) {
  const message = error?.message ?? String(error ?? '未知错误');
  console.error(context, error);
  toast(`${context}：${message}`, 'error');
}

/** 设置侧栏状态文字（兼容旧 API）。空字符串表示清除。 */
export function setStatus(text) {
  const el = document.getElementById('status');
  if (el) el.textContent = text ?? '';
}

/** 全局忙碌条：长任务期间打开。 */
export function setBusy(on) {
  const bar = document.getElementById('busy-bar');
  if (!bar) return;
  bar.classList.toggle('on', Boolean(on));
}
