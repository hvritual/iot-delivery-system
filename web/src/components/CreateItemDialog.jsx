import { useMemo, useState } from "react";

import { boardOrder } from "../lib/presentation.mjs";
import { workItemKinds } from "../lib/r2-presentation.mjs";

export function CreateItemDialog({ milestones = [], onCheckSimilar, onClose, onCreate, projects = [], releases = [], sprints = [] }) {
  const [form, setForm] = useState({
    title: "",
    board: "研发交付效能",
    projectId: "",
    parentId: "",
    kind: "task",
    type: "delivery",
    owner: "",
    priority: "P1",
    releaseId: "",
    sprintId: "",
    milestoneId: "",
    startDate: "",
    dueDate: "",
    estimatePoints: "",
    dependenciesText: "",
    plan: "",
    solution: "",
  });
  const [saving, setSaving] = useState(false);
  const [candidates, setCandidates] = useState([]);

  const planning = useMemo(() => ({
    releases: releases.filter((value) => !form.projectId || value.projectId === form.projectId),
    sprints: sprints.filter((value) => !form.projectId || value.projectId === form.projectId),
    milestones: milestones.filter((value) => !form.projectId || value.projectId === form.projectId),
  }), [form.projectId, milestones, releases, sprints]);

  async function submit(event, confirmSimilar = false) {
    event?.preventDefault();
    const input = serializeForm(form);
    setSaving(true);
    try {
      if (!confirmSimilar && onCheckSimilar) {
        const matches = await onCheckSimilar(input);
        if (matches?.length) {
          setCandidates(matches);
          return;
        }
      }
      const created = await onCreate(input);
      if (created) onClose();
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="dialog-backdrop" role="presentation">
      <section aria-labelledby="create-item-title" aria-modal="true" className="create-dialog r2-create-dialog" role="dialog">
        <div className="dialog-heading">
          <div>
            <span className="eyebrow">新建交付事项</span>
            <h2 id="create-item-title">把目标变成可验证的交付</h2>
          </div>
          <button aria-label="关闭" className="icon-button" onClick={onClose} type="button">×</button>
        </div>
        <form onSubmit={submit}>
          <label>
            事项名称
            <input autoFocus onChange={(event) => setForm((current) => ({ ...current, title: event.target.value }))} required value={form.title} />
          </label>
          <div className="form-grid">
            <label>
              所属项目
              <select onChange={(event) => setForm((current) => ({ ...current, projectId: event.target.value, releaseId: "", sprintId: "", milestoneId: "" }))} value={form.projectId}>
                <option value="">未归属项目（旧事项兼容）</option>
                {projects.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}
              </select>
            </label>
            <label>
              事项类型
              <select onChange={(event) => setForm((current) => ({ ...current, kind: event.target.value }))} value={form.kind}>
                {workItemKinds.map((kind) => <option key={kind.value} value={kind.value}>{kind.label}</option>)}
              </select>
            </label>
            <label>
              所属板块
              <select onChange={(event) => setForm((current) => ({ ...current, board: event.target.value }))} value={form.board}>
                {boardOrder.map((board) => <option key={board} value={board}>{board}</option>)}
              </select>
            </label>
            <label>
              负责人
              <input onChange={(event) => setForm((current) => ({ ...current, owner: event.target.value }))} required value={form.owner} />
            </label>
            <label>
              优先级
              <select onChange={(event) => setForm((current) => ({ ...current, priority: event.target.value }))} value={form.priority}>
                <option value="P0">P0 · 最高</option>
                <option value="P1">P1 · 重要</option>
                <option value="P2">P2 · 常规</option>
              </select>
            </label>
            <label>
              父事项 ID（可选）
              <input onChange={(event) => setForm((current) => ({ ...current, parentId: event.target.value }))} placeholder="Epic 或上级任务 ID" value={form.parentId} />
            </label>
            <label>
              开始日期
              <input onChange={(event) => setForm((current) => ({ ...current, startDate: event.target.value }))} type="date" value={form.startDate} />
            </label>
            <label>
              目标日期
              <input onChange={(event) => setForm((current) => ({ ...current, dueDate: event.target.value }))} type="date" value={form.dueDate} />
            </label>
            <label>
              估算点
              <input min="0" onChange={(event) => setForm((current) => ({ ...current, estimatePoints: event.target.value }))} placeholder="例如 3" step="0.5" type="number" value={form.estimatePoints} />
            </label>
            <label>
              发布版本
              <select onChange={(event) => setForm((current) => ({ ...current, releaseId: event.target.value }))} value={form.releaseId}>
                <option value="">未关联</option>
                {planning.releases.map((release) => <option key={release.id} value={release.id}>{release.version} · {release.name}</option>)}
              </select>
            </label>
            <label>
              Sprint
              <select onChange={(event) => setForm((current) => ({ ...current, sprintId: event.target.value }))} value={form.sprintId}>
                <option value="">未关联</option>
                {planning.sprints.map((sprint) => <option key={sprint.id} value={sprint.id}>{sprint.name}</option>)}
              </select>
            </label>
            <label>
              里程碑
              <select onChange={(event) => setForm((current) => ({ ...current, milestoneId: event.target.value }))} value={form.milestoneId}>
                <option value="">未关联</option>
                {planning.milestones.map((milestone) => <option key={milestone.id} value={milestone.id}>{milestone.name}</option>)}
              </select>
            </label>
          </div>
          <label>
            依赖事项 ID（逗号分隔）
            <input onChange={(event) => setForm((current) => ({ ...current, dependenciesText: event.target.value }))} placeholder="例如 IOT-20260903-0001, IOT-20260903-0002" value={form.dependenciesText} />
          </label>
          <label>
            规划
            <textarea onChange={(event) => setForm((current) => ({ ...current, plan: event.target.value }))} placeholder="目标、成功标准与边界" rows="3" value={form.plan} />
          </label>
          <label>
            初始方案
            <textarea onChange={(event) => setForm((current) => ({ ...current, solution: event.target.value }))} placeholder="准备采用的实施与验证方式" rows="3" value={form.solution} />
          </label>
          {candidates.length ? (
            <section className="similarity-callout">
              <strong>发现 {candidates.length} 条相似事项</strong>
              <p>请先确认是否复用或关联，避免重复创建。</p>
              <ul>{candidates.map((candidate) => <li key={candidate.id}>{candidate.id} · {candidate.title}（{Math.round(candidate.score * 100)}%）</li>)}</ul>
              <button className="secondary-button" disabled={saving} onClick={(event) => void submit(event, true)} type="button">已核对，仍然创建</button>
            </section>
          ) : null}
          <div className="dialog-actions">
            <button className="secondary-button" onClick={onClose} type="button">取消</button>
            <button className="primary-button" disabled={saving} type="submit">{saving ? "检查或创建中…" : "检查相似事项并创建"}</button>
          </div>
        </form>
      </section>
    </div>
  );
}

function serializeForm(form) {
  const dependencies = form.dependenciesText
    .split(",")
    .map((value) => value.trim())
    .filter(Boolean)
    .map((itemId) => ({ itemId, relation: "depends_on" }));
  const { dependenciesText, estimatePoints, ...input } = form;
  return {
    ...input,
    ...(estimatePoints !== "" ? { estimatePoints: Number(estimatePoints) } : {}),
    ...(dependencies.length ? { dependencies } : {}),
  };
}
