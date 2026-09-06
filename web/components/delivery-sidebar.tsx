"use client";
import {
  Activity,
  Box,
  CalendarDays,
  CircuitBoard,
  Folder,
  Inbox,
  LayoutDashboard,
  ListChecks,
  LogOut,
  Plus,
  Settings,
  ShieldCheck,
  Users,
  type LucideIcon,
} from "lucide-react";
import { Sidebar } from "@/components/ui/sidebar";
import { Action, Chip } from "./delivery/ui";
import { cn } from "@/lib/utils";
export type DeliveryBoard = { board: string; total?: number };
export type DeliverySurface =
  | "cockpit"
  | "items"
  | "projects"
  | "weekly"
  | "notifications"
  | "account";
const icons: Record<string, LucideIcon> = {
  设备质量与连接: CircuitBoard,
  产品与平台能力: Box,
  研发交付效能: Activity,
  运营保障与安全: ShieldCheck,
  客户与业务价值: Users,
};
type Props = {
  activeBoard: string | null;
  activeSurface: DeliverySurface;
  boards: readonly DeliveryBoard[];
  onNavigate: (s: DeliverySurface) => void;
  onSelectBoard: (b: string | null) => void;
  sampleVisible: boolean;
  onCreate?: () => void;
  canCreate?: boolean;
  sessionName?: string;
  sessionDescription?: string;
  onAccount?: () => void;
  onLogout?: () => void;
  busy?: boolean;
};
export function DeliverySidebar({
  activeBoard,
  activeSurface,
  boards,
  onNavigate,
  onSelectBoard,
  sampleVisible,
  onCreate,
  canCreate = true,
  sessionName,
  sessionDescription,
  onAccount,
  onLogout,
  busy,
}: Props) {
  function row(
    label: string,
    Icon: LucideIcon,
    active: boolean,
    click: () => void,
    count?: number,
  ) {
    return (
      <button
        key={label}
        type="button"
        className={cn("nav-item", active && "active")}
        data-active={active ? "" : undefined}
        aria-current={active ? "page" : undefined}
        aria-label={label}
        onClick={click}
      >
        <Icon className="icon" aria-hidden="true" />
        <span>{label}</span>
        {count !== undefined ? (
          <span className="nav-count">{count}</span>
        ) : null}
      </button>
    );
  }
  return (
    <Sidebar collapsible="none" className="sidebar" aria-label="主导航">
      <div className="brand">
        <span className="brand-mark">I</span>
        <strong>IoT Delivery</strong>
      </div>
      <div className="sidebar-create">
        <Action
          variant="ghost"
          className="ghost"
          icon={Plus}
          disabled={!canCreate}
          onClick={onCreate}
        >
          新建交付事项
        </Action>
      </div>
      <nav className="sidebar-nav">
        <div className="nav-group">
          <p className="nav-group-label">我的工作</p>
          {row("我的本周", CalendarDays, activeSurface === "weekly", () =>
            onNavigate("weekly"),
          )}
          {row("通知收件箱", Inbox, activeSurface === "notifications", () =>
            onNavigate("notifications"),
          )}
        </div>
        <div className="nav-group">
          <p className="nav-group-label">工作空间</p>
          {row("交付驾驶舱", LayoutDashboard, activeSurface === "cockpit", () =>
            onNavigate("cockpit"),
          )}
          {row(
            "交付事项",
            ListChecks,
            activeSurface === "items" && !activeBoard,
            () => onNavigate("items"),
          )}
          {row("项目与排期", Folder, activeSurface === "projects", () =>
            onNavigate("projects"),
          )}
        </div>
        <div className="nav-group">
          <p className="nav-group-label">五个交付板块</p>
          {boards.map((board) =>
            row(
              board.board,
              icons[board.board] ?? Activity,
              activeSurface === "items" && activeBoard === board.board,
              () => onSelectBoard(board.board),
              board.total,
            ),
          )}
        </div>
        {onAccount ? (
          <div className="nav-group">
            <p className="nav-group-label">账号</p>
            {row(
              "账号与管理",
              Settings,
              activeSurface === "account",
              onAccount,
            )}
          </div>
        ) : null}
      </nav>
      <footer className="sidebar-footer">
        <span className="avatar" aria-hidden="true">
          {Array.from(sessionName || "I")[0]}
        </span>
        <div className="session-identity">
          <strong>{sessionName || "IoT 研发交付"}</strong>
          <small>{sessionDescription || "交付工作空间"}</small>
          {sampleVisible ? <Chip>本地示例</Chip> : null}
        </div>
        {onLogout ? (
          <button
            type="button"
            className="icon-button"
            title="退出"
            aria-label="退出"
            disabled={busy}
            onClick={onLogout}
          >
            <LogOut className="icon" />
          </button>
        ) : null}
      </footer>
    </Sidebar>
  );
}
