import assert from "node:assert/strict";
import test from "node:test";

import {
  filterDeliveryItems,
  parseIoTBindings,
  parseTraceLinks,
  projectProgressLabel,
} from "../src/lib/r2-presentation.mjs";

test("filters tasks by project, owner, status, kind, and text", () => {
  const items = [
    { id: "IOT-1", title: "执行 OTA 灰度发布", projectId: "PRJ-1", owner: "张三", status: "in_progress", kind: "task" },
    { id: "IOT-2", title: "处理支付缺陷", projectId: "PRJ-2", owner: "李四", status: "blocked", kind: "defect" },
  ];
  assert.deepEqual(
    filterDeliveryItems(items, { projectId: "PRJ-1", owner: "张三", query: "灰度" }).map((item) => item.id),
    ["IOT-1"],
  );
  assert.deepEqual(filterDeliveryItems(items, { kind: "defect" }).map((item) => item.id), ["IOT-2"]);
});

test("parses structured IoT and engineering evidence associations from editable lines", () => {
  assert.deepEqual(parseIoTBindings("device | SN-001 | 测试机\nfirmware|fw-2.8.0|固件 2.8"), [
    { kind: "device", reference: "SN-001", label: "测试机" },
    { kind: "firmware", reference: "fw-2.8.0", label: "固件 2.8" },
  ]);
  assert.deepEqual(parseTraceLinks("pull_request | PR-12 | 灰度实现 | https://example.test/pr/12 | merged"), [
    { kind: "pull_request", reference: "PR-12", title: "灰度实现", url: "https://example.test/pr/12", status: "merged" },
  ]);
});

test("formats weighted project progress for the project cockpit", () => {
  assert.equal(projectProgressLabel({ progressPercent: 70, completedItems: 1, totalItems: 2 }), "70% · 1/2 项完成");
  assert.equal(projectProgressLabel(null), "暂无项目进度");
});
