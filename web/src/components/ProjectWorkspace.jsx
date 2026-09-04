import { useState } from "react";

import { boardOrder } from "../lib/presentation.mjs";
import { projectProgressLabel } from "../lib/r2-presentation.mjs";

export function ProjectWorkspace({ activeProjectId, onCreateMilestone, onCreateProject, onCreateRelease, onCreateSprint, onSelectProject, planning, progress, projects }) {
  const [projectForm, setProjectForm] = useState({ name: "", board: "研发交付效能", owner: "" });
  const [releaseForm, setReleaseForm] = useState({ name: "", version: "", targetDate: "" });
  const [sprintForm, setSprintForm] = useState({ name: "", startDate: "", endDate: "" });
  const [milestoneForm, setMilestoneForm] = useState({ name: "", targetDate: "" });
  const [saving, setSaving] = useState("");

  const activeProject = projects.find((project) => project.id === activeProjectId) ?? null;

  async function submitProject(event) {
    event.preventDefault();
    setSaving("project");
    try {
      const created = await onCreateProject(projectForm);
      if (created) setProjectForm({ name: "", board: "研发交付效能", owner: "" });
    } finally {
      setSaving("");
    }
  }

  async function submitPlanning(event, kind) {
    event.preventDefault();
    if (!activeProjectId) return;
    setSaving(kind);
    try {
      if (kind === "release") {
        const created = await onCreateRelease({ ...releaseForm, projectId: activeProjectId });
        if (created) setReleaseForm({ name: "", version: "", targetDate: "" });
      }
      if (kind === "sprint") {
        const created = await onCreateSprint({ ...sprintForm, projectId: activeProjectId });
        if (created) setSprintForm({ name: "", startDate: "", endDate: "" });
      }
      if (kind === "milestone") {
        const created = await onCreateMilestone({ ...milestoneForm, projectId: activeProjectId });
        if (created) setMilestoneForm({ name: "", targetDate: "" });
      }
    } finally {
      setSaving("");
    }
  }

  return (
    <section className="project-workspace" aria-label="项目、版本与排期">
      <div className="section-header">
        <div>
          <span className="eyebrow">项目交付空间</span>
          <h2>{activeProject?.name ?? "选择或创建项目"}</h2>
        </div>
        <div className="project-progress-pill">{projectProgressLabel(progress)}</div>
      </div>
      <div className="project-selector-row">
        <label>
          当前项目
          <select onChange={(event) => onSelectProject(event.target.value)} value={activeProjectId}>
            <option value="">全部项目 / 未归属事项</option>
            {projects.map((project) => <option key={project.id} value={project.id}>{project.name} · {project.owner}</option>)}
          </select>
        </label>
        {activeProject ? <p>{activeProject.id} · {activeProject.board} · {activeProject.owner}</p> : <p>项目可承载版本、Sprint、里程碑、Epic 与可交付事项。</p>}
      </div>
      <div className="project-forms">
        <form onSubmit={submitProject}>
          <h3>新建项目</h3>
          <input onChange={(event) => setProjectForm((current) => ({ ...current, name: event.target.value }))} placeholder="项目名称" required value={projectForm.name} />
          <input onChange={(event) => setProjectForm((current) => ({ ...current, owner: event.target.value }))} placeholder="项目负责人" required value={projectForm.owner} />
          <select onChange={(event) => setProjectForm((current) => ({ ...current, board: event.target.value }))} value={projectForm.board}>
            {boardOrder.map((board) => <option key={board} value={board}>{board}</option>)}
          </select>
          <button className="secondary-button" disabled={saving === "project"} type="submit">{saving === "project" ? "创建中…" : "创建项目"}</button>
        </form>
        <form onSubmit={(event) => void submitPlanning(event, "release")}>
          <h3>发布版本</h3>
          <input disabled={!activeProjectId} onChange={(event) => setReleaseForm((current) => ({ ...current, name: event.target.value }))} placeholder="版本名称" required value={releaseForm.name} />
          <input disabled={!activeProjectId} onChange={(event) => setReleaseForm((current) => ({ ...current, version: event.target.value }))} placeholder="版本号，例如 2.8.0" required value={releaseForm.version} />
          <input disabled={!activeProjectId} onChange={(event) => setReleaseForm((current) => ({ ...current, targetDate: event.target.value }))} type="date" value={releaseForm.targetDate} />
          <button className="secondary-button" disabled={!activeProjectId || saving === "release"} type="submit">{saving === "release" ? "创建中…" : "新增版本"}</button>
        </form>
        <form onSubmit={(event) => void submitPlanning(event, "sprint")}>
          <h3>Sprint</h3>
          <input disabled={!activeProjectId} onChange={(event) => setSprintForm((current) => ({ ...current, name: event.target.value }))} placeholder="Sprint 名称" required value={sprintForm.name} />
          <div className="compact-date-row"><input disabled={!activeProjectId} onChange={(event) => setSprintForm((current) => ({ ...current, startDate: event.target.value }))} required type="date" value={sprintForm.startDate} /><input disabled={!activeProjectId} onChange={(event) => setSprintForm((current) => ({ ...current, endDate: event.target.value }))} required type="date" value={sprintForm.endDate} /></div>
          <button className="secondary-button" disabled={!activeProjectId || saving === "sprint"} type="submit">{saving === "sprint" ? "创建中…" : "新增 Sprint"}</button>
        </form>
        <form onSubmit={(event) => void submitPlanning(event, "milestone")}>
          <h3>里程碑</h3>
          <input disabled={!activeProjectId} onChange={(event) => setMilestoneForm((current) => ({ ...current, name: event.target.value }))} placeholder="里程碑名称" required value={milestoneForm.name} />
          <input disabled={!activeProjectId} onChange={(event) => setMilestoneForm((current) => ({ ...current, targetDate: event.target.value }))} required type="date" value={milestoneForm.targetDate} />
          <button className="secondary-button" disabled={!activeProjectId || saving === "milestone"} type="submit">{saving === "milestone" ? "创建中…" : "新增里程碑"}</button>
        </form>
      </div>
      {activeProject ? (
        <div className="planning-chips">
          <span>版本：{planning.releases.filter((value) => value.projectId === activeProjectId).map((value) => value.version).join("、") || "未创建"}</span>
          <span>Sprint：{planning.sprints.filter((value) => value.projectId === activeProjectId).map((value) => value.name).join("、") || "未创建"}</span>
          <span>里程碑：{planning.milestones.filter((value) => value.projectId === activeProjectId).map((value) => value.name).join("、") || "未创建"}</span>
        </div>
      ) : null}
    </section>
  );
}
