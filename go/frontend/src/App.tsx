import { useCallback, useEffect, useRef, useState } from "react";
import {
  Button,
  Input,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "xwang-ui";
import { call } from "./api";
import { FeedbackProvider, useFeedback } from "./feedback";
import { TaskProvider, useTasks } from "./tasks";
import {
  cycleTheme,
  getThemePreference,
  initTheme,
  seedThemeFromConfig,
  startThemeWatcher,
  themeLabel,
} from "./theme.js";
import { DEFAULT_ROUTE, NAV_GROUPS } from "./nav.js";
import type { Library, Video } from "./types";
import { Icon } from "./components/shell";
import { Detail } from "./components/detail";
import { WatchView } from "./views/WatchView";
import { HistoryView } from "./views/HistoryView";
import { ScrapeView } from "./views/ScrapeView";
import { TaskMonitorView } from "./views/TaskMonitorView";
import { IssuesView } from "./views/IssuesView";
import { SettingsView } from "./views/SettingsView";
import { Onboarding } from "./views/Onboarding";

function Application() {
  const [libraries, setLibraries] = useState<Library[]>([]);
  const [libraryId, setLibraryId] = useState("");
  const [route, setRoute] = useState(DEFAULT_ROUTE);
  const [search, setSearch] = useState("");
  const [keyword, setKeyword] = useState("");
  const [preference, setPreference] = useState(getThemePreference());
  const [themeTitle, setThemeTitle] = useState(themeLabel());
  const [platform, setPlatform] = useState(
    document.documentElement.dataset.platform,
  );
  const [detail, setDetail] = useState<{
    item: Video;
    play: boolean;
    libraryId: string;
  } | null>(null);
  const [version, setVersion] = useState(0);
  const [appVersion, setAppVersion] = useState("");
  const [continueCount, setContinueCount] = useState(0);
  const searchRef = useRef<HTMLInputElement>(null);
  const { run, notify } = useFeedback();
  const { task } = useTasks();
  const library = libraries.find((lib) => lib.id === libraryId);
  const refreshLibraries = useCallback(async () => {
    const libs = (await call("ListLibraries")) ?? [];
    setLibraries(libs);
    setLibraryId((old) =>
      libs.some((lib) => lib.id === old)
        ? old
        : ((libs.find((lib) => lib.available) ?? libs[0])?.id ?? ""),
    );
  }, []);
  useEffect(() => {
    let active = true;
    initTheme();
    const stopTheme = startThemeWatcher();
    const syncTheme = () => {
      setPreference(getThemePreference());
      setThemeTitle(themeLabel());
    };
    syncTheme();
    window.addEventListener("cinevault:theme-changed", syncTheme);
    const shortcut = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        searchRef.current?.focus();
        searchRef.current?.select();
      }
    };
    document.addEventListener("keydown", shortcut);
    void run("初始化失败", async () => {
      const [libs, cfg] = await Promise.all([
        call("ListLibraries"),
        call("GetConfig"),
      ]);
      if (!active) return;
      setLibraries(libs ?? []);
      setLibraryId((libs?.find((lib) => lib.available) ?? libs?.[0])?.id ?? "");
      seedThemeFromConfig(cfg.theme);
      syncTheme();
      if (!libs?.length && localStorage.getItem("cinevault.onboarded") !== "1")
        setRoute("onboarding");
    });
    window.runtime
      ?.Environment()
      .then((env) => {
        if (active) {
          setPlatform(env.platform);
          document.documentElement.dataset.platform = env.platform;
        }
      })
      .catch((error) => notify(`读取窗口平台失败：${String(error)}`, "error"));
    call("GetAppVersion")
      .then((value) => {
        if (active) setAppVersion(value);
      })
      .catch(() => undefined);
    return () => {
      active = false;
      stopTheme();
      window.removeEventListener("cinevault:theme-changed", syncTheme);
      document.removeEventListener("keydown", shortcut);
    };
  }, [run, notify]);
  useEffect(() => {
    let active = true;
    if (!libraryId) {
      setContinueCount(0);
      return;
    }
    call("ListContinueWatching", libraryId)
      .then((items) => {
        if (active) setContinueCount(items?.length ?? 0);
      })
      .catch((error) => {
        if (active) notify(`读取继续观看失败：${String(error)}`, "error");
      });
    return () => {
      active = false;
    };
  }, [libraryId, version, task.indexVersion, notify]);
  function navigate(next: string) {
    setDetail(null);
    setRoute(next);
  }
  function selectLibrary(id: string) {
    setDetail(null);
    setLibraryId(id);
  }
  async function addLibrary() {
    const path = await call("PickLibraryRoot");
    if (!path) return;
    const lib = await call("AddLibrary", path);
    await refreshLibraries();
    selectLibrary(lib.id);
    notify(`已添加视频库：${lib.root}`, "ok");
  }
  const add = () => {
    void run("添加视频库失败", addLibrary);
  };
  const changed = () => setVersion((n) => n + 1);
  const open = (item: Video, play = false) =>
    setDetail({ item, play, libraryId });
  const commitSearch = (value: string) => {
    setKeyword(value.trim());
    if (!["actresses", "movies"].includes(route)) navigate("movies");
  };
  const viewKey = `${libraryId}:${route}`;
  let content;
  if (route === "actresses" || route === "movies")
    content = (
      <WatchView
        key={viewKey}
        refreshToken={version}
        library={library}
        mode={route}
        keyword={keyword}
        onOpen={open}
        navigate={navigate}
        addLibrary={add}
      />
    );
  else if (route === "continue" || route === "recent")
    content = (
      <HistoryView
        key={viewKey}
        refreshToken={version}
        libraryId={libraryId}
        kind={route}
        onOpen={open}
        onChanged={changed}
      />
    );
  else if (route === "scan")
    content = (
      <ScrapeView key={viewKey} libraryId={libraryId} navigate={navigate} />
    );
  else if (route === "tasks") content = <TaskMonitorView />;
  else if (route === "issues")
    content = (
      <IssuesView key={viewKey} libraryId={libraryId} navigate={navigate} />
    );
  else if (route === "onboarding")
    content = (
      <Onboarding
        libraryId={libraryId}
        addLibrary={addLibrary}
        done={() => navigate(DEFAULT_ROUTE)}
      />
    );
  else
    content = (
      <SettingsView
        key={route}
        section={route.replace("settings-", "")}
        libraries={libraries}
        addLibrary={add}
        refreshLibraries={refreshLibraries}
        selectLibrary={selectLibrary}
      />
    );
  return (
    <div id="app">
      <header className="titlebar">
        <div className="titlebar-left">
          <div className="brand-block">
            <span className="brand-name">视频库</span>
            <span className="brand-version mono">{appVersion || "0.1.0"}</span>
          </div>
          <span className="titlebar-sep" />
          <div className="library-select-wrap">
            <Icon name="folder_special" />
            <Select
              value={libraryId}
              disabled={!libraries.length}
              onValueChange={selectLibrary}
            >
              <SelectTrigger aria-label="选择视频库" className="w-[180px] h-7">
                <SelectValue placeholder="尚未添加视频库" />
              </SelectTrigger>
              <SelectContent>
                {libraries.map((lib) => (
                  <SelectItem value={lib.id} key={lib.id}>
                    {lib.root}
                    {lib.available ? "" : "（不可访问）"}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <Button variant="ghost" size="sm" onClick={add}>
            <Icon name="add" />
            添加库
          </Button>
        </div>
        <div className="titlebar-center">
          <div className="global-search">
            <Icon name="search" />
            <Input
              ref={searchRef}
              aria-label="全局搜索"
              placeholder="搜索番号、演员、片名…"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              onBlur={() => commitSearch(search)}
              onKeyDown={(event) => {
                if (event.key === "Enter") commitSearch(search);
                if (event.key === "Escape") {
                  setSearch("");
                  commitSearch("");
                }
              }}
            />
            <kbd>Ctrl+K</kbd>
          </div>
        </div>
        <div className="titlebar-right">
          <Button
            size="sm"
            disabled={!libraryId || task.running}
            onClick={() =>
              void run("扫描失败", async () => {
                const result = await call("ScanLibrary", libraryId);
                notify(
                  `扫描完成：发现 ${result.candidates?.length ?? 0} 个文件`,
                  "ok",
                );
                navigate("scan");
              })
            }
          >
            <Icon name="sync" />
            立即扫描
          </Button>
          <Button
            variant="secondary"
            size="icon-sm"
            title={themeTitle}
            aria-label={themeTitle}
            onClick={() => cycleTheme()}
          >
            <Icon
              name={
                preference === "light"
                  ? "light_mode"
                  : preference === "dark"
                    ? "dark_mode"
                    : "desktop_windows"
              }
            />
          </Button>
          {platform !== "darwin" && (
            <div className="win-ctl-group">
              <button
                className="win-ctl"
                aria-label="最小化"
                onClick={() => window.runtime?.WindowMinimise()}
              >
                <Icon name="remove" />
              </button>
              <button
                className="win-ctl"
                aria-label="最大化"
                onClick={() =>
                  void run("切换窗口失败", async () => {
                    const rt = window.runtime;
                    if (!rt) throw new Error("窗口接口不可用");
                    if (await rt.WindowIsMaximised()) rt.WindowUnmaximise();
                    else rt.WindowMaximise();
                  })
                }
              >
                <Icon name="crop_square" />
              </button>
              <button
                className="win-ctl close"
                aria-label="关闭窗口"
                onClick={() => window.runtime?.Quit()}
              >
                <Icon name="close" />
              </button>
            </div>
          )}
        </div>
      </header>
      <div className="shell-body">
        <aside className="sidebar">
          <nav className="sidebar-nav" aria-label="主导航">
            {NAV_GROUPS.map((group) => (
              <div className="nav-group" key={group.id}>
                <div className="nav-group-label">{group.label}</div>
                {group.items.map((item) => (
                  <Button
                    variant="ghost"
                    className={`nav-item justify-start${route === item.id ? " active" : ""}`}
                    key={item.id}
                    aria-current={route === item.id ? "page" : undefined}
                    onClick={() => navigate(item.id)}
                  >
                    <span className="nav-left">
                      <Icon name={item.icon} />
                      <span className="nav-label">{item.label}</span>
                    </span>
                    {item.id === "tasks" && task.running && (
                      <span className="nav-dot" />
                    )}
                    {item.id === "continue" && continueCount > 0 && (
                      <span className="nav-badge">{continueCount}</span>
                    )}
                  </Button>
                ))}
              </div>
            ))}
          </nav>
        </aside>
        <main className="content-host">{content}</main>
      </div>
      {detail && (
        <Detail
          key={`${detail.libraryId}:${detail.item.id}`}
          item={detail.item}
          autoplay={detail.play}
          libraryId={detail.libraryId}
          onClose={() => {
            setDetail(null);
            changed();
          }}
          onChanged={changed}
        />
      )}
    </div>
  );
}
export default function App() {
  return (
    <FeedbackProvider>
      <TaskProvider>
        <Application />
      </TaskProvider>
    </FeedbackProvider>
  );
}
