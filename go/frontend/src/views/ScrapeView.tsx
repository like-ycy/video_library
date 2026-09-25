import { useEffect, useState } from "react";
import { Badge, Button, Checkbox, Label, Switch } from "xwang-ui";
import { call } from "../api";
import { useFeedback } from "../feedback";
import { useTasks } from "../tasks";
import { formatSize } from "../format.js";
import type { Candidate, Health, ScanResult } from "../types";
import { EmptyState, PageHeader, StatusBadge } from "../components/shell";
import { TaskProgress } from "./TaskMonitorView";
/**
 * 缺图时给出悬停提示：程序到底在找哪个目录、缺哪几个文件。
 *
 * 「缺图」两个字本身说明不了任何事 —— 路径拼错、图片被删、这一次刮削没成功，
 * 在界面上长得一模一样。把后端算好的期望路径摊开，用户才能自己动手核对。
 */
function artHint(c: Candidate): string | undefined {
  if (!c.scraped || !c.missingArt) return undefined;
  const files = c.missingFiles ?? [];
  const tail = `图片目录：${c.artDir}`;
  if (files.length) {
    return `缺少 ${files.length} 个文件：\n${files.join("\n")}\n${tail}`;
  }
  return `边车记录里没有任何图片路径\n${tail}`;
}
export function ScrapeView({
  libraryId,
  navigate,
}: {
  libraryId: string;
  navigate: (route: string) => void;
}) {
  const [scan, setScan] = useState<ScanResult>({ candidates: [], issues: [] });
  const [selected, setSelected] = useState<string[]>([]);
  const [onlyUnscraped, setOnlyUnscraped] = useState(true);
  const [force, setForce] = useState(false);
  const [health, setHealth] = useState<Health | null>(null);
  const [healthError, setHealthError] = useState("");
  const [scanError, setScanError] = useState("");
  const [version, setVersion] = useState(0);
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(false);
  const { task, start, cancel } = useTasks();
  const { run, notify } = useFeedback();
  useEffect(() => {
    let active = true;
    if (!libraryId) return;
    setLoading(true);
    setScanError("");
    call("ScanLibrary", libraryId)
      .then((result) => {
        if (!active) return;
        setScan(result);
        const stems = new Set((result.candidates ?? []).map((c) => c.stem));
        setSelected((old) => old.filter((stem) => stems.has(stem)));
      })
      .catch((error) => {
        if (active) setScanError(String(error));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    // 进页面查一次即可：结果有后端 TTL 缓存兜底，不必每次重跑 doctor。
    call("ScraperHealth", false)
      .then((value) => {
        if (active) {
          setHealth(value);
          setHealthError("");
        }
      })
      .catch((error) => {
        if (active) {
          setHealth(null);
          setHealthError(String(error));
        }
      });
    return () => {
      active = false;
    };
  }, [libraryId, version, task.indexVersion]);
  // 候选列表按番号自然序正序展示（IPX-9 排在 IPX-100 前面）。
  const candidates = [...(scan.candidates ?? [])].sort((a, b) =>
    a.fanha.localeCompare(b.fanha, "zh-Hans-CN", { numeric: true }),
  );
  const visible = candidates.filter(
    (c) => !onlyUnscraped || !c.scraped || c.missingArt,
  );
  const locked = busy || loading || task.running || task.indexing;
  async function index(rebuild: boolean) {
    if (
      rebuild &&
      !window.confirm(
        "重建索引会重扫该库，收藏、评分和进度按业务键保留。继续？",
      )
    )
      return;
    setBusy(true);
    await run(rebuild ? "重建索引失败" : "更新索引失败", async () => {
      const stats = await call(
        rebuild ? "RebuildIndex" : "ImportLibrary",
        libraryId,
      );
      notify(
        `索引已更新：${stats.Found} 条，探测 ${stats.Probed} 条，复用缓存 ${stats.MediaCached} 条，缺失 ${stats.Vanished} 条`,
        "ok",
      );
      setVersion((n) => n + 1);
    });
    setBusy(false);
  }
  return (
    <div className="page">
      <PageHeader title="扫描与待处理" sub="扫描磁盘候选，勾选后开始刮削">
        <Button size="sm" variant="secondary" onClick={() => navigate("tasks")}>
          任务监控
        </Button>
      </PageHeader>
      {!libraryId ? (
        <EmptyState title="没有视频库可扫描">
          <Button onClick={() => navigate("settings-library")}>
            添加视频库
          </Button>
        </EmptyState>
      ) : (
        <div className="scrape-main">
          <div className="scrape-bar">
            <Button disabled={locked} onClick={() => setVersion((n) => n + 1)}>
              扫描
            </Button>
            <Button
              variant="secondary"
              disabled={locked}
              onClick={() => void index(false)}
            >
              更新索引
            </Button>
            <Button
              variant="secondary"
              disabled={locked}
              onClick={() => void index(true)}
            >
              重建索引
            </Button>
            <Label className="flex items-center gap-2">
              <Checkbox
                checked={onlyUnscraped}
                onCheckedChange={(v) => setOnlyUnscraped(v === true)}
              />
              仅显示未刮削/缺图
            </Label>
            <Label className="flex items-center gap-2">
              <Switch
                checked={force}
                onCheckedChange={setForce}
                disabled={locked}
              />
              重新下载图片
            </Label>
            <Button
              variant="secondary"
              size="sm"
              disabled={locked}
              onClick={() =>
                setSelected((old) => [
                  ...new Set([...old, ...visible.map((c) => c.stem)]),
                ])
              }
            >
              全选可见
            </Button>
            <Button
              variant="ghost"
              size="sm"
              disabled={locked}
              onClick={() => setSelected([])}
            >
              清空选择
            </Button>
            <Button
              disabled={locked || !selected.length}
              onClick={() => {
                setBusy(true);
                void run("启动刮削失败", () =>
                  start(libraryId, selected, force),
                ).finally(() => setBusy(false));
              }}
            >
              开始刮削（{selected.length}）
            </Button>
            <Button
              variant="danger"
              disabled={!task.running}
              onClick={() => void run("取消刮削失败", cancel)}
            >
              取消
            </Button>
          </div>
          <div className="px-4 py-2">
            {healthError ? (
              <Badge variant="danger" title={healthError}>
                刮削器不可用：{healthError}
              </Badge>
            ) : health && !health.chrome.found ? (
              <Badge variant="danger">缺少 Chrome</Badge>
            ) : (
              health &&
              !health.driver.ready && (
                <Badge
                  variant={health.driver.writable ? "warning" : "danger"}
                  title={health.driver.dir}
                >
                  {health.driver.writable
                    ? "驱动将在首次刮削时下载"
                    : "驱动目录不可写"}
                </Badge>
              )
            )}
            {scanError && (
              <p role="alert" className="text-danger">
                扫描失败：{scanError}
              </p>
            )}
          </div>
          <div className="scrape-body">
            <div className="table-wrap">
              <table className="candidates">
                <thead>
                  <tr>
                    <th>选择</th>
                    <th>演员</th>
                    <th>番号</th>
                    <th>文件</th>
                    <th>大小</th>
                    <th>状态</th>
                  </tr>
                </thead>
                <tbody>
                  {visible.map((c) => (
                    <tr key={`${c.actress}/${c.stem}`}>
                      <td>
                        <Checkbox
                          aria-label={`选择 ${c.fanha} ${c.actress}`}
                          disabled={locked}
                          checked={selected.includes(c.stem)}
                          onCheckedChange={(value) =>
                            setSelected((old) =>
                              value === true
                                ? [...new Set([...old, c.stem])]
                                : old.filter((stem) => stem !== c.stem),
                            )
                          }
                        />
                      </td>
                      <td>{c.actress}</td>
                      <td>{c.fanha}</td>
                      <td>{c.videoFile}</td>
                      <td>{formatSize(c.fileSize)}</td>
                      <td>
                        <span title={artHint(c)}>
                          <StatusBadge
                            scraped={c.scraped}
                            missingArt={c.missingArt}
                          />
                        </span>
                      </td>
                    </tr>
                  ))}
                  {!visible.length && (
                    <tr>
                      <td colSpan={6} className="empty-cell">
                        {loading
                          ? "扫描中…"
                          : candidates.length
                            ? "所有条目均已刮削，取消筛选可查看。"
                            : "没有扫描到视频文件。"}
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
              {(scan.issues ?? []).map((issue, i) => (
                <div key={i} className="item failed">
                  {issue.actress} · {issue.subject}：{issue.message}
                </div>
              ))}
            </div>
            <div className="progress-panel">
              <TaskProgress />
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
