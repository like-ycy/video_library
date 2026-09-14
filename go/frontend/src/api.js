// 与 Go 后端通信的唯一入口。
//
// 直接用 Wails 注入的 window.go / window.runtime，而不是 frontend/wailsjs/ 下的
// 生成绑定：那些文件要先跑一次 `wails dev` 才会存在，而本项目刻意不做前端构建
// 步骤 —— 依赖运行时注入的全局对象，clone 下来就能跑，也不用维护 npm 依赖。

function backend() {
  return globalThis.go?.main?.App ?? null;
}

/**
 * 调用后端方法。
 *
 * 后端方法返回 (值, error)，Wails 会把 error 转成 Promise 拒绝，
 * 因此这里不需要额外判断返回值形状。
 */
export async function call(method, ...args) {
  const app = backend();
  if (!app) {
    throw new Error(
      `后端不可用（${method}）。如果你是在浏览器里直接打开了 index.html，请改用 wails dev 启动。`,
    );
  }
  const fn = app[method];
  if (typeof fn !== 'function') {
    throw new Error(`后端没有方法 ${method}`);
  }
  return fn(...args);
}

/**
 * 订阅后端事件，返回取消订阅函数。
 *
 * 后端不可用时返回空函数而不是抛错：事件订阅发生在初始化早期，
 * 这里抛错会让整个界面起不来，而实际上没有事件也能用。
 */
export function on(event, handler) {
  if (typeof globalThis.runtime?.EventsOn !== 'function') {
    return () => {};
  }
  const off = globalThis.runtime.EventsOn(event, handler);
  return typeof off === 'function' ? off : () => {};
}
