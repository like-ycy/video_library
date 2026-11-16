import { useEffect, useRef, useState } from "react";
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
  Progress,
} from "xwang-ui";
import { call, on } from "../api";
import { formatSize } from "../format.js";
import type { Component } from "../types";

/** 安装来源 → 给人看的说法。空字符串表示当前没有可用的可执行文件。 */
const SOURCE_LABEL: Record<string, string> = {
  config: "手动指定路径",
  component: "按需下载的组件",
  bundled: "随安装包附带",
  dev: "开发目录",
  path: "系统 PATH",
  auto: "自动探测",
};

const STATE_BADGE: Record<
  Component["state"],
  { label: string; variant: "success" | "warning" | "danger" | "neutral" }
> = {
  ready: { label: "已就绪", variant: "success" },
  outdated: { label: "可更新", variant: "warning" },
  missing: { label: "未安装", variant: "neutral" },
  corrupt: { label: "文件不完整", variant: "danger" },
  incompatible: { label: "需要更新的 App", variant: "danger" },
};

/**
 * 在 revision 变化时执行一次回调。
 *
 * 关键点是「回调放在 ref 里、依赖只留 revision」：调用方常写成内联箭头函数
 * （`onChanged={() => setVersion((n) => n + 1)}`），它的身份每次渲染都会变。
 * 若把回调放进依赖，effect 会在「刷新 → 渲染 → 新回调」之间无限循环 ——
 * 实测一次安装完成能触发上百次重扫。
 *
 * seen 额外保证同一个 revision 只通知一次：React 的严格模式会重复执行
 * effect，重放一次就等于多刷新一遍整页数据。
 */
function useOnRevision(revision: number, action: () => void) {
  const seen = useRef(0);
  const latest = useRef(action);
  latest.current = action;
  useEffect(() => {
    if (revision === 0 || seen.current === revision) return;
    seen.current = revision;
    latest.current();
  }, [revision]);
}

/**
 * 组件安装的公共逻辑：进度、错误、刷新触发。
 *
 * 抽出来是因为「下载 20 MB」这件事在设置页和刮削页都要做，两处各写一份
 * 事件订阅，迟早有一处忘了取消订阅、或把别的事件的进度画到自己身上。
 *
 * 参数是整条组件快照而不只是名字：安装状态要能从后端快照里恢复，见下方
 * `component.installing` 那段。
 */
export function useComponentInstall(component: Component) {
  const name = component.name;
  const [installing, setInstalling] = useState(false);
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(
    null,
  );
  const [error, setError] = useState("");
  // 每次状态可能变化时 +1，供调用方放进 effect 依赖里重新拉数据。
  const [revision, setRevision] = useState(0);

  useEffect(() => {
    const bump = () => setRevision((n) => n + 1);
    const disposers = [
      on("component:progress", (payload) => {
        if (payload.name === name) {
          setInstalling(true);
          setProgress({ done: payload.done, total: payload.total });
        }
      }),
      on("component:done", (payload) => {
        if (payload.name !== name) return;
        setInstalling(false);
        setProgress(null);
        setError("");
        bump();
      }),
      on("component:failed", (payload) => {
        if (payload.name !== name) return;
        setInstalling(false);
        setProgress(null);
        setError(payload.message);
      }),
      // 后台清单刷新完成：状态可能从「拿不到下载信息」变成「可下载」，
      // 也可能发现新版本。不通知的话用户得再进一次页面才看得到。
      on("component:refreshed", bump),
    ];
    return () => disposers.forEach((off) => off());
  }, [name]);

  // 以清单快照为准同步「是否在装」。
  //
  // 下载期间切走页面再回来，这个 hook 会被重新创建，本地 state 归零；而
  // 解析清单、等文件锁、解压这几段本来就不发进度事件，界面于是显示成
  // 「什么也没发生」——「下载」和「移除」都重新可点，重复下载报错、误点
  // 移除还会把刚装好的目录删掉。所以每次快照变化都拿后端状态覆盖本地。
  useEffect(() => {
    if (component.installing) {
      setInstalling(true);
      setProgress({
        done: component.progressDone,
        total: component.progressTotal,
      });
      return;
    }
    setInstalling(false);
    setProgress(null);
  }, [component.installing, component.progressDone, component.progressTotal]);

  function install() {
    setError("");
    setInstalling(true);
    // 先给 0/0：下载开始前的「解析清单」阶段也要有反馈，否则点下去没反应。
    setProgress({ done: 0, total: 0 });
    // 后端立即返回，结果走事件；这里只拦「连后台任务都没起来」的情况。
    call("InstallComponent", name).catch((err) => {
      setInstalling(false);
      setProgress(null);
      setError(String(err));
    });
  }

  function remove() {
    setError("");
    return call("RemoveComponent", name)
      .then(() => setRevision((n) => n + 1))
      .catch((err) => {
        // 后端的拒绝理由是有用的（「刮削进行中」「文件被占用」），
        // 静默吞掉会让用户以为按钮坏了。
        setError(String(err));
        setRevision((n) => n + 1);
      });
  }

  return { installing, progress, error, install, remove, revision };
}

/** 一行「已装 0.4.2 / 可更新到 0.4.3」的说明。 */
function versionLine(component: Component): string {
  if (component.installedVersion && component.availableVersion) {
    return component.installedVersion === component.availableVersion
      ? `版本 ${component.installedVersion}`
      : `已装 ${component.installedVersion}，最新 ${component.availableVersion}`;
  }
  if (component.installedVersion) return `已装 ${component.installedVersion}`;
  if (component.availableVersion) return `最新 ${component.availableVersion}`;
  return "版本未知";
}

/** 设置页里的组件卡片：状态 + 下载/移除。 */
export function ComponentCard({
  component,
  onChanged,
}: {
  component: Component;
  onChanged: () => void;
}) {
  const { installing, progress, error, install, remove, revision } =
    useComponentInstall(component);
  const [confirming, setConfirming] = useState(false);

  useOnRevision(revision, onChanged);

  const badge = STATE_BADGE[component.state] ?? STATE_BADGE.missing;
  const percent = progress?.total
    ? Math.min(100, (progress.done / progress.total) * 100)
    : 0;
  const action = component.state === "outdated" ? "更新" : component.installedVersion ? "重新下载" : "下载安装";
  const downloadable = component.canInstall && component.onDemand;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          {component.label}
          <Badge variant={badge.variant}>{badge.label}</Badge>
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="detail-rows">
          <div className="detail-row">
            版本：<b>{versionLine(component)}</b>
          </div>
          {component.path && (
            <div className="detail-row">
              使用中：<b>{SOURCE_LABEL[component.source] ?? component.source}</b>
              <span className="mono block break-all">{component.path}</span>
            </div>
          )}
          {component.size > 0 && (
            <div className="detail-row">
              下载大小：<b>{formatSize(component.size)}</b>
            </div>
          )}
        </div>

        {installing && (
          <div className="space-y-1">
            <Progress value={percent} />
            <p className="text-muted">
              {progress?.total
                ? `下载中 ${formatSize(progress.done)} / ${formatSize(progress.total)}`
                : "正在准备下载…"}
            </p>
          </div>
        )}
        {error && (
          <p role="alert" className="text-danger">
            {error}
          </p>
        )}
        {!installing && !error && component.message && (
          <p className="text-muted">{component.message}</p>
        )}

        <div className="flex flex-wrap gap-2">
          {downloadable && (
            <Button disabled={installing} onClick={install}>
              {action}
            </Button>
          )}
          {component.installedVersion && (
            <Button
              variant="secondary"
              disabled={installing}
              onClick={() => setConfirming(true)}
            >
              移除
            </Button>
          )}
        </div>
        {!downloadable && component.canInstall && !component.onDemand && (
          <p className="text-muted">
            下载入口已关闭（见下方「缺少时允许一键下载安装」）。
          </p>
        )}
        {!component.canInstall && !component.path && !installing && (
          <p className="text-muted">
            暂时拿不到下载信息（清单不可达）。可检查网络或到「环境诊断」查看详情，
            也可以手动指定路径。
          </p>
        )}

        <Dialog
          open={confirming}
          onOpenChange={(open) => {
            if (!open) setConfirming(false);
          }}
        >
          <DialogContent>
            <DialogTitle>卸载{component.label}</DialogTitle>
            <DialogDescription>
              已下载的文件会被删除，之后可重新下载。
              {component.source === "component" ? "" : "（当前使用的不是这份副本）"}
            </DialogDescription>
            <DialogFooter>
              <Button variant="secondary" onClick={() => setConfirming(false)}>
                取消
              </Button>
              <Button
                variant="danger"
                onClick={() => {
                  setConfirming(false);
                  void remove();
                }}
              >
                卸载
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </CardContent>
    </Card>
  );
}

/**
 * 刮削页顶部的「还差一步」提示。
 *
 * 只在缺组件时出现：安装完成后它自己消失，不需要用户再去设置页确认一遍。
 */
export function ComponentPrompt({
  component,
  onInstalled,
}: {
  component: Component;
  onInstalled: () => void;
}) {
  const { installing, progress, error, install, revision } =
    useComponentInstall(component);

  useOnRevision(revision, onInstalled);

  const percent = progress?.total
    ? Math.min(100, (progress.done / progress.total) * 100)
    : 0;

  return (
    <div className="component-prompt">
      <div>
        <b>刮削器还没安装</b>
        <p className="text-muted">
          首次刮削需要先下载刮削器
          {component.size > 0 ? `（${formatSize(component.size)}）` : ""}
          ，只下载一次，之后不再重复。
        </p>
      </div>
      {installing ? (
        <div className="component-prompt-progress">
          <Progress value={percent} />
          <span className="text-muted">
            {progress?.total
              ? `${formatSize(progress.done)} / ${formatSize(progress.total)}`
              : "准备中…"}
          </span>
        </div>
      ) : (
        <Button onClick={install} disabled={!component.canInstall}>
          下载安装
        </Button>
      )}
      {error && (
        <p role="alert" className="text-danger">
          {error}
        </p>
      )}
      {!component.canInstall && !installing && !error && component.message && (
        <p className="text-muted">{component.message}</p>
      )}
    </div>
  );
}
