import { useEffect, useRef, useState } from "react";
import {
  Button,
  Input,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "xwang-ui";
import { call } from "../api";
import { useFeedback } from "../feedback";
import { useTasks } from "../tasks";
import type { Library, Video } from "../types";
import { EmptyState, PageHeader, RefreshButton } from "../components/shell";
import { VideoCard } from "../components/card";

const PAGE_SIZE = 120;
export function WatchView({
  library,
  mode,
  keyword,
  onOpen,
  navigate,
  addLibrary,
  refreshToken,
}: {
  library?: Library;
  mode: string;
  keyword: string;
  onOpen: (item: Video, play?: boolean) => void;
  navigate: (route: string) => void;
  addLibrary: () => void;
  refreshToken: number;
}) {
  const [actors, setActors] = useState<
    { actress: string; total: number; missing: number }[]
  >([]);
  const [genres, setGenres] = useState<string[]>([]);
  const [actor, setActor] = useState("");
  const [actorSearch, setActorSearch] = useState("");
  const [search, setSearch] = useState(keyword);
  const [query, setQuery] = useState(keyword);
  const [selectedGenres, setSelectedGenres] = useState<string[]>([]);
  const [favorite, setFavorite] = useState(false);
  // 番号正序是全部影片与演员视频的默认排序。
  const [sort, setSort] = useState("fanha");
  const [desc, setDesc] = useState(false);
  const [showActors, setShowActors] = useState(mode === "actresses");
  const [page, setPage] = useState(1);
  const [items, setItems] = useState<Video[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [version, setVersion] = useState(0);
  const { task } = useTasks();
  const { notify } = useFeedback();
  const request = useRef(0);
  // 演员页选中具体演员后固定按番号正序，排序控件一并停用。
  const forceFanha = mode === "actresses" && actor !== "";
  const effSort = forceFanha ? "fanha" : sort;
  const effDesc = forceFanha ? false : desc;
  // 演员名按名称正序（中文按拼音，数字段按数值）。
  const sortedActors = [...actors].sort((a, b) =>
    a.actress.localeCompare(b.actress, "zh-Hans-CN", { numeric: true }),
  );
  useEffect(() => {
    setSearch(keyword);
    setQuery(keyword);
    setPage(1);
  }, [keyword]);
  useEffect(() => {
    const timer = setTimeout(() => {
      setQuery(search.trim());
      setPage(1);
    }, 250);
    return () => clearTimeout(timer);
  }, [search]);
  useEffect(() => {
    let active = true;
    if (!library?.available) return;
    Promise.all([
      call("ListActresses", library.id),
      call("ListGenres", library.id),
    ])
      .then(([a, g]) => {
        if (active) {
          setActors(a ?? []);
          setGenres(g ?? []);
        }
      })
      .catch((error) => {
        if (active) notify(`读取筛选项失败：${String(error)}`, "error");
      });
    return () => {
      active = false;
    };
  }, [
    library?.id,
    library?.available,
    version,
    refreshToken,
    task.indexVersion,
    notify,
  ]);
  useEffect(() => {
    const ticket = ++request.current;
    let active = true;
    if (!library?.available) return;
    setLoading(true);
    setError("");
    call(
      "QueryVideos",
      library.id,
      {
        Actress: mode === "actresses" ? actor : "",
        Keyword: query,
        Genres: selectedGenres,
        FavoriteOnly: favorite,
        IncludeMissing: false,
        MinDurationMs: 0,
      },
      { Field: effSort, Desc: effDesc },
      page,
      PAGE_SIZE,
    )
      .then((result) => {
        if (active && ticket === request.current) {
          setItems((old) =>
            page === 1
              ? result.items
              : [
                  ...new Map(
                    [...old, ...result.items].map((item) => [item.id, item]),
                  ).values(),
                ],
          );
          setTotal(result.total);
        }
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
  }, [
    library?.id,
    library?.available,
    mode,
    actor,
    query,
    selectedGenres,
    favorite,
    effSort,
    effDesc,
    page,
    version,
    refreshToken,
    task.indexVersion,
  ]);
  const title = mode === "actresses" ? "演员" : "全部影片";
  if (!library)
    return (
      <div className="page">
        <PageHeader title={title} />
        <EmptyState
          title="还没有添加视频库"
          desc="添加一个目录，结构为 演员名/视频文件.mp4"
        >
          <Button onClick={addLibrary}>添加视频库</Button>
        </EmptyState>
      </div>
    );
  if (!library.available)
    return (
      <div className="page">
        <PageHeader title={title} />
        <EmptyState title="视频库不可用" desc={library.root}>
          <Button onClick={() => navigate("settings-library")}>
            视频库管理
          </Button>
        </EmptyState>
      </div>
    );
  return (
    <div className="page">
      <PageHeader
        title={title}
        sub={`共 ${total} 条 · ${actors.length} 位演员`}
      >
        <RefreshButton
          onClick={() => {
            setPage(1);
            setVersion((n) => n + 1);
          }}
          disabled={loading}
        />
        {mode === "actresses" && (
          <Button
            variant="secondary"
            size="sm"
            onClick={() => setShowActors((value) => !value)}
          >
            {showActors ? "隐藏演员栏" : "显示演员栏"}
          </Button>
        )}
      </PageHeader>
      <div className={`watch-root${showActors ? "" : " hide-actors"}`}>
        <aside className="actor-panel">
          <div className="actor-panel-head">
            <Input
              aria-label="搜索演员"
              placeholder="搜索演员…"
              value={actorSearch}
              onChange={(e) => setActorSearch(e.target.value)}
            />
          </div>
          <div className="actor-list">
            <Button
              variant={actor === "" ? "default" : "ghost"}
              className="w-full justify-between"
              onClick={() => {
                setActor("");
                setPage(1);
              }}
            >
              全部演员<span>{actors.length}位</span>
            </Button>
            {sortedActors
              .filter((a) =>
                a.actress.toLowerCase().includes(actorSearch.toLowerCase()),
              )
              .map((a) => (
                <Button
                  key={a.actress}
                  variant={actor === a.actress ? "default" : "ghost"}
                  className="w-full justify-between"
                  title={a.missing ? `缺失 ${a.missing} 个文件` : a.actress}
                  onClick={() => {
                    setActor(a.actress);
                    setPage(1);
                  }}
                >
                  <span className="truncate">{a.actress}</span>
                  <span>{a.total}部</span>
                </Button>
              ))}
          </div>
        </aside>
        <div className="watch-main">
          <div className="filters">
            <Input
              className="w-[220px]"
              aria-label="搜索番号或标题"
              placeholder="搜索番号或标题"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
            {genres.slice(0, 20).map((g) => (
              <Button
                key={g}
                size="sm"
                variant={selectedGenres.includes(g) ? "default" : "secondary"}
                onClick={() => {
                  setSelectedGenres((old) =>
                    old.includes(g) ? old.filter((v) => v !== g) : [...old, g],
                  );
                  setPage(1);
                }}
              >
                {g}
              </Button>
            ))}
            <span className="spacer" />
            <Button
              size="sm"
              variant={favorite ? "default" : "secondary"}
              onClick={() => {
                setFavorite((v) => !v);
                setPage(1);
              }}
            >
              仅收藏
            </Button>
            <Select
              value={effSort}
              disabled={forceFanha}
              onValueChange={(value) => {
                setSort(value);
                setPage(1);
              }}
            >
              <SelectTrigger
                aria-label="排序"
                title={forceFanha ? "已选择演员，固定按番号正序" : undefined}
                className="w-[150px]"
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {Object.entries({
                  release_date: "发布时间",
                  file_size: "文件大小",
                  duration_ms: "真实时长",
                  title: "标题",
                  fanha: "番号",
                  actress: "演员",
                  scraped_at: "刮削时间",
                }).map(([value, label]) => (
                  <SelectItem key={value} value={value}>
                    {label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button
              variant="secondary"
              size="sm"
              disabled={forceFanha}
              title={forceFanha ? "已选择演员，固定按番号正序" : undefined}
              onClick={() => {
                setDesc((v) => !v);
                setPage(1);
              }}
            >
              {desc ? "降序" : "升序"}
            </Button>
          </div>
          {error && (
            <p role="alert" className="p-4 text-danger">
              查询失败：{error}
            </p>
          )}
          <div className="video-grid">
            {items.map((item) => (
              <VideoCard key={item.id} item={item} onOpen={onOpen} />
            ))}
            {!loading && !error && !items.length && (
              <EmptyState
                title="没有符合条件的视频"
                desc="放宽筛选条件，或扫描视频库建立索引。"
              >
                <Button onClick={() => navigate("scan")}>去扫描入库</Button>
              </EmptyState>
            )}
          </div>
          <div className="load-more">
            {loading
              ? "加载中…"
              : items.length < total && (
                  <Button
                    variant="secondary"
                    onClick={() => setPage((n) => n + 1)}
                  >
                    加载更多（{items.length} / {total}）
                  </Button>
                )}
          </div>
        </div>
      </div>
    </div>
  );
}
