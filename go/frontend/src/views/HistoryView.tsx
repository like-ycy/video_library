import { useEffect, useState } from "react";
import { Button } from "xwang-ui";
import { call } from "../api";
import { useFeedback } from "../feedback";
import type { Video } from "../types";
import { EmptyState, PageHeader, RefreshButton } from "../components/shell";
import { VideoCard } from "../components/card";
export function HistoryView({
  libraryId,
  kind,
  onOpen,
  onChanged,
  refreshToken,
}: {
  libraryId: string;
  kind: "continue" | "recent";
  onOpen: (item: Video, play?: boolean) => void;
  onChanged: () => void;
  refreshToken: number;
}) {
  const [items, setItems] = useState<Video[]>([]);
  const [version, setVersion] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const { run, notify } = useFeedback();
  useEffect(() => {
    let active = true;
    if (!libraryId) return;
    setLoading(true);
    setError("");
    call(
      kind === "continue" ? "ListContinueWatching" : "ListRecentPlays",
      libraryId,
    )
      .then((rows) => {
        if (active) setItems(rows ?? []);
      })
      .catch((error) => {
        if (active) setError(String(error));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [libraryId, kind, version, refreshToken]);
  return (
    <div className="page">
      <PageHeader
        title={kind === "continue" ? "继续观看" : "最近播放"}
        sub={
          kind === "continue" ? "有播放进度且尚未看完" : "按最近播放时间倒序"
        }
      >
        <RefreshButton
          onClick={() => setVersion((n) => n + 1)}
          disabled={loading}
        />
        {kind === "recent" && (
          <Button
            size="sm"
            variant="danger"
            disabled={!libraryId}
            onClick={() => {
              if (
                window.confirm("清空该库的最近播放记录？收藏与评分不受影响。")
              )
                void run("清空记录失败", async () => {
                  await call("ClearRecentHistory", libraryId);
                  setVersion((n) => n + 1);
                  onChanged();
                  notify("已清空最近播放");
                });
            }}
          >
            清空记录
          </Button>
        )}
      </PageHeader>
      <div className="page-body">
        {error && (
          <p role="alert" className="text-danger">
            {error}
          </p>
        )}
        {loading ? (
          "加载中…"
        ) : (
          <div
            className={kind === "continue" ? "continue-row" : "history-grid"}
          >
            {items.map((item) => (
              <VideoCard
                key={item.id}
                item={item}
                kind={kind}
                onOpen={onOpen}
                onClear={
                  kind === "continue"
                    ? () =>
                        void run("清除进度失败", async () => {
                          await call("ClearProgress", libraryId, item.id);
                          setVersion((n) => n + 1);
                          onChanged();
                        })
                    : undefined
                }
              />
            ))}
            {!items.length && !error && (
              <EmptyState
                title={libraryId ? "暂无播放记录" : "还没有视频库"}
                desc="添加视频库并播放后，记录会显示在这里。"
              />
            )}
          </div>
        )}
      </div>
    </div>
  );
}
