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

let toastTimer = null;

export function toast(message, kind = 'info') {
  const el = document.getElementById('toast');
  if (!el) return;
  el.textContent = message;
  el.classList.remove('hidden');
  el.classList.toggle('error', kind === 'error');
  window.clearTimeout(toastTimer);
  toastTimer = window.setTimeout(
    () => el.classList.add('hidden'),
    kind === 'error' ? 8000 : 3000,
  );
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

/** 设置顶栏状态文字。空字符串表示清除。 */
export function setStatus(text) {
  const el = document.getElementById('status');
  if (el) el.textContent = text ?? '';
}
