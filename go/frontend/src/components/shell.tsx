import type { ReactNode } from "react";
import { Badge, Button } from "xwang-ui";

export function Icon({ name, fill = false }: { name: string; fill?: boolean }) {
  return (
    <span aria-hidden="true" className={`ms${fill ? " fill" : ""}`}>
      {name}
    </span>
  );
}
export function PageHeader({
  title,
  sub,
  children,
}: {
  title: string;
  sub?: string;
  children?: ReactNode;
}) {
  return (
    <header className="page-header">
      <div>
        <h1>{title}</h1>
        {sub && <div className="page-sub">{sub}</div>}
      </div>
      <div className="page-header-actions">{children}</div>
    </header>
  );
}
export function EmptyState({
  title,
  desc,
  icon = "inbox",
  children,
}: {
  title: string;
  desc?: string;
  icon?: string;
  children?: ReactNode;
}) {
  return (
    <div className="empty">
      <div className="empty-icon">
        <Icon name={icon} />
      </div>
      <h2 className="empty-title">{title}</h2>
      <p className="empty-desc">{desc}</p>
      {children && <div className="empty-actions">{children}</div>}
    </div>
  );
}
export function StatusBadge({
  scraped,
  missing,
  missingArt,
}: {
  scraped: boolean;
  missing?: boolean;
  missingArt?: boolean;
}) {
  return (
    <Badge
      variant={
        missing
          ? "danger"
          : !scraped
            ? "neutral"
            : missingArt
              ? "warning"
              : "success"
      }
    >
      {missing
        ? "文件异常"
        : !scraped
          ? "未刮削"
          : missingArt
            ? "缺图"
            : "已刮削"}
    </Badge>
  );
}
export function RefreshButton({
  onClick,
  disabled = false,
}: {
  onClick: () => void;
  disabled?: boolean;
}) {
  return (
    <Button variant="secondary" size="sm" disabled={disabled} onClick={onClick}>
      <Icon name="refresh" />
      刷新
    </Button>
  );
}
