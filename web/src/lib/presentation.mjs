export const boardOrder = [
  "设备质量与连接",
  "产品与平台能力",
  "研发交付效能",
  "运营保障与安全",
  "客户与业务价值",
];

const gateOrder = [
  "planning",
  "solution_reviewed",
  "development_completed",
  "test_passed",
  "production_validated",
];

export function normalizeDashboard(payload = {}) {
  const sourceBoards = new Map((payload.boards ?? []).map((board) => [board.board, board]));
  return {
    boards: boardOrder.map((board) => ({
      board,
      total: 0,
      active: 0,
      blocked: 0,
      verifying: 0,
      released: 0,
      closed: 0,
      ...sourceBoards.get(board),
    })),
    items: Array.isArray(payload.items) ? payload.items : [],
    generatedAt: payload.generatedAt ?? null,
  };
}

export function dailyFocus(items = [], today = todayInChina()) {
  const source = Array.isArray(items) ? items : [];
  return {
    blocked: source.filter((item) => item.status === "blocked").length,
    overdue: source.filter((item) => isOverdue(item, today)).length,
    verifying: source.filter((item) => item.status === "verifying").length,
  };
}

export function filterItems(items = [], focus = "all", today = todayInChina()) {
  const source = Array.isArray(items) ? items : [];
  switch (focus) {
    case "blocked":
      return source.filter((item) => item.status === "blocked");
    case "overdue":
      return source.filter((item) => isOverdue(item, today));
    case "verification":
      return source.filter((item) => item.status === "verifying");
    case "attention":
      return source.filter((item) => item.status === "blocked" || isOverdue(item, today));
    default:
      return source;
  }
}

export function archiveEntries(id) {
  const itemID = String(id ?? "").trim().replace(/[^A-Za-z0-9_-]/g, "");
  if (!itemID) return [];
  return [
    { label: "规划", path: `10-交付管理/01-规划/${itemID}-规划.md` },
    { label: "方案", path: `10-交付管理/02-方案/${itemID}-方案.md` },
    { label: "发布与验证", path: `10-交付管理/04-发布与验证/${itemID}-验证.md` },
    { label: "复盘", path: `10-交付管理/05-复盘/${itemID}-复盘.md` },
  ];
}

export function todayInChina(value = new Date()) {
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone: "Asia/Shanghai",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(value);
  const fields = Object.fromEntries(parts.map((part) => [part.type, part.value]));
  return `${fields.year}-${fields.month}-${fields.day}`;
}

export function gatePosition(gate) {
  const index = gateOrder.indexOf(gate);
  return index === -1 ? 0 : index + 1;
}

export function nextGate(gate) {
  const index = gateOrder.indexOf(gate);
  return index === -1 || index === gateOrder.length - 1 ? null : gateOrder[index + 1];
}

export function gateLabel(gate) {
  return {
    planning: "规划确认",
    solution_reviewed: "方案评审",
    development_completed: "研发完成",
    test_passed: "测试通过",
    production_validated: "生产验证",
  }[gate] ?? "未知关卡";
}

export function statusLabel(status) {
  return {
    planned: "待推进",
    in_progress: "进行中",
    blocked: "受阻",
    verifying: "验证中",
    released: "已发布",
    closed: "已复盘关闭",
  }[status] ?? "未知";
}

export function priorityLabel(priority) {
  return priority || "P1";
}

function isOverdue(item, today) {
  if (item?.status === "closed" || !/^\d{4}-\d{2}-\d{2}$/.test(item?.dueDate ?? "")) return false;
  return item.dueDate < today;
}
