import { useCallback, useEffect, useRef, useState } from "react";
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  Input,
  Label,
  Switch,
} from "xwang-ui";
import { call } from "../api";
import { useFeedback } from "../feedback";
import { getThemePreference, setThemePreference } from "../theme.js";
import type {
  Component,
  Config,
  EnvReport,
  Health,
  Library,
  Paths,
} from "../types";
import { PageHeader } from "../components/shell";
import { ComponentCard } from "../components/componentInstaller";

const TITLES: Record<string, string> = {
  library: "视频库管理",
  scraper: "刮削器设置",
  player: "播放器设置",
  env: "环境诊断",
  appearance: "外观与配置文件",
  about: "关于与更新",
};

// 与后端 scraper.SiteOptions 对齐的展示信息。这里的 name 是 Python `--site`
// 的取值，写错就是整批任务失败，所以前端只负责展示与回传，合法性由后端校验。
const SITE_OPTIONS: { name: string; label: string; note: string }[] = [
  {
    name: "javlibrary",
    label: "JavLibrary",
    note: "收录最全，但每条 job 都要过 Cloudflare 验证。",
  },
  {
    name: "javranking",
    label: "JavRanking",
    note: "无反爬、直接读索引，速度快但收录较少。",
  },
];
export function SettingsView({
  section,
  libraries,
  addLibrary,
  refreshLibraries,
  selectLibrary,
}: {
  section: string;
  libraries: Library[];
  addLibrary: () => void;
  refreshLibraries: () => Promise<void>;
  selectLibrary: (id: string) => void;
}) {
  const [config, setConfig] = useState<Config | null>(null);
  const [components, setComponents] = useState<Component[]>([]);
  const [paths, setPaths] = useState<Paths | null>(null);
  const [health, setHealth] = useState<Health | null>(null);
  const [healthError, setHealthError] = useState("");
  const [report, setReport] = useState<EnvReport | null>(null);
  const [loadError, setLoadError] = useState("");
  const [saving, setSaving] = useState(false);
  const [version, setVersion] = useState(0);
  const [appVersion, setAppVersion] = useState("");
  // 待确认的破坏性操作。用 Dialog 而不是 window.confirm：无边框窗口里
  // 原生 confirm 可能不弹出，表现为点击无反应。
  const [pendingRemove, setPendingRemove] = useState<Library | null>(null);
  const [pendingRebuild, setPendingRebuild] = useState<Library | null>(null);
  // 本次请求是否强制绕过后端的 doctor 结果缓存。用 ref 而不是 state：
  // 置位本身不该触发 effect，只对「点击按钮后这一次」生效。
  const forceRef = useRef(false);
  const [preference, setPreference] = useState(getThemePreference());
  const { run, notify } = useFeedback();
  useEffect(() => {
    let active = true;
    const force = forceRef.current;
    forceRef.current = false;
    setLoadError("");
    if (section === "about")
      call("GetAppVersion")
        .then((value) => {
          if (active) setAppVersion(value);
        })
        .catch(() => undefined);
    if (["scraper", "player", "appearance"].includes(section))
      call("GetConfig")
        .then((value) => {
          if (active) setConfig(value);
        })
        .catch((error) => {
          if (active) setLoadError(String(error));
        });
    if (["appearance", "env"].includes(section))
      call("Paths")
        .then((value) => {
          if (active) setPaths(value);
        })
        .catch((error) => {
          if (active) setLoadError(String(error));
        });
    if (section === "scraper")
      call("ListComponents")
        .then((value) => {
          if (active) setComponents(value ?? []);
        })
        .catch(() => undefined);
    if (section === "scraper")
      call("ScraperHealth", force)
        .then((value) => {
          if (active) {
            setHealth(value);
            setHealthError("");
          }
        })
        .catch((error) => {
          if (active) setHealthError(String(error));
        });
    if (section === "env")
      call("DiagnoseEnv", force)
        .then((value) => {
          if (active) setReport(value);
        })
        .catch((error) => {
          if (active) setLoadError(String(error));
        });
    const changed = () => setPreference(getThemePreference());
    window.addEventListener("cinevault:theme-changed", changed);
    return () => {
      active = false;
      window.removeEventListener("cinevault:theme-changed", changed);
    };
  }, [section, version]);
  // 组件状态变化后同步刷新健康检查：不然同一页上会同时出现
  // 「组件已就绪」和「刮削器不可用」——后者是安装前查到的旧结论。
  const reloadComponents = useCallback(() => {
    call("ListComponents")
      // 后端在组件目录不可用时返回空数组；null 兜底是为了旧版本后端。
      .then((list) => setComponents(list ?? []))
      .catch(() => undefined);
    call("ScraperHealth", true)
      .then((value) => {
        setHealth(value);
        setHealthError("");
      })
      .catch((error) => {
        setHealth(null);
        setHealthError(String(error));
      });
  }, []);
  function update<K extends keyof Config>(key: K, value: Config[K]) {
    setConfig((old) => old && { ...old, [key]: value });
  }
  async function save(patch: Partial<Config>) {
    setSaving(true);
    await run("保存设置失败", async () => {
      // 读取最新配置再合并本页字段，避免覆盖其他设置页或主题的新值。
      const current = await call("GetConfig");
      const saved = await call("SaveConfig", {
        ...current,
        theme: getThemePreference(),
        ...patch,
      });
      setConfig(saved);
      // 组件卡片的「下载入口是否显示」来自后端算好的 onDemand 字段，
      // 不跟着 config 一起变。不重查的话，开关保存后要有一次页面切换才生效。
      reloadComponents();
      notify("设置已保存", "ok");
    });
    setSaving(false);
  }
  // 当前优先站点与「另一个」站点：回退开关的说明要指名道姓，
  // 只说「另一个站点」用户还得自己推。
  const activeSite =
    SITE_OPTIONS.find((option) => option.name === config?.siteOrder) ??
    SITE_OPTIONS[0];
  const fallbackSite =
    SITE_OPTIONS.find((option) => option.name !== activeSite.name) ??
    SITE_OPTIONS[1];
  // 设置页把「最终会传给 Python 的那串值」直接写出来：出问题时日志里的
  // `--site` 与这里逐字一致，不用再反推设置被读成了什么。
  const siteChain = config?.siteFallback
    ? `${activeSite.name},${fallbackSite.name}`
    : activeSite.name;
  const envRows = report
    ? [
        ["平台", report.platform],
        ["Go", report.goVersion],
        ["配置目录", paths?.configDir],
        ["配置文件", paths?.configFile],
        ["ffprobe", report.ffprobeOk ? report.ffprobePath : "未找到（可选）"],
        ["scraper", report.scraperExeOk ? report.scraperExePath : "未找到"],
        ["scraper 来源", report.scraperSource || "—"],
        ["播放器", report.playerPath || "系统默认"],
        [
          "Chrome",
          report.scraperHealth?.chrome.found
            ? report.scraperHealth.chrome.path
            : "未找到",
        ],
        ["协议", report.scraperHealth?.v],
        ["临时目录可写", report.tempWritable ? "是" : "否"],
        ["错误", report.scraperError],
      ]
    : [];
  return (
    <div className="page">
      <PageHeader title={TITLES[section] ?? "设置"} />
      <div className="page-body settings-page space-y-4">
        {loadError && (
          <p role="alert" className="text-danger">
            读取设置失败：{loadError}
            <Button
              variant="secondary"
              onClick={() => setVersion((n) => n + 1)}
            >
              重试
            </Button>
          </p>
        )}
        {section === "library" && (
          <>
            <Button onClick={addLibrary}>添加视频库</Button>
            {libraries.map((lib) => (
              <Card key={lib.id}>
                <CardContent className="pt-4">
                  <div className="lib-row">
                    <div className="lib-main">
                      <Badge variant={lib.available ? "success" : "danger"}>
                        {lib.available ? "可访问" : "不可访问"}
                      </Badge>
                      <span className="mono ml-2">{lib.root}</span>
                      <p className="text-muted">ID：{lib.id}</p>
                    </div>
                    <div className="flex flex-wrap gap-2">
                      <Button
                        size="sm"
                        variant="secondary"
                        disabled={!lib.available}
                        onClick={() =>
                          void run("更新索引失败", async () => {
                            selectLibrary(lib.id);
                            await call("ImportLibrary", lib.id);
                            notify("索引已更新", "ok");
                          })
                        }
                      >
                        更新索引
                      </Button>
                      <Button
                        size="sm"
                        variant="secondary"
                        disabled={!lib.available}
                        onClick={() => setPendingRebuild(lib)}
                      >
                        重建索引
                      </Button>
                      <Button
                        size="sm"
                        variant="danger"
                        onClick={() => setPendingRemove(lib)}
                      >
                        移出
                      </Button>
                    </div>
                  </div>
                </CardContent>
              </Card>
            ))}
            <p className="text-muted">
              移出视频库不会删除磁盘文件，重新添加同一目录即可恢复浏览。
            </p>
            <Dialog
              open={pendingRebuild !== null}
              onOpenChange={(open) => {
                if (!open) setPendingRebuild(null);
              }}
            >
              <DialogContent>
                <DialogTitle>重建索引</DialogTitle>
                <DialogDescription>
                  重建索引会重扫，收藏、评分和进度按业务键保留。继续？
                </DialogDescription>
                <DialogFooter>
                  <Button
                    variant="secondary"
                    onClick={() => setPendingRebuild(null)}
                  >
                    取消
                  </Button>
                  <Button
                    onClick={() => {
                      const target = pendingRebuild;
                      if (!target) return;
                      setPendingRebuild(null);
                      void run("重建失败", async () => {
                        await call("RebuildIndex", target.id);
                        notify("重建完成", "ok");
                      });
                    }}
                  >
                    继续
                  </Button>
                </DialogFooter>
              </DialogContent>
            </Dialog>
            <Dialog
              open={pendingRemove !== null}
              onOpenChange={(open) => {
                if (!open) setPendingRemove(null);
              }}
            >
              <DialogContent>
                <DialogTitle>移出视频库</DialogTitle>
                <DialogDescription>
                  移出视频库不会删除磁盘文件，仅从配置移除。继续？
                  {pendingRemove && (
                    <span className="mono mt-2 block break-all">
                      {pendingRemove.root}
                    </span>
                  )}
                </DialogDescription>
                <DialogFooter>
                  <Button
                    variant="secondary"
                    onClick={() => setPendingRemove(null)}
                  >
                    取消
                  </Button>
                  <Button
                    variant="danger"
                    onClick={() => {
                      const target = pendingRemove;
                      if (!target) return;
                      setPendingRemove(null);
                      void run("移除失败", async () => {
                        await call("RemoveLibrary", target.id);
                        await refreshLibraries();
                        notify("已移出视频库", "ok");
                      });
                    }}
                  >
                    移出
                  </Button>
                </DialogFooter>
              </DialogContent>
            </Dialog>
          </>
        )}
        {section === "scraper" && config && (
          <>
            {components.map((component) => (
              <ComponentCard
                key={component.name}
                component={component}
                onChanged={reloadComponents}
              />
            ))}
            <Card>
              <CardHeader>
                <CardTitle>刮削器配置</CardTitle>
              </CardHeader>
              <CardContent>
                <form
                  className="space-y-4"
                  onSubmit={(event) => {
                    event.preventDefault();
                    void save({
                      scraperPath: config.scraperPath.trim(),
                      concurrency: config.concurrency,
                      scrapeTimeoutMin: config.scrapeTimeoutMin,
                      componentsOnDemand: config.componentsOnDemand,
                      componentMirror: config.componentMirror.trim(),
                      siteOrder: config.siteOrder,
                      siteFallback: config.siteFallback,
                    });
                  }}
                >
                  <Label htmlFor="scraper-path">scraper 路径</Label>
                  <div className="flex gap-2">
                    <Input
                      id="scraper-path"
                      value={config.scraperPath}
                      placeholder="留空自动探测 tools/scraper"
                      onChange={(event) =>
                        update("scraperPath", event.target.value)
                      }
                    />
                    <Button
                      variant="secondary"
                      onClick={() =>
                        void run("选择程序失败", async () => {
                          const path = await call(
                            "PickExecutable",
                            "选择刮削器",
                          );
                          if (path) update("scraperPath", path);
                        })
                      }
                    >
                      浏览…
                    </Button>
                  </div>
                  <Label htmlFor="concurrency">并发数（1–8）</Label>
                  <Input
                    id="concurrency"
                    required
                    type="number"
                    min={1}
                    max={8}
                    step={1}
                    value={config.concurrency}
                    onChange={(event) =>
                      update("concurrency", event.target.valueAsNumber)
                    }
                  />
                  <Label htmlFor="timeout">单任务超时（分钟，5–180）</Label>
                  <Input
                    id="timeout"
                    required
                    type="number"
                    min={5}
                    max={180}
                    step={1}
                    value={config.scrapeTimeoutMin}
                    onChange={(event) =>
                      update("scrapeTimeoutMin", event.target.valueAsNumber)
                    }
                  />
                  <Label>优先站点</Label>
                  <div className="flex flex-wrap gap-2">
                    {SITE_OPTIONS.map((option) => (
                      <Button
                        key={option.name}
                        type="button"
                        disabled={saving}
                        variant={
                          config.siteOrder === option.name
                            ? "default"
                            : "secondary"
                        }
                        onClick={() => update("siteOrder", option.name)}
                      >
                        {option.label}
                      </Button>
                    ))}
                  </div>
                  <p className="text-muted">
                    {activeSite.note}
                    {config.siteFallback
                      ? `未收录或被拦下时，自动改试 ${fallbackSite.label}。`
                      : `只使用 ${activeSite.label}，不会尝试 ${fallbackSite.label}。`}
                  </p>
                  <Label className="flex items-center gap-2">
                    <Switch
                      checked={config.siteFallback}
                      onCheckedChange={(value) =>
                        update("siteFallback", value === true)
                      }
                    />
                    失败时回退到另一个站点
                  </Label>
                  <p className="text-muted">
                    关掉就只用当前站点。实际传给刮削器的站点链：
                    <span className="mono ml-1">{siteChain}</span>
                  </p>
                  <Label className="flex items-center gap-2">
                    <Switch
                      checked={config.componentsOnDemand}
                      onCheckedChange={(value) =>
                        update("componentsOnDemand", value === true)
                      }
                    />
                    缺少时允许一键下载安装
                  </Label>
                  <p className="text-muted">
                    关掉之后仍可使用已下载的组件与手动指定的路径，只是不再显示下载入口。
                  </p>
                  <Label htmlFor="component-mirror">
                    组件下载源（留空直连 GitHub）
                  </Label>
                  <Input
                    id="component-mirror"
                    value={config.componentMirror}
                    placeholder="https://ghproxy.example/"
                    onChange={(event) =>
                      update("componentMirror", event.target.value)
                    }
                  />
                  <p className="text-muted">
                    填镜像前缀即可加速下载，例如 <span className="mono">https://ghproxy.example/</span>
                    ，程序会在其后再拼上完整的 GitHub 地址。必须是 https。
                  </p>
                  <p className="text-muted">
                    注意：清单与组件包都会从该地址取，也就是说，能否安装由它决定。
                    只填你信任的镜像。
                  </p>
                  <Button type="submit" disabled={saving}>
                    保存刮削器设置
                  </Button>
                </form>
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>环境自检</CardTitle>
              </CardHeader>
              <CardContent>
                <Button
                  variant="secondary"
                  onClick={() => {
                    forceRef.current = true;
                    setVersion((n) => n + 1);
                  }}
                >
                  重新自检
                </Button>
                {healthError ? (
                  <p role="alert" className="text-danger">
                    {healthError}
                  </p>
                ) : (
                  health && (
                    <div className="detail-rows">
                      {[
                        ["刮削器", health.scraper],
                        ["Python", health.python],
                        ["协议", health.v],
                        [
                          "Chrome",
                          health.chrome.found ? health.chrome.path : "未找到",
                        ],
                        [
                          "驱动",
                          health.driver.ready
                            ? "就绪"
                            : health.driver.writable
                              ? "首次刮削时下载"
                              : "目录不可写",
                        ],
                      ].map(([k, v]) => (
                        <div key={k} className="detail-row">
                          {k}：<b>{v}</b>
                        </div>
                      ))}
                    </div>
                  )
                )}
              </CardContent>
            </Card>
          </>
        )}
        {section === "player" && config && (
          <Card>
            <CardHeader>
              <CardTitle>外部播放器</CardTitle>
            </CardHeader>
            <CardContent>
              <p className="text-muted mb-4">
                HEVC / 10bit 内嵌播放可能不兼容，可使用 PotPlayer、MPC-HC、VLC
                或系统默认播放器。
              </p>
              <form
                className="space-y-4"
                onSubmit={(event) => {
                  event.preventDefault();
                  void save({ playerPath: config.playerPath.trim() });
                }}
              >
                <Label htmlFor="player-path">可执行文件</Label>
                <div className="flex gap-2">
                  <Input
                    id="player-path"
                    value={config.playerPath}
                    placeholder="留空使用系统默认关联"
                    onChange={(event) =>
                      update("playerPath", event.target.value)
                    }
                  />
                  <Button
                    variant="secondary"
                    onClick={() =>
                      void run("选择程序失败", async () => {
                        const path = await call("PickExecutable", "选择播放器");
                        if (path) update("playerPath", path);
                      })
                    }
                  >
                    浏览…
                  </Button>
                </div>
                <div className="flex gap-2">
                  <Button type="submit" disabled={saving}>
                    保存播放器设置
                  </Button>
                  <Button
                    variant="secondary"
                    onClick={() => update("playerPath", "")}
                  >
                    使用系统默认
                  </Button>
                </div>
              </form>
            </CardContent>
          </Card>
        )}
        {section === "env" && (
          <Card>
            <CardHeader>
              <CardTitle>环境诊断</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="flex gap-2 mb-4">
                <Button
                  onClick={() => {
                    forceRef.current = true;
                    setVersion((n) => n + 1);
                  }}
                >
                  重新检查
                </Button>
                <Button
                  variant="secondary"
                  disabled={!report}
                  onClick={() =>
                    void run("复制诊断失败", async () => {
                      await navigator.clipboard.writeText(
                        envRows.map(([k, v]) => `${k}: ${v ?? "—"}`).join("\n"),
                      );
                      notify("诊断信息已复制", "ok");
                    })
                  }
                >
                  复制诊断
                </Button>
              </div>
              {envRows.map(([k, v]) => (
                <div className="detail-row" key={k}>
                  {k}：<b>{v ?? "—"}</b>
                </div>
              ))}
            </CardContent>
          </Card>
        )}
        {section === "appearance" && (
          <>
            <Card>
              <CardHeader>
                <CardTitle>主题</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="flex gap-2">
                  {[
                    ["system", "跟随系统"],
                    ["dark", "深色"],
                    ["light", "浅色"],
                  ].map(([value, label]) => (
                    <Button
                      key={value}
                      disabled={saving}
                      variant={preference === value ? "default" : "secondary"}
                      onClick={() => {
                        setThemePreference(value);
                        void save({ theme: value });
                      }}
                    >
                      {label}
                    </Button>
                  ))}
                </div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>配置位置</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="mono break-all">{paths?.configDir}</p>
                <p className="mono break-all">{paths?.configFile}</p>
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>首次引导</CardTitle>
              </CardHeader>
              <CardContent>
                <p>重置后，无视频库时下次启动显示引导。</p>
                <Button
                  variant="secondary"
                  onClick={() => {
                    localStorage.removeItem("cinevault.onboarded");
                    notify("已重置引导标记", "ok");
                  }}
                >
                  重置引导
                </Button>
              </CardContent>
            </Card>
          </>
        )}
        {section === "about" && (
          <Card>
            <CardHeader>
              <CardTitle>视频库工作站</CardTitle>
            </CardHeader>
            <CardContent>
              <p>本地视频库：刮削元数据、浏览和观看。</p>
              <p>
                App 版本：{appVersion || "0.1.0"} · 协议版本：1
              </p>
              <p>Go + Wails + React + Tailwind CSS</p>
              <p className="text-muted">
                当前请手动更新应用，自动更新尚未提供。
              </p>
            </CardContent>
          </Card>
        )}
      </div>
    </div>
  );
}
