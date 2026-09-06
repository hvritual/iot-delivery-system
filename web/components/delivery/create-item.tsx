"use client";
import { useRef, useState, type FormEvent } from "react";
import { ArrowLeft, ArrowRight, CopyCheck, X } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { Action, Chip, Failure, FieldGroup, FormField, Notice } from "./ui";
import {
  BOARD_NAMES,
  KIND_LABELS,
  EMPTY_PLANNING,
  type Planning,
  type Project,
  type CreateCommand,
  type RequestFailure,
} from "./model";

type Candidate = { id: string; title: string; score: number };
type Form = {
  title: string;
  board: string;
  projectId: string;
  parentId: string;
  kind: string;
  type: string;
  owner: string;
  priority: string;
  releaseId: string;
  sprintId: string;
  milestoneId: string;
  startDate: string;
  dueDate: string;
  estimatePoints: string;
  dependenciesText: string;
  plan: string;
  solution: string;
};
const initial: Form = {
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
};
export function CreateItemDialog({
  projects = [],
  releases = EMPTY_PLANNING.releases,
  sprints = EMPTY_PLANNING.sprints,
  milestones = EMPTY_PLANNING.milestones,
  onCreate,
  onCheckSimilar,
  onClose,
}: {
  projects?: Project[];
  releases?: Planning["releases"];
  sprints?: Planning["sprints"];
  milestones?: Planning["milestones"];
  onCreate: CreateCommand;
  onCheckSimilar?: (input: Record<string, unknown>) => Promise<Candidate[]>;
  onClose: () => void;
}) {
  const [form, setForm] = useState<Form>(initial);
  const [step, setStep] = useState<"basics" | "planning" | "similarity">(
    "basics",
  );
  const [busy, setBusy] = useState(false);
  const lock = useRef(false);
  const [error, setError] = useState<RequestFailure | string | null>(null);
  const [candidates, setCandidates] = useState<Candidate[]>([]);
  const [checkedInput, setCheckedInput] = useState<Record<
    string,
    unknown
  > | null>(null);
  const dirty = Object.keys(initial).some(
    (k) => form[k as keyof Form] !== initial[k as keyof Form],
  );
  function change(patch: Partial<Form>) {
    setError(null);
    setForm((f) => ({ ...f, ...patch }));
    setCandidates([]);
    setCheckedInput(null);
  }
  function close() {
    if (busy) return;
    if (!dirty || window.confirm("尚有未创建的事项内容，确定放弃吗？"))
      onClose();
  }
  const choices = (list: Planning["releases"]) => [
    { value: "", label: "未关联" },
    ...list
      .filter((p) => p.projectId === form.projectId)
      .map((p) => ({
        value: p.id,
        label: p.version ? `${p.version} · ${p.name}` : p.name,
      })),
  ];
  function input() {
    const { dependenciesText, estimatePoints, ...value } = form;
    if (!value.title.trim() || !value.owner.trim())
      throw new Error("请填写事项名称和负责人。");
    if (value.startDate && value.dueDate && value.dueDate < value.startDate)
      throw new Error("目标日期不能早于开始日期。");
    if (
      estimatePoints !== "" &&
      (!Number.isFinite(Number(estimatePoints)) || Number(estimatePoints) < 0)
    )
      throw new Error("估算点必须为非负数。");
    return {
      ...value,
      title: value.title.trim(),
      owner: value.owner.trim(),
      ...(estimatePoints !== ""
        ? { estimatePoints: Number(estimatePoints) }
        : {}),
      dependencies: [
        ...new Set(
          dependenciesText
            .split(/[,，\n]/)
            .map((v) => v.trim())
            .filter(Boolean),
        ),
      ].map((itemId) => ({ itemId, relation: "depends_on" })),
    };
  }
  async function submit(event?: FormEvent, confirmed = false) {
    event?.preventDefault();
    if (lock.current) return;
    setError("");
    if (step === "basics") {
      try {
        input();
        setStep("planning");
      } catch (e) {
        setError((e as Error).message);
      }
      return;
    }
    lock.current = true;
    setBusy(true);
    try {
      const value = confirmed ? checkedInput : input();
      if (!value) throw new Error("请重新执行相似事项检查。");
      if (!confirmed && onCheckSimilar) {
        const matches = await onCheckSimilar(value);
        if (matches?.length) {
          setCandidates(matches);
          setCheckedInput(value);
          setStep("similarity");
          return;
        }
      }
      const result = await onCreate(value);
      if (result) onClose();
      else setError("事项尚未创建，已保留输入，请检查服务端返回的信息。");
    } catch (cause) {
      setError(cause instanceof Error ? cause : "检查或创建未完成。");
    } finally {
      lock.current = false;
      setBusy(false);
    }
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) close();
      }}
    >
      <DialogContent
        showCloseButton={false}
        className="modal create-item-modal"
        style={{ "--modal-width": "760px" } as React.CSSProperties}
      >
        <header className="modal-head">
          <div>
            <DialogTitle>新建交付事项</DialogTitle>
            <DialogDescription>
              将目标、负责人和交付范围明确到一个工作对象。
            </DialogDescription>
          </div>
          <button
            className="icon-button"
            aria-label="关闭"
            disabled={busy}
            onClick={close}
          >
            <X className="icon" />
          </button>
        </header>
        <form
          onSubmit={(e) => void submit(e)}
          className="modal-form"
          aria-label="新建交付事项"
        >
          <div className="modal-body">
            <nav className="step-tabs" aria-label="创建步骤">
              <button
                type="button"
                className={step === "basics" ? "active" : ""}
                disabled={busy}
                onClick={() => setStep("basics")}
              >
                基本信息
              </button>
              <button
                type="button"
                className={step === "planning" ? "active" : ""}
                disabled={busy}
                onClick={() => {
                  try {
                    input();
                    setStep("planning");
                  } catch (e) {
                    setError((e as Error).message);
                  }
                }}
              >
                规划与排期
              </button>
              {step === "similarity" ? (
                <span className="active">相似事项确认</span>
              ) : null}
            </nav>
            <Failure error={error} />
            <div hidden={step !== "basics"}>
              <FieldGroup className="form-grid">
                <FormField
                  className="span-2"
                  label="事项名称"
                  required
                  disabled={busy}
                  value={form.title}
                  onChange={(e) => change({ title: e.target.value })}
                />
                <FormField
                  label="所属项目"
                  disabled={busy}
                  options={[
                    { value: "", label: "未归属项目（兼容旧事项）" },
                    ...projects.map((p) => ({ value: p.id, label: p.name })),
                  ]}
                  value={form.projectId}
                  onChange={(e) =>
                    change({
                      projectId: e.target.value,
                      releaseId: "",
                      sprintId: "",
                      milestoneId: "",
                    })
                  }
                />
                <FormField
                  label="事项类型"
                  disabled={busy}
                  options={Object.entries(KIND_LABELS).map(
                    ([value, label]) => ({ value, label }),
                  )}
                  value={form.kind}
                  onChange={(e) => change({ kind: e.target.value })}
                />
                <FormField
                  label="所属板块"
                  disabled={busy}
                  options={BOARD_NAMES.map((b) => ({ value: b, label: b }))}
                  value={form.board}
                  onChange={(e) => change({ board: e.target.value })}
                />
                <FormField
                  label="负责人"
                  required
                  disabled={busy}
                  value={form.owner}
                  onChange={(e) => change({ owner: e.target.value })}
                />
                <FormField
                  label="优先级"
                  disabled={busy}
                  options={[
                    { value: "P0", label: "P0 · 最高" },
                    { value: "P1", label: "P1 · 重要" },
                    { value: "P2", label: "P2 · 常规" },
                  ]}
                  value={form.priority}
                  onChange={(e) => change({ priority: e.target.value })}
                />
                <FormField
                  label="父事项 ID（可选）"
                  disabled={busy}
                  value={form.parentId}
                  onChange={(e) => change({ parentId: e.target.value })}
                />
                <FormField
                  label="开始日期"
                  disabled={busy}
                  type="date"
                  value={form.startDate}
                  onChange={(e) => change({ startDate: e.target.value })}
                />
                <FormField
                  label="目标日期"
                  disabled={busy}
                  type="date"
                  value={form.dueDate}
                  onChange={(e) => change({ dueDate: e.target.value })}
                />
              </FieldGroup>
            </div>
            <div hidden={step !== "planning"}>
              <FieldGroup className="form-grid">
                <FormField
                  label="估算点"
                  disabled={busy}
                  type="number"
                  min={0}
                  step={0.5}
                  value={form.estimatePoints}
                  onChange={(e) => change({ estimatePoints: e.target.value })}
                />
                <FormField
                  label="发布版本"
                  disabled={busy || !form.projectId}
                  options={choices(releases)}
                  value={form.releaseId}
                  onChange={(e) => change({ releaseId: e.target.value })}
                />
                <FormField
                  label="Sprint"
                  disabled={busy || !form.projectId}
                  options={choices(sprints)}
                  value={form.sprintId}
                  onChange={(e) => change({ sprintId: e.target.value })}
                />
                <FormField
                  label="里程碑"
                  disabled={busy || !form.projectId}
                  options={choices(milestones)}
                  value={form.milestoneId}
                  onChange={(e) => change({ milestoneId: e.target.value })}
                />
                <FormField
                  className="span-2"
                  label="依赖事项 ID（逗号分隔）"
                  disabled={busy}
                  value={form.dependenciesText}
                  onChange={(e) => change({ dependenciesText: e.target.value })}
                />
                <FormField
                  className="span-2"
                  label="规划"
                  disabled={busy}
                  multiline
                  value={form.plan}
                  onChange={(e) => change({ plan: e.target.value })}
                  hint="目标、成功标准与边界"
                />
                <FormField
                  className="span-2"
                  label="初始方案"
                  disabled={busy}
                  multiline
                  value={form.solution}
                  onChange={(e) => change({ solution: e.target.value })}
                />
              </FieldGroup>
            </div>
            {step === "similarity" ? (
              <div>
                <Notice
                  title={`发现 ${candidates.length} 条相似事项`}
                  tone="warning"
                  icon={CopyCheck}
                >
                  请先核对是否已有工作覆盖本目标；不自动合并，也不会静默创建重复事项。
                </Notice>
                <div className="similarity-list">
                  {candidates.map((c) => (
                    <article key={c.id}>
                      <div>
                        <strong>{c.title}</strong>
                        <p className="caption mono">{c.id}</p>
                      </div>
                      <Chip>{Math.round(c.score * 100)}% 相似</Chip>
                    </article>
                  ))}
                </div>
                <p className="caption">
                  确认仅适用于刚才检查的内容，不豁免服务端完全重复校验。返回修改后需重新检查。
                </p>
              </div>
            ) : null}
          </div>
          <footer className="modal-foot">
            <Action onClick={close} disabled={busy}>
              取消
            </Action>
            {step !== "basics" ? (
              <Action
                icon={ArrowLeft}
                disabled={busy}
                onClick={() =>
                  setStep(step === "similarity" ? "planning" : "basics")
                }
              >
                返回修改
              </Action>
            ) : null}
            {step === "similarity" ? (
              <Action
                primary
                busy={busy}
                onClick={() => void submit(undefined, true)}
              >
                已核对，仍然创建
              </Action>
            ) : (
              <Action primary type="submit" busy={busy} icon={ArrowRight}>
                {step === "basics" ? "规划与排期" : "检查相似事项并创建"}
              </Action>
            )}
          </footer>
        </form>
      </DialogContent>
    </Dialog>
  );
}
