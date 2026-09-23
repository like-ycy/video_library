import type { Backend, Events } from "./types";

// 直接使用 Wails 注入的 API，构建不依赖本机生成的 wailsjs 文件。
export function call<K extends keyof Backend>(
  method: K,
  ...args: Parameters<Backend[K]>
): ReturnType<Backend[K]> {
  const app = window.go?.main.App;
  if (!app)
    return Promise.reject(
      new Error(`后端不可用（${method}），请使用 Wails 启动。`),
    ) as ReturnType<Backend[K]>;
  const fn = app[method] as (
    ...values: Parameters<Backend[K]>
  ) => ReturnType<Backend[K]>;
  if (typeof fn !== "function")
    return Promise.reject(new Error(`后端没有方法 ${method}`)) as ReturnType<
      Backend[K]
    >;
  return fn(...args);
}

export function on<K extends keyof Events>(
  event: K,
  handler: (payload: Events[K]) => void,
) {
  // 浏览器预览没有 Wails 事件源；真实请求仍明确报错。
  return window.runtime?.EventsOn(event, handler) ?? (() => {});
}
