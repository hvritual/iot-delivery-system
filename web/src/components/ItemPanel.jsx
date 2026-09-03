import { useEffect, useMemo, useState } from "react";

import { archiveEntries, gateLabel, gatePosition, nextGate, statusLabel } from "../lib/presentation.mjs";
import { parseIoTBindings, parseTraceLinks, stringifyIoTBindings, stringifyTraceLinks } from "../lib/r2-presentation.mjs";

const gates = ["planning", "solution_reviewed", "development_completed", "test_passed", "production_validated"];

export function ItemPanel({ item, onAddComment, onAdvance, onClose, onUpdateContext, onUpdateItem, planning = { releases: [], sprints: [], milestones: [] } }) {
  const [context, setContext] = useState(emptyContext());
  const [details, setDetails] = useState(emptyDetails());
  const [evidenceTitle, setEvidenceTitle] = useState("");
  const [evidenceReference, setEvidenceReference] = useState("");
  const [retrospective, setRetrospective] = useState("");
  const [comment, setComment] = useState("");
  const [saving, setSaving] = useState("");

  useEffect(() => {
    setContext({
      plan: item?.plan ?? "",
      solution: item?.solution ?? "",
      blocker: item?.blocker ?? "",
      decisionTitle: "",
      decisionOutcome: "",
    });
    setDetails({
      owner: item?.owner ?? "",
      releaseId: item?.releaseId ?? "",
      sprintId: item?.sprintId ?? "",
      milestoneId: item?.milestoneId ?? "",
      startDate: item?.startDate ?? "",
      dueDate: item?.dueDate ?? "",
      estimatePoints: String(item?.estimatePoints ?? 0),
      progressPercent: String(item?.progressPercent ?? 0),
      dependenciesText: (item?.dependencies ?? []).map((dependency) => dependency.itemId).join(", "),
      iotBindingsText: stringifyIoTBindings(item?.iotBindings),
      traceLinksText: stringifyTraceLinks(item?.traceLinks),
    });
    setEvidenceTitle(item ? `${gateLabel(nextGate(item.gate))}证据` : "");
    setEvidenceReference("");
    setRetrospective(item?.retrospective ?? "");
    setComment("");
  }, [item]);

  const projectPlanning = useMemo(() => ({
    releases: planning.releases.filter((value) => !item?.projectId || value.projectId === item.projectId),
    sprints: planning.sprints.filter((value) => !item?.projectId || value.projectId === item.projectId),
    milestones: planning.milestones.filter((value) => !item?.projectId || value.projectId === item.projectId),
  }), [item?.projectId, planning]);

  if (!item) {
    return (
      <aside className="item-panel empty-panel">
        <span className="eyebrow">事项详情</span>
        <h2>选择一条交付事项</h2>
        <p>你可以从中间的任务表进入详情，记录方案、排期、IoT 范围、证据、评论与复盘。</p>
      </aside>
    );
  }

  const upcomingGate = nextGate(item.gate);

  async function saveContext(event) {
    event.preventDefault();
    setSaving("context");
    try {
      await onUpdateContext(item.id, {
        plan: context.plan,
        solution: context.solution,
        blocker: context.blocker,
        ...(context.decisionTitle.trim()
          ? { decision: { title: context.decisionTitle, outcome: context.decisionOutcome || "已记录，待补充结论。" } }
          : {}),
      });
      setContext((current) => ({ ...current, decisionTitle: "", decisionOutcome: "" }));
    } finally {
      setSaving("");
    }
  }

  async function saveDetails(event) {
    event.preventDefault();
    setSaving("details");
    try {
      const dependencies = details.dependenciesText.split(",").map((value) => value.trim()).filter(Boolean).map((itemId) => ({ itemId, relation: "depends_on" }));
      await onUpdateItem(item.id, {
        owner: details.owner,
        releaseId: details.releaseId,
        sprintId: details.sprintId,
        milestoneId: details.milestoneId,
        startDate: details.startDate,
        dueDate: details.dueDate,
        estimatePoints: Number(details.estimatePoints || 0),
        progressPercent: Number(details.progressPercent || 0),
        dependencies,
        iotBindings: parseIoTBindings(details.iotBindingsText),
        traceLinks: parseTraceLinks(details.traceLinksText),
      });
    } finally {
      setSaving("");
    }
  }

  async function submitComment(event) {
    event.preventDefault();
    if (!comment.trim()) return;
    setSaving("comment");
    try {
      await onAddComment(item.id, comment.trim());
      setComment("");
    } finally {
      setSaving("");
    }
  }

  async function submitGate(event) {
    event.preventDefault();
    if (!upcomingGate) return;
    setSaving("gate");
    try {
      await onAdvance(item.id, upcomingGate, { title: evidenceTitle, reference: evidenceReference });
    } finally {
      setSaving("");
    }
  }

  async function submitRetrospective(event) {
    event.preventDefault();
    setSaving("close");
    try {
      await onClose(item.id, retrospective);
    } finally {
      setSaving("");
    }
  }

  return (
    <aside className="item-panel">
      <div className="panel-heading">
        <div>
          <span className="eyebrow">事项详情</span>
          <h2>{item.title}</h2>
          <p>{item.id}{item.kind ? ` · ${item.kind}` : ""}{item.projectId ? ` · ${item.projectId}` : ""}{item.isSample ? " · 本地示例" : ""}</p>
        </div>
        <span className={`status-badge ${item.status}`}>{statusLabel(item.status)}</span>
      </div>

      <section className="gate-track" aria-label="交付关卡">
        {gates.map((gate) => (
          <div className={`gate-step ${gatePosition(item.gate) >= gatePosition(gate) ? "complete" : ""}`} key={gate}>
            <span>{gatePosition(gate)}</span>
            <small>{gateLabel(gate)}</small>
          </div>
        ))}
      </section>

      {item.blocker ? <section className="blocker-callout"><span>受阻原因</span><strong>{item.blocker}</strong></section> : null}

      <form className="context-form" onSubmit={saveContext}>
        <div className="section-title"><h3>规划、方案与决策</h3><span>保存后自动沉淀到 Obsidian</span></div>
        <label>规划<textarea value={context.plan} onChange={(event) => setContext((current) => ({ ...current, plan: event.target.value }))} placeholder="目标、边界、里程碑…" rows="3" /></label>
        <label>方案<textarea value={context.solution} onChange={(event) => setContext((current) => ({ ...current, solution: event.target.value }))} placeholder="技术方案、接口或验证策略…" rows="3" /></label>
        <label>当前阻塞项<input value={context.blocker} onChange={(event) => setContext((current) => ({ ...current, blocker: event.target.value }))} placeholder="为空表示没有阻塞" /></label>
        <div className="decision-fields">
          <label>新增决策标题（可选）<input value={context.decisionTitle} onChange={(event) => setContext((current) => ({ ...current, decisionTitle: event.target.value }))} placeholder="例如：采用分组灰度发布" /></label>
          <label>决策结论<input value={context.decisionOutcome} onChange={(event) => setContext((current) => ({ ...current, decisionOutcome: event.target.value }))} placeholder="结论与后续影响" /></label>
        </div>
        <button className="secondary-button" disabled={saving === "context"} type="submit">{saving === "context" ? "保存中…" : "保存交付上下文"}</button>
      </form>

      <form className="item-details-form" onSubmit={saveDetails}>
        <div className="section-title"><h3>排期、IoT 范围与交付关联</h3><span>字段更新会记入活动审计</span></div>
        <div className="form-grid">
          <label>负责人<input onChange={(event) => setDetails((current) => ({ ...current, owner: event.target.value }))} required value={details.owner} /></label>
          <label>进度 %<input max="100" min="0" onChange={(event) => setDetails((current) => ({ ...current, progressPercent: event.target.value }))} type="number" value={details.progressPercent} /></label>
          <label>开始日期<input onChange={(event) => setDetails((current) => ({ ...current, startDate: event.target.value }))} type="date" value={details.startDate} /></label>
          <label>目标日期<input onChange={(event) => setDetails((current) => ({ ...current, dueDate: event.target.value }))} type="date" value={details.dueDate} /></label>
          <label>估算点<input min="0" onChange={(event) => setDetails((current) => ({ ...current, estimatePoints: event.target.value }))} step="0.5" type="number" value={details.estimatePoints} /></label>
          <label>发布版本<select onChange={(event) => setDetails((current) => ({ ...current, releaseId: event.target.value }))} value={details.releaseId}><option value="">未关联</option>{projectPlanning.releases.map((value) => <option key={value.id} value={value.id}>{value.version} · {value.name}</option>)}</select></label>
          <label>Sprint<select onChange={(event) => setDetails((current) => ({ ...current, sprintId: event.target.value }))} value={details.sprintId}><option value="">未关联</option>{projectPlanning.sprints.map((value) => <option key={value.id} value={value.id}>{value.name}</option>)}</select></label>
          <label>里程碑<select onChange={(event) => setDetails((current) => ({ ...current, milestoneId: event.target.value }))} value={details.milestoneId}><option value="">未关联</option>{projectPlanning.milestones.map((value) => <option key={value.id} value={value.id}>{value.name}</option>)}</select></label>
        </div>
        <label>依赖事项 ID（逗号分隔）<input onChange={(event) => setDetails((current) => ({ ...current, dependenciesText: event.target.value }))} placeholder="IOT-..." value={details.dependenciesText} /></label>
        <label>IoT 绑定（每行：类型 | 引用 | 标签）<textarea onChange={(event) => setDetails((current) => ({ ...current, iotBindingsText: event.target.value }))} placeholder={"device | SN-001 | 测试机\nfirmware | fw-2.8.0 | 固件版本\ncustomer | CUST-01 | 客户\nenvironment | staging | 预发布\nrollout_batch | gray-01 | 首批灰度"} rows="5" value={details.iotBindingsText} /></label>
        <label>研发证据关联（每行：类型 | 引用 | 标题 | URL | 状态）<textarea onChange={(event) => setDetails((current) => ({ ...current, traceLinksText: event.target.value }))} placeholder={"pull_request | PR-12 | 灰度实现 | https://... | merged\nbuild | build-88 | 固件构建 | https://... | success\ntest | test-21 | 回归测试 | https://... | passed\ndefect | BUG-33 | 已知缺陷 | https://... | open\nrelease | rel-2.8.0 | 发布证据 | https://... | released"} rows="6" value={details.traceLinksText} /></label>
        <button className="secondary-button" disabled={saving === "details"} type="submit">{saving === "details" ? "保存中…" : "保存排期与关联"}</button>
      </form>

      {upcomingGate && item.status !== "blocked" ? <form className="gate-action" onSubmit={submitGate}><div><span className="eyebrow">下一关</span><h3>{gateLabel(upcomingGate)}</h3></div><input value={evidenceTitle} onChange={(event) => setEvidenceTitle(event.target.value)} required placeholder="填写评审、测试或验收证据" /><input value={evidenceReference} onChange={(event) => setEvidenceReference(event.target.value)} placeholder="证据引用（例如测试报告、PR 或工单链接）" /><button className="primary-button" disabled={saving === "gate"} type="submit">{saving === "gate" ? "提交中…" : `提交${gateLabel(upcomingGate)}证据`}</button></form> : null}
      {item.gate === "production_validated" && item.status !== "closed" ? <form className="retrospective-form" onSubmit={submitRetrospective}><h3>关闭前复盘</h3><textarea value={retrospective} onChange={(event) => setRetrospective(event.target.value)} required rows="3" placeholder="记录有效做法、偏差和下次改进动作" /><button className="primary-button" disabled={saving === "close"} type="submit">{saving === "close" ? "关闭中…" : "提交复盘并关闭"}</button></form> : null}

      <form className="comments-form" onSubmit={submitComment}>
        <h3>评论与活动审计</h3>
        <textarea onChange={(event) => setComment(event.target.value)} placeholder="补充协作结论、风险或下一步…" rows="2" value={comment} />
        <button className="secondary-button" disabled={saving === "comment"} type="submit">{saving === "comment" ? "提交中…" : "新增评论"}</button>
        {item.comments?.length ? <div className="comments-list">{item.comments.map((value) => <article key={value.id}><strong>{value.author}</strong><p>{value.body}</p></article>)}</div> : <p>尚无评论。</p>}
        {item.activities?.length ? <div className="activities-list">{item.activities.slice().reverse().map((activity) => <span key={activity.id}>{activity.actor} · {activity.summary}</span>)}</div> : null}
      </form>

      <section className="evidence-history"><h3>关卡证据</h3>{item.evidence?.length ? item.evidence.map((evidence, index) => <article key={`${evidence.recordedAt}-${evidence.title}-${index}`}><strong>{evidence.title}</strong><p>{evidence.kind}{evidence.reference ? ` · ${evidence.reference}` : ""}</p></article>) : <p>尚未记录关卡证据。</p>}</section>
      <section className="archive-paths"><h3>Obsidian 档案</h3><p>每次提交会由本地事务 Outbox 投影为以下可追溯笔记。</p><ul>{archiveEntries(item.id).map((entry) => <li key={entry.path}><span>{entry.label}</span><code>{entry.path}</code></li>)}</ul></section>
      <section className="decision-history"><h3>已记录决策</h3>{item.decisions?.length ? item.decisions.map((decision) => <article key={decision.id}><strong>{decision.title}</strong><p>{decision.outcome}</p></article>) : <p>尚未记录决策。</p>}</section>
    </aside>
  );
}

function emptyContext() {
  return { plan: "", solution: "", blocker: "", decisionTitle: "", decisionOutcome: "" };
}

function emptyDetails() {
  return { owner: "", releaseId: "", sprintId: "", milestoneId: "", startDate: "", dueDate: "", estimatePoints: "0", progressPercent: "0", dependenciesText: "", iotBindingsText: "", traceLinksText: "" };
}
