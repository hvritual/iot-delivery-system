/** Browser projections of the existing delivery JSON contract (no new API). */
export type Evidence = {
  kind: string;
  title: string;
  reference?: string;
  recordedAt?: string;
};
export type IoTBinding = {
  kind: string;
  reference: string;
  label?: string;
  attributes?: Record<string, string>;
};
export type TraceLink = {
  kind: string;
  reference: string;
  title?: string;
  url?: string;
  status?: string;
  recordedAt?: string;
};
export type Dependency = { itemId: string; relation: string };
export type WorkItem = {
  id: string;
  revision?: number;
  title: string;
  board: string;
  kind?: string;
  type?: string;
  projectId?: string;
  parentId?: string;
  owner?: string;
  priority?: string;
  status?: string;
  gate?: string;
  releaseId?: string;
  sprintId?: string;
  milestoneId?: string;
  startDate?: string;
  dueDate?: string;
  estimatePoints?: number;
  progressPercent?: number;
  plan?: string;
  solution?: string;
  blocker?: string;
  retrospective?: string;
  isSample?: boolean;
  createdAt?: string;
  updatedAt?: string;
  dependencies?: Dependency[];
  iotBindings?: IoTBinding[];
  traceLinks?: TraceLink[];
  evidence?: Evidence[];
  decisions?: {
    id: string;
    title: string;
    outcome: string;
    createdAt?: string;
  }[];
  comments?: { id: string; body: string; author: string; createdAt?: string }[];
  activities?: {
    id: string;
    actor: string;
    summary: string;
    occurredAt?: string;
  }[];
};
export type Board = {
  board: string;
  total?: number;
  active?: number;
  blocked?: number;
  verifying?: number;
  released?: number;
  closed?: number;
};
export type Project = {
  id: string;
  name: string;
  board: string;
  owner: string;
  description?: string;
};
export type PlanningRecord = {
  id: string;
  projectId: string;
  name: string;
  version?: string;
  status?: string;
  targetDate?: string;
  startDate?: string;
  endDate?: string;
};
export type Planning = {
  releases: PlanningRecord[];
  sprints: PlanningRecord[];
  milestones: PlanningRecord[];
};
export type ItemFilter = {
  projectId: string;
  owner: string;
  status: string;
  kind: string;
  query: string;
  releaseId?: string;
  sprintId?: string;
  milestoneId?: string;
};
export type SavedView = {
  id: string;
  name: string;
  filter?: Partial<ItemFilter>;
};
export type Notification = {
  deliveryId: string;
  channel: string;
  title?: string;
  eventType?: string;
  subject?: string;
  body?: string;
  deliveredAt?: string;
  createdAt?: string;
};
export type MemberWeek = {
  member: string;
  weekStart: string;
  weekEnd: string;
  items?: WorkItem[];
};
export type Progress = {
  progressPercent: number;
  totalItems: number;
  completedItems: number;
  totalEstimatePoints?: number;
  completedEstimatePoints?: number;
};
export type Schedule = {
  totalItems?: number;
  scheduledItems?: number;
  overdueItems?: number;
  unscheduledItems?: number;
  blockedItems?: number;
  dependencyBlockedItems?: number;
  asOfDate?: string;
  risks?: { itemId: string; title: string; reason: string; dueDate?: string }[];
  capacity?: {
    owner: string;
    remainingEstimatePoints: number;
    blockedItems?: number;
    totalItems?: number;
  }[];
};
export type Mutation = (
  id: string,
  input: Record<string, unknown>,
  revision?: number,
) => Promise<unknown>;
export type CreateCommand = (
  input: Record<string, unknown>,
) => Promise<unknown>;
export type RequestFailure = Error & { status?: number; traceId?: string };
export const EMPTY_PLANNING: Planning = {
  releases: [],
  sprints: [],
  milestones: [],
};
export const EMPTY_FILTER: ItemFilter = {
  projectId: "",
  owner: "",
  status: "",
  kind: "",
  query: "",
};
export const BOARD_NAMES = [
  "设备质量与连接",
  "产品与平台能力",
  "研发交付效能",
  "运营保障与安全",
  "客户与业务价值",
] as const;
export const GATES = [
  "planning",
  "solution_reviewed",
  "development_completed",
  "test_passed",
  "production_validated",
] as const;
export const GATE_LABELS: Record<string, string> = {
  planning: "规划确认",
  solution_reviewed: "方案评审",
  development_completed: "研发完成",
  test_passed: "测试通过",
  production_validated: "生产验证",
};
export const STATUS_LABELS: Record<string, string> = {
  planned: "待推进",
  in_progress: "进行中",
  blocked: "受阻",
  verifying: "验证中",
  released: "已发布",
  closed: "已复盘关闭",
};
export const KIND_LABELS: Record<string, string> = {
  epic: "Epic",
  task: "任务",
  subtask: "子任务",
  defect: "缺陷",
};
export const gateText = (gate?: string) =>
  GATE_LABELS[gate ?? ""] ?? "未知关卡";
export const kindText = (kind?: string) =>
  KIND_LABELS[kind ?? "task"] ?? kind ?? "任务";
export function upcomingGate(gate?: string): string | null {
  const index = GATES.indexOf(gate as (typeof GATES)[number]);
  return index < 0 || index === GATES.length - 1 ? null : GATES[index + 1];
}
export function safeReference(value?: string): string | undefined {
  if (!value) return undefined;
  try {
    const url = new URL(value);
    return ["https:", "http:"].includes(url.protocol) ? url.href : undefined;
  } catch {
    return undefined;
  }
}
export function displayDate(value?: string) {
  return value ? value.slice(0, 10) : "未排期";
}
