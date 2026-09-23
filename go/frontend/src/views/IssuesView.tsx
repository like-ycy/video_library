import { useEffect, useState } from "react";
import { Button, Card, CardContent, CardHeader, CardTitle } from "xwang-ui";
import { call } from "../api";
import { useTasks } from "../tasks";
import type { Issue } from "../types";
import { EmptyState, PageHeader, RefreshButton } from "../components/shell";
const META: Record<string, [string, string]> = {
  unrecognized: ["番号无法识别", "按 字母串-数字 规范文件名，例如 IPX-001.mp4"],
  sidecar_corrupt: [
    "边车 JSON 损坏",
    "检查 演员/meta/演员.json 是否为合法 JSON",
  ],
  missing_file: ["文件缺失", "重新连接硬盘或重新扫描索引"],
  sidecar_missing: ["缺少边车元数据", "到扫描页勾选后开始刮削"],
  unscraped: ["尚未刮削", "到扫描页开始刮削"],
  missing_art: ["图片缺失", "到扫描页勾选重新下载图片"],
};
export function IssuesView({
  libraryId,
  navigate,
}: {
  libraryId: string;
  navigate: (route: string) => void;
}) {
  const [issues, setIssues] = useState<Issue[]>([]);
  const [error, setError] = useState("");
  const [version, setVersion] = useState(0);
  const [loading, setLoading] = useState(false);
  const { task } = useTasks();
  useEffect(() => {
    let active = true;
    if (!libraryId) return;
    setLoading(true);
    setError("");
    call("ScanLibrary", libraryId)
      .then((result) => {
        if (active)
          setIssues([
            ...(result.issues ?? []),
            ...(result.candidates ?? [])
              .filter((c) => !c.scraped || c.missingArt)
              .map((c) => ({
                kind: c.scraped ? "missing_art" : "unscraped",
                actress: c.actress,
                subject: c.fanha,
                message: c.scraped ? "封面或截图缺失" : "尚未刮削元数据",
              })),
          ]);
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
  }, [libraryId, version, task.indexVersion]);
  const groups: Record<string, Issue[]> = {};
  for (const issue of issues) (groups[issue.kind] ??= []).push(issue);
  return (
    <div className="page">
      <PageHeader title="异常与修复" sub="按问题类型分组，附带修复建议">
        <RefreshButton
          onClick={() => setVersion((n) => n + 1)}
          disabled={loading}
        />
        <Button variant="secondary" size="sm" onClick={() => navigate("scan")}>
          去扫描
        </Button>
      </PageHeader>
      <div className="page-body space-y-4">
        {error && (
          <p role="alert" className="text-danger">
            {error}
          </p>
        )}
        {loading ? (
          "检查中…"
        ) : !libraryId ? (
          <EmptyState title="没有视频库" />
        ) : !issues.length && !error ? (
          <EmptyState title="没有发现问题" />
        ) : (
          Object.entries(groups).map(([kind, rows]) => (
            <Card key={kind}>
              <CardHeader>
                <CardTitle>
                  {META[kind]?.[0] ?? kind}（{rows!.length}）
                </CardTitle>
              </CardHeader>
              <CardContent>
                {rows!.map((row, i) => (
                  <div className="issue-row" key={i}>
                    <div className="issue-main">
                      <strong>{row.subject}</strong> · {row.actress}
                      <p>{row.message}</p>
                      <p className="issue-tip">{META[kind]?.[1]}</p>
                    </div>
                    {["unscraped", "missing_art", "sidecar_missing"].includes(
                      kind,
                    ) && (
                      <Button size="sm" onClick={() => navigate("scan")}>
                        去处理
                      </Button>
                    )}
                  </div>
                ))}
              </CardContent>
            </Card>
          ))
        )}
      </div>
    </div>
  );
}
