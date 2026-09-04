"use client";

import {
  ActivityIcon,
  BoxesIcon,
  CircuitBoardIcon,
  LayoutDashboardIcon,
  ShieldCheckIcon,
  UsersRoundIcon,
  type LucideIcon,
} from "lucide-react";

import { Badge } from "@/components/ui/badge";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
} from "@/components/ui/sidebar";

export type DeliveryBoard = {
  board: string;
  total?: number;
};

export type DeliverySurface = "cockpit" | "items" | "projects" | "weekly" | "notifications";

type DeliverySidebarProps = {
  activeBoard: string | null;
  activeSurface: DeliverySurface;
  boards: readonly DeliveryBoard[];
  onNavigate: (surface: DeliverySurface) => void;
  onSelectBoard: (board: string | null) => void;
  sampleVisible: boolean;
};

const boardIcons: Record<string, LucideIcon> = {
  "设备质量与连接": CircuitBoardIcon,
  "产品与平台能力": BoxesIcon,
  "研发交付效能": ActivityIcon,
  "运营保障与安全": ShieldCheckIcon,
  "客户与业务价值": UsersRoundIcon,
};

export function DeliverySidebar({
  activeBoard,
  activeSurface,
  boards,
  onNavigate,
  onSelectBoard,
  sampleVisible,
}: DeliverySidebarProps) {
  function selectBoard(board: string) {
    onSelectBoard(board);
    onNavigate("items");
  }

  return (
    <Sidebar collapsible="icon" variant="inset">
      <SidebarHeader className="px-2 pt-2">
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton isActive={activeSurface === "cockpit"} onClick={() => onNavigate("cockpit")} size="lg" tooltip="IoT 交付" type="button">
              <span className="flex size-7 items-center justify-center rounded-md bg-primary font-semibold text-primary-foreground">I</span>
              <span className="flex min-w-0 flex-1 flex-col text-left leading-tight">
                <span className="truncate font-semibold">IoT Delivery</span>
                <span className="truncate text-xs font-normal text-muted-foreground">研发交付系统</span>
              </span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>

      <SidebarContent className="px-2">
        <SidebarGroup>
          <SidebarGroupLabel>我的工作</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              <SidebarMenuItem>
                <SidebarMenuButton isActive={activeSurface === "weekly"} onClick={() => onNavigate("weekly")} tooltip="我的本周" type="button">
                  <UsersRoundIcon />
                  <span>我的本周</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton isActive={activeSurface === "notifications"} onClick={() => onNavigate("notifications")} tooltip="通知收件箱" type="button">
                  <ShieldCheckIcon />
                  <span>通知收件箱</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>

        <SidebarGroup>
          <SidebarGroupLabel>工作空间</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              <SidebarMenuItem>
                <SidebarMenuButton isActive={activeSurface === "cockpit"} onClick={() => onNavigate("cockpit")} tooltip="交付驾驶舱" type="button">
                  <LayoutDashboardIcon />
                  <span>交付驾驶舱</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton isActive={activeSurface === "items" && activeBoard === null} onClick={() => onNavigate("items")} tooltip="交付事项" type="button">
                  <BoxesIcon />
                  <span>交付事项</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton isActive={activeSurface === "projects"} onClick={() => onNavigate("projects")} tooltip="项目与排期" type="button">
                  <ActivityIcon />
                  <span>项目与排期</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>

        <SidebarGroup>
          <SidebarGroupLabel>五个交付板块</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {boards.map((board) => {
                const Icon = boardIcons[board.board] ?? ActivityIcon;
                return (
                  <SidebarMenuItem key={board.board}>
                    <SidebarMenuButton
                      isActive={activeSurface === "items" && activeBoard === board.board}
                      onClick={() => selectBoard(board.board)}
                      tooltip={board.board}
                      type="button"
                    >
                      <Icon />
                      <span>{board.board}</span>
                    </SidebarMenuButton>
                    <SidebarMenuBadge>{board.total ?? 0}</SidebarMenuBadge>
                  </SidebarMenuItem>
                );
              })}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      <SidebarFooter className="px-2 pb-2">
        <div className="delivery-sidebar-projection">
          <div className="flex items-center gap-2 text-sm font-medium">
            <span className="size-2 rounded-full bg-emerald-500" />
            <span>Obsidian 投影就绪</span>
          </div>
          <p>规划、决策和复盘由 Yunka 单向沉淀。</p>
          {sampleVisible ? <Badge className="mt-3" variant="secondary">本地示例数据</Badge> : null}
        </div>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}
