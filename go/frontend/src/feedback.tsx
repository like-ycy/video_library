import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { Button, Progress } from "xwang-ui";

type Notice = {
  id: number;
  message: string;
  kind: "info" | "ok" | "warn" | "error";
};
const Context = createContext<{
  notify: (message: string, kind?: Notice["kind"]) => void;
  run: <T>(context: string, action: () => Promise<T>) => Promise<T | undefined>;
}>(null!);
export const useFeedback = () => useContext(Context);
export function FeedbackProvider({ children }: { children: ReactNode }) {
  const [notices, setNotices] = useState<Notice[]>([]);
  const [pending, setPending] = useState(0);
  const serial = useRef(0);
  const timers = useRef(new Set<ReturnType<typeof setTimeout>>());
  useEffect(
    () => () => {
      timers.current.forEach(clearTimeout);
    },
    [],
  );
  const notify = useCallback(
    (message: string, kind: Notice["kind"] = "info") => {
      const id = ++serial.current;
      setNotices((items) => [...items.slice(-3), { id, message, kind }]);
      const timer = setTimeout(
        () => {
          setNotices((items) => items.filter((item) => item.id !== id));
          timers.current.delete(timer);
        },
        kind === "error" ? 8000 : 3200,
      );
      timers.current.add(timer);
    },
    [],
  );
  const run = useCallback(
    async <T,>(context: string, action: () => Promise<T>) => {
      setPending((n) => n + 1);
      try {
        return await action();
      } catch (error) {
        console.error(context, error);
        notify(
          `${context}：${error instanceof Error ? error.message : String(error)}`,
          "error",
        );
      } finally {
        setPending((n) => n - 1);
      }
    },
    [notify],
  );
  return (
    <Context.Provider value={{ notify, run }}>
      {children}
      {pending > 0 && (
        <Progress
          aria-label="正在处理"
          className="fixed inset-x-0 bottom-0 z-50 h-1"
          value={null}
        />
      )}
      <div id="toast-host" role="status" aria-live="polite">
        {notices.map((n) => (
          <div className={`toast ${n.kind}`} key={n.id}>
            <span>{n.message}</span>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="关闭提示"
              onClick={() =>
                setNotices((items) => items.filter((item) => item.id !== n.id))
              }
            >
              ×
            </Button>
          </div>
        ))}
      </div>
    </Context.Provider>
  );
}
