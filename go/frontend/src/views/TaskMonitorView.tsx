import { useState } from "react";
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Progress,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "xwang-ui";
import { useTasks } from "../tasks";
import { useFeedback } from "../feedback";
import { EmptyState, PageHeader } from "../components/shell";
const STAGES: Record<string, string> = {
  search: "搜索",
  detail: "详情页",
  download_cover: "下载封面",
  download_shots: "下载截图",
};
export function TaskProgress() {
  const { task, cancel, loadLog } = useTasks();
  const { run } = useFeedback();
  const [tab, setTab] = useState("results");
  const ok =
    task.finished?.ok ?? task.results.filter((r) => r.status === "ok").length;
  const failed =
    task.finished?.failed ??
    task.results.filter((r) => r.status === "failed").length;
  const skipped = task.finished?.skipped ?? 0;
  const percent = task.total
    ? Math.min(100, ((ok + failed + skipped) / task.total) * 100)
    : 0;
  return (
    <div className="space-y-3">
      <Card>
        <CardHeader>
          <CardTitle className="flex justify-between items-center">
            <Badge
              variant={
                task.running
                  ? "info"
                  : task.finished?.canceled
                    ? "warning"
                    : "neutral"
              }
            >
              {task.running
                ? "运行中"
                : task.finished?.canceled
                  ? "已取消"
                  : task.finished
                    ? "已结束"
                    : "空闲"}
            </Badge>
            {task.running && (
              <Button
                variant="danger"
                size="sm"
                onClick={() => void run("取消任务失败", cancel)}
              >
                取消任务
              </Button>
            )}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <Progress value={percent} aria-label="刮削进度" />
          <div className="counts">
            <span>成功 {ok}</span>
            <span>失败 {failed}</span>
            <span>跳过 {skipped}</span>
            <span>总数 {task.total || "—"}</span>
          </div>
          {(task.finished?.fatalMessage || task.finished?.error) && (
            <p role="alert" className="text-danger">
              {task.finished.fatalMessage || task.finished.error}
            </p>
          )}
        </CardContent>
      </Card>
      <Card>
        <CardContent className="pt-4">
          <Tabs
            value={tab}
            onValueChange={(value) => {
              setTab(value);
              if (value === "log") void run("读取日志失败", loadLog);
            }}
          >
            <TabsList>
              <TabsTrigger value="results">逐条结果</TabsTrigger>
              <TabsTrigger value="log">日志</TabsTrigger>
            </TabsList>
            <TabsContent value="results">
              {task.results.length ? (
                <div className="item-list">
                  {task.results.map((entry) => (
                    <div className={`item ${entry.status}`} key={entry.fanha}>
                      <span className="f">{entry.fanha}</span>
                      <span className="msg">
                        {entry.status === "running"
                          ? `${STAGES[entry.stage ?? ""] ?? entry.stage ?? ""} ${Math.round((entry.percent ?? 0) * 100)}%`
                          : entry.title ||
                            [entry.message, entry.detail]
                              .filter(Boolean)
                              .join("：") ||
                            `完成（截图 ${entry.shots ?? 0}/${entry.totalShots ?? 0}）`}
                      </span>
                    </div>
                  ))}
                </div>
              ) : (
                <EmptyState
                  title="暂无任务记录"
                  desc="在扫描页选择条目后开始刮削。"
                />
              )}
            </TabsContent>
            <TabsContent value="log">
              <Button
                variant="secondary"
                size="sm"
                onClick={() => void run("读取日志失败", loadLog)}
              >
                刷新日志
              </Button>
              <pre className="log">{task.log.join("\n") || "暂无日志"}</pre>
            </TabsContent>
          </Tabs>
        </CardContent>
      </Card>
    </div>
  );
}
export function TaskMonitorView() {
  return (
    <div className="page">
      <PageHeader title="任务监控" sub="任务状态跨页面持续更新" />
      <div className="page-body">
        <TaskProgress />
      </div>
    </div>
  );
}
