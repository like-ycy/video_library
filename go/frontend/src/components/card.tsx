import { Button, Card, Progress } from "xwang-ui";
import type { Video } from "../types";
import {
  formatDuration,
  formatTimecode,
  formatRelativeTime,
} from "../format.js";
import { Icon, StatusBadge } from "./shell";

export function VideoCard({
  item,
  kind = "movies",
  onOpen,
  onClear,
}: {
  item: Video;
  kind?: string;
  onOpen: (item: Video, play?: boolean) => void;
  onClear?: () => void;
}) {
  const progress =
    item.durationMs > 0
      ? Math.min(100, (item.watchPositionMs / item.durationMs) * 100)
      : 0;
  const poster = item.coverUrl ? (
    <img
      src={item.coverUrl}
      alt={`${item.fanha} 封面`}
      loading="lazy"
      onError={(event) => {
        event.currentTarget.style.visibility = "hidden";
      }}
    />
  ) : (
    <div className="placeholder">
      <Icon name="movie" />
    </div>
  );
  if (kind === "continue")
    return (
      <Card className="continue-card p-0 gap-0 overflow-hidden">
        <Button
          variant="ghost"
          className="continue-thumb h-auto rounded-none p-0"
          aria-label={`继续播放 ${item.fanha}`}
          onClick={() => onOpen(item, true)}
        >
          {poster}
          <span className="fanha-chip mono">{item.fanha}</span>
          <span className="continue-play">
            <Icon name="play_arrow" />
          </span>
          <Progress
            className="absolute inset-x-0 bottom-0 h-1 rounded-none"
            value={progress}
            aria-label="播放进度"
          />
        </Button>
        <div className="continue-body">
          <div className="continue-title">{item.title || "（无标题）"}</div>
          <div className="continue-meta mono">
            <span>
              {formatTimecode(item.watchPositionMs)} /{" "}
              {formatTimecode(item.durationMs)}
            </span>
            <span>{Math.round(progress)}%</span>
          </div>
          <div className="continue-foot">
            <span>{item.actress}</span>
            <span>{formatRelativeTime(item.lastPlayedAt)}</span>
            {onClear && (
              <Button variant="ghost" size="sm" onClick={onClear}>
                清除进度
              </Button>
            )}
          </div>
        </div>
      </Card>
    );
  if (kind === "recent")
    return (
      <Card className="history-card p-0 gap-0">
        <Button
          variant="ghost"
          className="history-poster h-auto p-0 rounded-none"
          onClick={() => onOpen(item)}
          aria-label={`查看 ${item.fanha}`}
        >
          {poster}
        </Button>
        <div className="history-body">
          <div className="history-top">
            <span className="mono text-primary">{item.fanha}</span>
            <span>{item.actress}</span>
          </div>
          <div className="history-title">{item.title || "（无标题）"}</div>
          <Progress className="my-2" value={progress} aria-label="播放进度" />
          <div className="history-meta mono">
            <span>
              {formatTimecode(item.watchPositionMs)} /{" "}
              {formatDuration(item.durationMs)}
            </span>
            <span>×{item.playCount}</span>
          </div>
        </div>
        <div className="history-actions">
          <Button size="sm" onClick={() => onOpen(item, true)}>
            继续
          </Button>
        </div>
      </Card>
    );
  return (
    <Card className="video-card p-0 gap-0 overflow-hidden">
      <Button
        variant="ghost"
        className="h-auto w-full flex-col gap-0 rounded-none p-0 whitespace-normal text-left"
        onClick={() => onOpen(item)}
        aria-label={`查看 ${item.fanha}`}
      >
        <div className="poster">
          {poster}
          <span className="fanha-chip mono">{item.fanha}</span>
          {item.favorite && (
            <span className="fav-btn">
              <Icon name="favorite" fill />
            </span>
          )}
          <div className="poster-meta">
            <StatusBadge scraped={item.scraped} missing={item.missing} />
            <span>{formatDuration(item.durationMs)}</span>
          </div>
          {progress > 0 && (
            <Progress
              className="absolute inset-x-0 bottom-0 h-1 rounded-none"
              value={progress}
              aria-label="播放进度"
            />
          )}
        </div>
        <div className="card-body w-full">
          <div className="card-actress">{item.actress || "—"}</div>
          <div className="card-title">{item.title || "（无标题）"}</div>
          <div className="card-foot">
            <span>{item.releaseDate || "—"}</span>
            <span>{item.rating ? `★ ${item.rating}` : ""}</span>
          </div>
        </div>
      </Button>
      <Button
        variant="ghost"
        size="sm"
        disabled={!item.playable}
        onClick={() => onOpen(item, true)}
      >
        <Icon name="play_arrow" />
        播放
      </Button>
    </Card>
  );
}
