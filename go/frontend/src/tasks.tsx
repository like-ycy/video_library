import {
  createContext,
  useContext,
  useEffect,
  useReducer,
  useRef,
  type ReactNode,
} from "react";
import { call, on } from "./api";
import { useFeedback } from "./feedback";
import type { FinishedEvent, ScrapeEvent } from "./types";

export interface TaskResult extends ScrapeEvent {
  status: "running" | "ok" | "failed";
}
export interface TaskState {
  running: boolean;
  libraryId: string;
  total: number;
  results: TaskResult[];
  finished: FinishedEvent | null;
  log: string[];
  indexVersion: number;
  indexing: boolean;
}
export const initialTask: TaskState = {
  running: false,
  libraryId: "",
  total: 0,
  results: [],
  finished: null,
  log: [],
  indexVersion: 0,
  indexing: false,
};
type Action =
  | { type: "start"; libraryId: string; total: number }
  | { type: "item"; payload: ScrapeEvent; status: TaskResult["status"] }
  | { type: "finished"; payload: FinishedEvent }
  | { type: "running"; running: boolean }
  | { type: "log"; log: string[] }
  | { type: "index"; indexing: boolean; updated?: boolean };
export function taskReducer(state: TaskState, action: Action): TaskState {
  switch (action.type) {
    case "start":
      return {
        ...initialTask,
        running: true,
        libraryId: action.libraryId,
        total: action.total,
        indexVersion: state.indexVersion,
      };
    case "running":
      return { ...state, running: action.running };
    case "log":
      return { ...state, log: action.log };
    case "index":
      return {
        ...state,
        indexing: action.indexing,
        indexVersion: state.indexVersion + (action.updated ? 1 : 0),
      };
    case "finished":
      return { ...state, running: false, finished: action.payload };
    case "item": {
      const old = state.results.find(
        (item) => item.fanha === action.payload.fanha,
      );
      if (old && old.status !== "running" && action.status === "running")
        return state;
      const entry = { ...old, ...action.payload, status: action.status };
      return {
        ...state,
        results: old
          ? state.results.map((item) =>
              item.fanha === entry.fanha ? entry : item,
            )
          : [...state.results, entry],
      };
    }
  }
}
const Context = createContext<{
  task: TaskState;
  start: (id: string, stems: string[], force: boolean) => Promise<void>;
  cancel: () => Promise<void>;
  loadLog: () => Promise<void>;
}>(null!);
export const useTasks = () => useContext(Context);
export function TaskProvider({ children }: { children: ReactNode }) {
  const [task, dispatch] = useReducer(taskReducer, initialTask);
  const { notify } = useFeedback();
  const starting = useRef(false);
  useEffect(() => {
    let active = true;
    let eventSeen = false;
    const item = (status: TaskResult["status"]) => (payload: ScrapeEvent) => {
      eventSeen = true;
      dispatch({ type: "item", payload, status });
    };
    const off = [
      on("scrape:progress", item("running")),
      on("scrape:item-done", item("ok")),
      on("scrape:item-failed", item("failed")),
      on("scrape:finished", (payload) => {
        eventSeen = true;
        dispatch({ type: "finished", payload });
        notify(
          payload.fatalMessage ||
            payload.error ||
            `${payload.canceled ? "已取消" : "已完成"}：成功 ${payload.ok}，失败 ${payload.failed}，跳过 ${payload.skipped}`,
          payload.fatalMessage || payload.error ? "error" : "info",
        );
      }),
      on("index:progress", () => dispatch({ type: "index", indexing: true })),
      on("index:done", () => {
        dispatch({ type: "index", indexing: false, updated: true });
        notify("索引已更新", "ok");
      }),
      on("index:failed", (payload) => {
        dispatch({ type: "index", indexing: false });
        notify(`更新索引失败：${payload.message}`, "error");
      }),
    ];
    call("ScrapeStatus")
      .then((running) => {
        if (active && !eventSeen && !starting.current)
          dispatch({ type: "running", running });
      })
      .catch((error) => {
        if (active) notify(`读取任务状态失败：${String(error)}`, "error");
      });
    return () => {
      active = false;
      off.forEach((unsubscribe) => unsubscribe());
    };
  }, [notify]);
  async function start(id: string, stems: string[], force: boolean) {
    if (starting.current || task.running) throw new Error("已有刮削任务运行中");
    if (!stems.length) throw new Error("请先选择条目");
    starting.current = true;
    try {
      const health = await call("ScraperHealth", true);
      if (!health.chrome.found) throw new Error("未找到 Chrome，请先安装");
      dispatch({ type: "start", libraryId: id, total: stems.length });
      await call("StartScrape", id, stems, force);
      notify(`已开始刮削 ${stems.length} 条`);
    } catch (error) {
      dispatch({ type: "running", running: false });
      throw error;
    } finally {
      starting.current = false;
    }
  }
  async function cancel() {
    notify(
      (await call("CancelScrape")) ? "正在终止刮削器…" : "当前没有运行中的刮削",
    );
  }
  async function loadLog() {
    dispatch({ type: "log", log: (await call("GetScrapeLog")) ?? [] });
  }
  return (
    <Context.Provider value={{ task, start, cancel, loadLog }}>
      {children}
    </Context.Provider>
  );
}
