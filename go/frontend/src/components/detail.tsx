import { flushSync } from "react-dom";
import { useLayoutEffect, useRef, useState, type RefObject } from "react";
import {
  Badge,
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "xwang-ui";
import { call } from "../api";
import { useFeedback } from "../feedback";
import {
  formatDate,
  formatDuration,
  formatPosition,
  formatSize,
  joinOrDash,
} from "../format.js";
import type { Video } from "../types";
import { Icon } from "./shell";

const SAVE_INTERVAL_MS = 10_000;
function Player({
  item,
  libraryId,
  queue,
  clearing,
}: {
  item: Video;
  libraryId: string;
  queue: RefObject<Promise<void>>;
  clearing: RefObject<boolean>;
}) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const { run, notify } = useFeedback();
  const [failed, setFailed] = useState(false);
  const lastSaved = useRef(0);
  const failedRef = useRef(false);
  const ended = useRef(false);
  const ready = useRef(false);
  function save(position: number) {
    // 顺序写入，避免较慢的旧进度覆盖 ended 的归零。
    queue.current = queue.current
      .then(() => call("SaveProgress", libraryId, item.id, position))
      .catch((error) => notify(`保存播放进度失败：${String(error)}`, "error"));
  }
  useLayoutEffect(() => {
    const video = videoRef.current!;
    void run("记录播放失败", () => call("PlayEmbedded", libraryId, item.id));
    return () => {
      if (
        ready.current &&
        !failedRef.current &&
        !ended.current &&
        !clearing.current
      )
        save(Math.round(video.currentTime * 1000));
      video.pause();
      video.removeAttribute("src");
      video.load();
    };
  }, [item.id, libraryId]);
  return (
    <div className="player">
      <video
        ref={videoRef}
        src={item.videoUrl}
        controls
        autoPlay
        playsInline
        onLoadedMetadata={(event) => {
          ready.current = true;
          const video = event.currentTarget;
          if (
            item.watchPositionMs > 0 &&
            item.watchPositionMs / 1000 < video.duration - 5
          )
            video.currentTime = item.watchPositionMs / 1000;
        }}
        onError={() => {
          failedRef.current = true;
          setFailed(true);
        }}
        onTimeUpdate={(event) => {
          if (
            !failedRef.current &&
            !ended.current &&
            ready.current &&
            !clearing.current &&
            performance.now() - lastSaved.current >= SAVE_INTERVAL_MS
          ) {
            lastSaved.current = performance.now();
            save(Math.round(event.currentTarget.currentTime * 1000));
          }
        }}
        onPause={(event) => {
          if (
            ready.current &&
            !clearing.current &&
            !failedRef.current &&
            !ended.current &&
            !event.currentTarget.ended
          )
            save(Math.round(event.currentTarget.currentTime * 1000));
        }}
        onEnded={() => {
          ended.current = true;
          save(0);
        }}
      />
      {failed && (
        <div className="p-3 text-warning">
          内嵌播放失败，HEVC / 10bit 建议使用外部播放器。
          <Button
            variant="secondary"
            onClick={() =>
              void run("打开外部播放器失败", () =>
                call("OpenInPlayer", libraryId, item.id, true),
              )
            }
          >
            外部播放器打开
          </Button>
        </div>
      )}
    </div>
  );
}
export function Detail({
  item: initialItem,
  libraryId,
  autoplay,
  onClose,
  onChanged,
}: {
  item: Video;
  libraryId: string;
  autoplay: boolean;
  onClose: () => void;
  onChanged: () => void;
}) {
  const [item, setItem] = useState(initialItem);
  const queue = useRef(Promise.resolve());
  const clearing = useRef(false);
  const [playing, setPlaying] = useState(autoplay);
  const [shot, setShot] = useState<number | null>(null);
  const [saving, setSaving] = useState(false);
  const { run } = useFeedback();
  const shots = item.shotUrls ?? [];
  const rows = [
    ["番号", item.fanha],
    ["演员", item.actress || joinOrDash(item.cast)],
    ["发布时间", formatDate(item.releaseDate)],
    ["真实时长", formatDuration(item.durationMs)],
    ["站点时长", item.siteLengthMin ? `${item.siteLengthMin} 分钟` : "—"],
    ["分辨率", item.width ? `${item.width}×${item.height}` : "—"],
    ["编码", [item.vcodec, item.acodec].filter(Boolean).join(" / ")],
    ["类别", joinOrDash(item.genres)],
    ["文件名", item.stem],
    ["文件大小", formatSize(item.fileSize)],
    ["刮削时间", item.scrapedAt],
    ["播放次数", item.playCount],
  ];
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) {
          flushSync(() => setPlaying(false));
          void queue.current.then(onClose);
        }
      }}
    >
      <DialogContent className="w-[92vw] max-w-[1100px] max-h-[90vh] overflow-y-auto">
        <DialogTitle>{item.title || item.fanha}</DialogTitle>
        <DialogDescription>
          {item.fanha} · {item.actress}
        </DialogDescription>
        {item.playable && item.videoUrl ? (
          playing ? (
            <Player
              key={item.watchPositionMs === 0 ? "start" : "resume"}
              item={item}
              libraryId={libraryId}
              queue={queue}
              clearing={clearing}
            />
          ) : (
            <Button
              variant="ghost"
              className="relative h-auto w-full overflow-hidden p-0"
              aria-label="内嵌播放"
              onClick={() => {
                clearing.current = false;
                setPlaying(true);
              }}
            >
              {item.coverUrl && (
                <img
                  className="detail-cover"
                  src={item.coverUrl}
                  alt={`${item.fanha} 封面`}
                />
              )}
              <span className="play-mask">
                <Icon name="play_arrow" />
              </span>
            </Button>
          )
        ) : (
          item.coverUrl && (
            <img className="detail-cover" src={item.coverUrl} alt="封面" />
          )
        )}
        <div className="detail-actions">
          <Button
            disabled={saving}
            variant={item.favorite ? "default" : "secondary"}
            onClick={() => {
              setSaving(true);
              void run("切换收藏失败", async () => {
                const favorite = await call(
                  "ToggleFavorite",
                  libraryId,
                  item.id,
                );
                setItem((value) => ({ ...value, favorite }));
                onChanged();
              }).finally(() => setSaving(false));
            }}
          >
            <Icon name="favorite" fill={item.favorite} />
            {item.favorite ? "已收藏" : "收藏"}
          </Button>
          <Button
            variant="secondary"
            disabled={!item.playable}
            onClick={() =>
              void run("打开外部播放器失败", () =>
                call("OpenInPlayer", libraryId, item.id, true),
              )
            }
          >
            外部播放器打开
            {item.watchPositionMs > 0 &&
              `（上次 ${formatPosition(item.watchPositionMs)}）`}
          </Button>
          {item.watchPositionMs > 0 && (
            <Button
              variant="ghost"
              onClick={() => {
                clearing.current = true;
                setPlaying(false);
                void run("清除进度失败", async () => {
                  await queue.current;
                  await call("ClearProgress", libraryId, item.id);
                  setItem((value) => ({ ...value, watchPositionMs: 0 }));
                  onChanged();
                });
              }}
            >
              从头开始
            </Button>
          )}
        </div>
        <div className="flex items-center gap-2" role="group" aria-label="评分">
          评分
          {[1, 2, 3, 4, 5].map((star) => (
            <Button
              key={star}
              size="icon-sm"
              variant="ghost"
              disabled={saving}
              aria-label={`${star} 星`}
              aria-pressed={(item.rating ?? 0) >= star}
              className={
                (item.rating ?? 0) >= star ? "text-warning" : "text-faint"
              }
              onClick={() => {
                setSaving(true);
                void run("保存评分失败", async () => {
                  const rating = item.rating === star ? null : star;
                  await call("SetRating", libraryId, item.id, rating);
                  setItem((value) => ({ ...value, rating }));
                  onChanged();
                }).finally(() => setSaving(false));
              }}
            >
              <Icon name="star" fill={(item.rating ?? 0) >= star} />
            </Button>
          ))}
        </div>
        {item.missing && <Badge variant="danger">文件已不在磁盘上</Badge>}
        {!item.scraped && <Badge>未刮削</Badge>}
        <div className="detail-rows">
          {rows.map(([label, value]) => (
            <div className="detail-row" key={label}>
              {label}：<b>{value || "—"}</b>
            </div>
          ))}
        </div>
        <div className="shots">
          {shots.map((url, index) => (
            <Button
              key={url}
              variant="ghost"
              className="h-auto p-0"
              onClick={() => setShot(index)}
              aria-label={`查看截图 ${index + 1}`}
            >
              <img src={url} alt={`截图 ${index + 1}`} loading="lazy" />
            </Button>
          ))}
        </div>
        {!shots.length && (
          <p className="text-muted">暂无剧照，完成刮削后会显示。</p>
        )}
        <Dialog
          open={shot !== null}
          onOpenChange={(open) => {
            if (!open) setShot(null);
          }}
        >
          <DialogContent className="w-[94vw] max-w-[1200px] max-h-[94vh]">
            <DialogTitle>
              截图 {(shot ?? 0) + 1} / {shots.length}
            </DialogTitle>
            <DialogDescription className="sr-only">
              影片剧照，左右按钮切换
            </DialogDescription>
            {shot !== null && (
              <img
                src={shots[shot]}
                alt={`截图 ${shot + 1}`}
                className="max-h-[75vh] w-full object-contain"
              />
            )}
            <div className="flex justify-between">
              <Button
                variant="secondary"
                onClick={() =>
                  setShot(
                    (index) => ((index ?? 0) - 1 + shots.length) % shots.length,
                  )
                }
              >
                上一张
              </Button>
              <Button
                variant="secondary"
                onClick={() =>
                  setShot((index) => ((index ?? 0) + 1) % shots.length)
                }
              >
                下一张
              </Button>
            </div>
          </DialogContent>
        </Dialog>
      </DialogContent>
    </Dialog>
  );
}
