import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { tmpdir } from "node:os";
import { launchBrowser } from "./cdp-browser.mjs";
import { loginThroughUI, assert } from "./browser-actions.mjs";
import { uiActions, pause } from "./ui-replica-actions.mjs";
import { verifyUIAccountFlows } from "./ui-replica-account.mjs";

export async function runUIReplicaScenario({ fixture, webBase, stopBackend }) {
  const output = path.resolve(
    process.env.IOT_DELIVERY_UI_EVIDENCE ||
      path.join(tmpdir(), "iotd-ui-evidence"),
  );
  await mkdir(output, { recursive: true });
  const browser = await launchBrowser();
  const admin = await browser.createContext();
  const reviewer = await browser.createContext();
  const report = {
    mode: "real-production-build-real-yunka",
    viewport: [1366, 768],
    checks: [],
    screens: [],
    consoleErrors: [],
    complete: false,
  };
  const ui = uiActions(admin, output, report);
  const ru = uiActions(reviewer, output, report);
  browser.client.socket.addEventListener("message", (event) => {
    const entry = JSON.parse(String(event.data));
    if (entry.method === "Runtime.exceptionThrown")
      report.consoleErrors.push(
        entry.params.exceptionDetails?.text || "Runtime exception",
      );
    if (entry.method === "Page.javascriptDialogOpening") {
      // The scenario intentionally accepts discard/destructive confirmation steps.
      void browser.client.send(
        "Page.handleJavaScriptDialog",
        { accept: true },
        entry.sessionId,
      );
    }
  });
  const ok = (label) => {
    report.checks.push(label);
    console.log(`UI check PASS: ${label}`);
  };
  try {
    for (const context of [admin, reviewer])
      await context.client.send(
        "Emulation.setDeviceMetricsOverride",
        { width: 1366, height: 768, deviceScaleFactor: 1, mobile: false },
        context.sessionId,
      );
    await admin.navigate(webBase);
    await admin.waitFor(
      `!!document.querySelector('form[aria-label="本地成员登录"]')`,
    );
    await ui.shot("01-login");
    await loginThroughUI(
      admin,
      webBase,
      fixture.organizationId,
      fixture.adminUserId,
      fixture.adminPassword,
      "YU-29 System Administrator",
    );
    await ui.text("每日交付概况");
    await ui.click("通知收件箱", ".sidebar");
    await ui.text("尚无投递记录");
    await ui.shot("57-state-inbox-empty");
    ok("real local login and empty inbox");

    const boards = [
      "设备质量与连接",
      "产品与平台能力",
      "研发交付效能",
      "运营保障与安全",
      "客户与业务价值",
    ];
    const names = [
      "连接稳定性专项",
      "租户生命周期管理",
      "IoT 交付工作台",
      "发布与安全治理",
      "客户直连协议交付",
    ];
    const projects = [];
    for (let i = 0; i < boards.length; i++)
      projects.push(
        await ui.request(
          "/api/projects",
          {
            method: "POST",
            body: {
              name: names[i],
              board: boards[i],
              owner: fixture.adminUserId,
            },
          },
          201,
        ),
      );
    const project = projects[0];
    const release = await ui.request(
      "/api/releases",
      {
        method: "POST",
        body: {
          projectId: project.id,
          name: "连接稳定性发布",
          version: "2.8.0",
          targetDate: "2026-09-18",
        },
      },
      201,
    );
    const sprint = await ui.request(
      "/api/sprints",
      {
        method: "POST",
        body: {
          projectId: project.id,
          name: "Sprint 18",
          startDate: "2026-09-07",
          endDate: "2026-09-18",
        },
      },
      201,
    );
    const milestone = await ui.request(
      "/api/milestones",
      {
        method: "POST",
        body: {
          projectId: project.id,
          name: "首批灰度验收",
          targetDate: "2026-09-18",
        },
      },
      201,
    );
    const titles = [
      ["4G 弱网重连与订单补报", "DNS 失败切换回归验证", "现场离线样本复盘"],
      ["租户停用与恢复状态机", "角色权限契约评审", "成员生命周期回归"],
      ["单次交付操作回执与回读", "保存视图与协作体验", "项目排期风险解释"],
      ["发布前安全证据检查", "过期凭据撤销验证", "异常访问审计回归"],
      ["标准协议客户接入", "联调证据与验收整理", "客户现场问题闭环"],
    ];
    const items = [];
    for (let b = 0; b < 5; b++)
      for (let n = 0; n < 3; n++) {
        let item = await ui.request(
          "/api/items",
          {
            method: "POST",
            body: {
              title: titles[b][n],
              projectId: projects[b].id,
              board: boards[b],
              kind: n === 2 ? "defect" : "task",
              type: "delivery",
              owner: fixture.adminUserId,
              priority: n === 0 ? "P1" : "P2",
              startDate: "2026-09-01",
              dueDate: n === 2 ? "2026-09-03" : "2026-09-18",
              estimatePoints: 3 + n,
              plan: "明确交付边界，保留网络中间层，按设备样本验证结果。",
              solution: "先验证失败路径，再提交回归与生产验收证据。",
              ...(b === 0
                ? {
                    releaseId: release.id,
                    sprintId: sprint.id,
                    milestoneId: milestone.id,
                  }
                : {}),
            },
          },
          201,
        );
        item = await ui.request(`/api/items/${item.id}`, {
          method: "PATCH",
          body: {
            expectedRevision: item.revision,
            progressPercent: 20 + n * 20,
            ...(n === 2 ? { blocker: "等待现场日志与可复现样本。" } : {}),
          },
        });
        items.push(item);
      }
    let selected = items[0];
    selected = await ui.request(`/api/items/${selected.id}`, {
      method: "PATCH",
      body: {
        expectedRevision: selected.revision,
        iotBindings: [
          {
            kind: "device",
            reference: "SN-LAB-001",
            label: "弱网回归测试机",
            attributes: { site: "lab-a" },
          },
          { kind: "firmware", reference: "fw-2.8.0", label: "候选固件" },
          { kind: "environment", reference: "staging", label: "预发布" },
        ],
        traceLinks: [
          {
            kind: "pull_request",
            reference: "PR-12",
            title: "重连策略改进",
            url: "https://example.invalid/pr/12",
            status: "merged",
            recordedAt: "2026-09-05T08:09:10Z",
          },
        ],
      },
    });
    const binding = await ui.request(
      "/auth/local/admin/project-role-bindings",
      {
        method: "POST",
        body: {
          projectId: project.id,
          userId: fixture.memberUserId,
          roleId: "release-approver",
        },
      },
      201,
    );
    await loginThroughUI(
      reviewer,
      webBase,
      fixture.organizationId,
      fixture.memberUserId,
      fixture.memberPassword,
      "YU-29 Ordinary Member",
    );
    await ui.click("交付驾驶舱", ".sidebar");
    await ui.click("刷新数据", ".topbar");
    await admin.waitFor(`Array.from(document.querySelectorAll('.metric strong')).some(el => el.textContent.trim() === "15")`);
    await ui.shot("02-cockpit");
    ok(
      "fixtures persisted through existing authenticated APIs; no client mock data",
    );

    for (let i = 0; i < boards.length; i++) {
      await ui.click(boards[i], ".sidebar");
      await ui.text(titles[i][0]);
      await ui.shot(
        [
          "03-board-device",
          "04-board-platform",
          "05-board-delivery",
          "06-board-security",
          "07-board-customer",
        ][i],
      );
    }
    await ui.click("交付事项", ".sidebar");
    await ui.shot("09-items-split");
    await ui.click("列表", ".list-toolbar");
    await ui.shot("08-items-all");
    await ui.click("筛选", ".list-toolbar");
    await ui.shot("10-filters-views");
    await ui.field("视图名称", "连接稳定性验收视图");
    await ui.click("保存视图");
    await ui.text("连接稳定性验收视图");
    const saved = await ui.request("/api/views");
    assert(
      saved.some((v) => v.name === "连接稳定性验收视图"),
      "saved view must persist",
    );
    await ui.click("筛选", ".list-toolbar");
    // The search uses a real field; the resulting empty state must not be a failure screen.
    await admin.evaluate(
      `(() => {const input=document.querySelector('input[aria-label="搜索交付事项"]');Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(input,'__no_such_delivery__');input.dispatchEvent(new Event('input',{bubbles:true}));})()`,
    );
    await pause(100);
    await ui.shot("56-state-no-results");
    await admin.evaluate(
      `(() => {const input=document.querySelector('input[aria-label="搜索交付事项"]');Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(input,'');input.dispatchEvent(new Event('input',{bubbles:true}));})()`,
    );
    await ui.text(titles[0][0]);
    await ui.click(titles[0][0], ".list-main");
    await ui.text("交付概况");
    await ui.shot("16-item-overview");
    for (const [tab, name] of [
      ["规划与方案", "17-item-plan-solution"],
      ["决策", "18-item-decisions"],
      ["排期与依赖", "19-item-schedule-dependencies"],
      ["IoT 范围", "20-item-iot-bindings"],
      ["研发关联", "21-item-trace-links"],
      ["评论与活动", "22-item-comments-activity"],
    ]) {
      await ui.click(tab, 'nav[aria-label="事项详情页签"]');
      await ui.shot(name);
    }
    await ui.field("补充评论", "已补充弱网验证记录，下一步提交方案评审。");
    await ui.click("新增评论", ".document");
    await ui.text("已补充弱网验证记录，下一步提交方案评审。");
    let returned = await ui.request(`/api/items/${selected.id}`);
    assert(
      returned.comments?.some((c) => c.body.includes("已补充弱网验证记录")),
      "comment persistence",
    );
    ok("saved views, search and seven detail tabs use real projections");

    // Keep a dirty plan while another real request updates the same revision.
    await ui.click("规划与方案", 'nav[aria-label="事项详情页签"]');
    await ui.field("规划", "未提交的计划草稿必须保留");
    await ui.request(`/api/items/${selected.id}`, {
      method: "PATCH",
      body: {
        expectedRevision: returned.revision,
        solution: "来自另一个编辑者的更新",
      },
    });
    await ui.click("保存交付上下文", ".document");
    await ui.text("409");
    await ui.shot("55-state-revision-conflict");
    const draft = await admin.evaluate(
      `Array.from(document.querySelectorAll('textarea')).some(el=>el.value==='未提交的计划草稿必须保留')`,
    );
    assert(draft, "CAS failure preserves plan draft");
    await ui.click("交付事项", ".sidebar");
    await ui.click("刷新数据", ".topbar");
    await ui.click(titles[0][0], ".list-main");
    ok("real stale revision rejects write and preserves user draft");

    for (const [gate, label, name] of [
      ["solution_reviewed", "方案评审", "23-gate-solution"],
      ["development_completed", "研发完成", "24-gate-development"],
      ["test_passed", "测试通过", "25-gate-test"],
    ]) {
      await ui.click(`提交${label}证据`, ".inspector");
      await ui.field("证据标题", `${label} · 弱网回归验收记录`);
      await ui.field(
        "证据引用",
        "https://example.invalid/evidence/weak-network",
      );
      await ui.shot(name);
      await ui.click(`提交${label}证据`, ".document");
      await ui.text("交付概况");
      returned = await ui.request(`/api/items/${selected.id}`);
      assert(returned.gate === gate, `${label} actual gate receipt`);
    }
    await ui.click("提交生产验证证据", ".inspector");
    await ui.field("证据标题", "同一实现者的生产验证应被拒绝");
    await ui.shot("26-gate-production");
    await ui.click("提交生产验证证据", ".document");
    await ui.text("403");
    await ui.shot("54-state-forbidden");
    returned = await ui.request(`/api/items/${selected.id}`);
    assert(
      returned.gate === "test_passed",
      "denied production review must not mutate",
    );
    await ui.click("交付事项", ".sidebar");
    await ru.click("刷新数据", ".topbar");
    await ru.click("交付事项", ".sidebar");
    await ru.click("列表", ".list-toolbar");
    await ru.text(titles[0][0]);
    await ru.click(titles[0][0], ".list-main");
    await ru.click("提交生产验证证据", ".inspector");
    await ru.field("证据标题", "独立验证者确认灰度样本通过");
    await ru.click("提交生产验证证据", ".document");
    await ru.text("交付概况");
    assert((await ru.request(`/api/items/${selected.id}`)).gate === "production_validated", "independent reviewer UI uses server-scoped items and actual protected gate");
    await ui.click("刷新数据", ".topbar");
    await ui.click(titles[0][0], ".list-main");
    await ui.click("提交复盘并关闭", ".inspector");
    await ui.field(
      "复盘内容",
      "有效做法：复用网络中间层。偏差：现场样本回传较慢。改进：将弱网回归纳入下一版本的验收清单。",
    );
    await ui.shot("27-item-retrospective-close");
    await ui.click("提交复盘并关闭", ".document");
    // Production review and closure both require a reviewer different from the implementer.
    await ui.text("403");
    returned = await ui.request(`/api/items/${selected.id}`);
    assert(returned.status === "released", "implementer close must reject without mutation");
    await ru.click("提交复盘并关闭", ".inspector");
    await ru.field("复盘内容", "独立复盘：保留网络中间层；弱网与补报回归纳入发布前验证。");
    await ru.click("提交复盘并关闭", ".document");
    await ru.text("已复盘关闭");
    // Explicitly leave the rejected draft before reading the committed reviewer result.
    await ui.click("交付事项", ".sidebar");
    await ui.click("刷新数据", ".topbar");
    await admin.waitFor(`Array.from(document.querySelectorAll('.list-main tr')).some(el => el.textContent.includes(${JSON.stringify(titles[0][0])}) && el.textContent.includes('已复盘关闭'))`);
    await ui.click(titles[0][0], ".list-main");
    await ui.click("查看 Obsidian 档案", ".inspector");
    await ui.shot("28-item-closed-archive");
    returned = await ui.request(`/api/items/${selected.id}`);
    assert(
      returned.status === "closed" && returned.retrospective,
      "close requires real validated gate and retrospective",
    );
    ok(
      "all adjacent gates, independent production review, retrospective close and readback",
    );

    await ui.click("项目与排期", ".sidebar");
    await ui.shot("29-projects-planning");
    await ui.click(names[0], ".projects-table");
    await ui.text("负责人剩余估算");
    await ui.shot("30-project-health");
    for (const [tab, name] of [
      ["发布版本", "31-project-release"],
      ["Sprint", "32-project-sprint"],
      ["里程碑", "33-project-milestone"],
      ["事项层级", "34-item-hierarchy"],
    ]) {
      await ui.click(tab, 'nav[aria-label="项目工作区"]');
      await ui.shot(name);
    }
    await ui.click("项目与排期", 'nav[aria-label="项目工作区"]');
    await ui.click("新建项目", ".page");
    await ui.field("项目名称", "UI 回归交付项目");
    await ui.field("项目负责人", fixture.adminUserId);
    await ui.shot("35-create-project");
    await ui.click("创建项目", ".modal");
    await ui.gone(".planning-modal");
    await ui.text("UI 回归交付项目");
    await ui.click(names[0], ".projects-table");
    for (const [tab, button, name, kind] of [
      ["发布版本", "新增发布版本", "36-create-release", "版本"],
      ["Sprint", "新增Sprint", "37-create-sprint", "Sprint"],
      ["里程碑", "新增里程碑", "38-create-milestone", "里程碑"],
    ]) {
      await ui.click(tab, 'nav[aria-label="项目工作区"]');
      await ui.click(button, ".page");
      await ui.field(`${kind}名称`, `${kind}界面创建验证`);
      if (kind === "版本") {
        await ui.field("版本号", "2.8.1");
        await ui.field("目标日期", "2026-09-25");
      } else if (kind === "Sprint") {
        await ui.field("开始日期", "2026-09-21");
        await ui.field("结束日期", "2026-10-02");
      } else await ui.field("目标日期", "2026-09-25");
      await ui.shot(name);
      await ui.click(`创建${kind}`, ".modal");
      await ui.gone(".planning-modal");
      await ui.text(`${kind}界面创建验证`);
    }
    ok(
      "project creation and all three planning creates return persisted records",
    );

    await ui.click("新建交付事项", ".topbar");
    await ui.field("事项名称", "独立的界面创建回归");
    await ui.field("所属项目", project.id);
    await ui.field("负责人", fixture.adminUserId);
    await ui.shot("13-create-item-basics");
    await ui.click("规划与排期", ".modal-foot");
    await ui.field("规划", "验证新界面中的创建、查重与回读路径。");
    await ui.field("估算点", "3");
    await ui.shot("14-create-item-planning");
    await ui.click("检查相似事项并创建", ".modal");
    await ui.gone(".create-item-modal");
    await ui.text("独立的界面创建回归");
    await ui.click("新建交付事项", ".topbar");
    await ui.field("事项名称", "独立的界面创建回归");
    await ui.field("所属项目", project.id);
    await ui.field("负责人", fixture.adminUserId);
    await ui.click("规划与排期", ".modal-foot");
    await ui.click("检查相似事项并创建", ".modal");
    await ui.text("相似事项确认");
    await ui.shot("15-similarity-review");
    await ui.click("已核对，仍然创建", ".modal");
    // Exact duplicates are forbidden by the existing domain service. Similarity
    // confirmation is a review step, not permission to bypass that invariant.
    await ui.text("事项重复，未创建");
    assert(
      await admin.evaluate(`document.querySelector('.create-item-modal')?.textContent.includes('HTTP 409')`),
      "exact duplicate 409 must be visible inside the still-open creation modal",
    );
    await ui.shot("61-state-exact-duplicate");
    const rejected = await ui.request("/api/dashboard");
    assert(
      rejected.items.filter((i) => i.title === "独立的界面创建回归").length === 1,
      "exact duplicate rejection must not insert another item",
    );
    await ui.click("基本信息", ".step-tabs");
    assert(
      await admin.evaluate(`Array.from(document.querySelectorAll('.create-item-modal input')).some(el => el.value === '独立的界面创建回归')`),
      "duplicate rejection retains the original draft",
    );
    await ui.field("事项名称", "独立的界面创建回归补充");
    await ui.click("规划与排期", ".modal-foot");
    await ui.click("检查相似事项并创建", ".modal");
    await ui.text("相似事项确认");
    const unconfirmed = await ui.request("/api/dashboard");
    assert(
      !unconfirmed.items.some((i) => i.title === "独立的界面创建回归补充"),
      "editing the draft invalidates the prior confirmation; new payload waits for review",
    );
    await ui.shot("15-similarity-review");
    await ui.click("已核对，仍然创建", ".modal");
    await ui.gone(".create-item-modal");
    await ui.text("独立的界面创建回归补充");
    const all = await ui.request("/api/dashboard");
    const original = all.items.filter((i) => i.title === "独立的界面创建回归");
    const created = all.items.filter((i) => i.title === "独立的界面创建回归补充");
    assert(
      original.length === 1 && created.length === 1 && original[0].id !== created[0].id,
      "confirmed similar but nonidentical title creates exactly one distinct item",
    );
    ok(
      "exact duplicate rejected with draft retained; edited similar title rechecked, confirmed and persisted exactly once",
    );
    await ui.click("我的本周", ".sidebar");
    await ui.field("成员名称", fixture.adminUserId);
    await ui.field("周起始日期", "2026-09-07");
    await ui.click("查看", ".week-toolbar");
    await ui.text("至");
    await ui.shot("11-member-week");
    await ui.click("通知收件箱", ".sidebar");
    await ui.shot("12-notification-inbox");
    await ui.click("设备质量与连接", ".sidebar");
    await ui.click(titles[0][2], ".list-main");
    await ui.shot("60-state-gate-blocked");
    ok("member week, notification read-only feed and blocked gate state");

    await verifyUIAccountFlows({
      admin,
      reviewer,
      ui,
      ru,
      fixture,
      binding,
      project,
      webBase,
      ok,
    });
    // Simulate an actual unavailable backend, not an injected browser response.
    await stopBackend();
    await admin.navigate(webBase);
    await ui.text("身份服务暂不可用");
    await ui.shot("52-state-auth-unavailable");
    ok("identity availability fails closed when real backend is stopped");
    assert(
      report.consoleErrors.length === 0,
      "no unhandled browser runtime exceptions",
    );
    report.complete = true;
  } catch (error) {
    report.failure = String(error.message);
    // Capture only the UI, never browser storage, network payloads or fixture secrets.
    try {
      const img = await admin.client.send(
        "Page.captureScreenshot",
        { format: "png", captureBeyondViewport: false },
        admin.sessionId,
      );
      await writeFile(
        path.join(output, "failure.png"),
        Buffer.from(img.data, "base64"),
      );
    } catch {}
    throw error;
  } finally {
    await writeFile(
      path.join(output, "ui-report.json"),
      JSON.stringify(report, null, 2),
    );
    await admin.close();
    await reviewer.close();
    await browser.close();
  }
}
