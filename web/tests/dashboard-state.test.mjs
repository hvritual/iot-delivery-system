import assert from "node:assert/strict";
import test from "node:test";

import {
	archiveEntries,
	boardOrder,
	dailyFocus,
	filterItems,
	gatePosition,
	nextGate,
	normalizeDashboard,
} from "../src/lib/presentation.mjs";

test("normalizes the five IoT owner boards even when the API returns one board", () => {
  const dashboard = normalizeDashboard({
    boards: [{ board: "研发交付效能", total: 2, active: 1, blocked: 1 }],
    items: [{ id: "IOT-001", gate: "solution_reviewed" }],
  });

  assert.deepEqual(
    dashboard.boards.map((board) => board.board),
    boardOrder,
  );
  assert.equal(dashboard.boards[2].blocked, 1);
  assert.equal(dashboard.items[0].id, "IOT-001");
});

test("preserves nested IoT and trace DTO fields in the cockpit model", () => {
  const item = {
    id: "IOT-DTO-001",
    iotBindings: [{ kind: "device", reference: "SN-001", label: "测试机", attributes: { site: "lab-a" } }],
    traceLinks: [{ kind: "build", reference: "build-9", title: "固件构建", url: "https://example.test/build-9", status: "passed", recordedAt: "2026-09-05T08:09:10Z" }],
  };

  const dashboard = normalizeDashboard({ items: [item] });
  assert.deepEqual(dashboard.items[0], item);
});

test("maps a delivery gate to a visible sequence and its permitted next gate", () => {
  assert.equal(gatePosition("test_passed"), 4);
  assert.equal(nextGate("test_passed"), "production_validated");
  assert.equal(nextGate("production_validated"), null);
});

test("derives daily blocked, overdue, and verification focus from delivery items", () => {
  const items = [
    { id: "IOT-001", status: "blocked", dueDate: "2026-09-01" },
    { id: "IOT-002", status: "verifying", dueDate: "2026-09-03" },
    { id: "IOT-003", status: "in_progress", dueDate: "2026-09-05" },
    { id: "IOT-004", status: "closed", dueDate: "2026-09-01" },
  ];

  assert.deepEqual(dailyFocus(items, "2026-09-03"), {
    blocked: 1,
    overdue: 1,
    verifying: 1,
  });
  assert.deepEqual(filterItems(items, "overdue", "2026-09-03").map((item) => item.id), ["IOT-001"]);
  assert.deepEqual(filterItems(items, "verification", "2026-09-03").map((item) => item.id), ["IOT-002"]);
});

test("maps an item to the traceable Obsidian archive entries", () => {
  assert.deepEqual(archiveEntries("IOT-001"), [
    { label: "规划", path: "10-交付管理/01-规划/IOT-001-规划.md" },
    { label: "方案", path: "10-交付管理/02-方案/IOT-001-方案.md" },
    { label: "发布与验证", path: "10-交付管理/04-发布与验证/IOT-001-验证.md" },
    { label: "复盘", path: "10-交付管理/05-复盘/IOT-001-复盘.md" },
  ]);
});
