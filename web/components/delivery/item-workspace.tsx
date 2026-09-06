"use client";
import { useEffect, useState, type FormEvent } from "react";
import {
  Archive,
  ArrowRight,
  Box,
  CheckCircle,
  ChevronLeft,
  ExternalLink,
  FileText,
  GitBranch,
  Link as LinkIcon,
  MessageSquare,
  Save,
  ShieldCheck,
  TriangleAlert,
} from "lucide-react";
import { archiveEntries } from "@/src/lib/presentation.mjs";
import {
  parseIoTBindings,
  parseTraceLinks,
  stringifyIoTBindings,
  stringifyTraceLinks,
} from "@/src/lib/r2-presentation.mjs";
import {
  Action,
  Chip,
  Failure,
  FieldGroup,
  FormField,
  GateTrack,
  Heading,
  NoData,
  Notice,
  Person,
  Prop,
  Section,
  Status,
} from "./ui";
import {
  EMPTY_PLANNING,
  gateText,
  kindText,
  upcomingGate,
  safeReference,
  displayDate,
  type WorkItem,
  type Planning,
  type Mutation,
  type RequestFailure,
} from "./model";
import { cn } from "@/lib/utils";

type Tab =
  | "overview"
  | "plan"
  | "decisions"
  | "schedule"
  | "iot"
  | "trace"
  | "activity"
  | "gate"
  | "close"
  | "archive";
const tabs: [Tab, string][] = [
  ["overview", "概况"],
  ["plan", "规划与方案"],
  ["decisions", "决策"],
  ["schedule", "排期与依赖"],
  ["iot", "IoT 范围"],
  ["trace", "研发关联"],
  ["activity", "评论与活动"],
];
type Props = {
  item: WorkItem | null;
  planning?: Planning;
  projectName?: string;
  compact?: boolean;
  readOnly?: boolean;
  initialTab?: Tab;
  onExpand?: (tab?: Tab) => void;
  onBack?: () => void;
  onDirtyChange?: (dirty: boolean) => void;
  onUpdateContext: Mutation;
  onUpdateItem: Mutation;
  onAddComment: (
    id: string,
    body: string,
    revision?: number,
  ) => Promise<unknown>;
  onAdvance: (
    id: string,
    gate: string,
    evidence: { title: string; reference: string },
    revision?: number,
  ) => Promise<unknown>;
  onClose: (
    id: string,
    retrospective: string,
    revision?: number,
  ) => Promise<unknown>;
};
export function ItemPanel(props: Props) {
  if (!props.item)
    return (
      <NoData title="选择一条交付事项" icon={FileText}>
        从列表进入详情，记录方案、排期、范围与交付证据。
      </NoData>
    );
  return <ItemContent key={props.item.id} {...props} item={props.item} />;
}
function ItemContent({
  item,
  planning = EMPTY_PLANNING,
  projectName,
  compact,
  initialTab,
  readOnly = false,
  onExpand,
  onBack,
  onDirtyChange,
  ...commands
}: Props & { item: WorkItem }) {
  const [tab, setTab] = useState<Tab>(initialTab || "overview");
  const [dirty, setDirty] = useState<Record<string, boolean>>({});
  const hasDirty = Object.values(dirty).some(Boolean);
  useEffect(() => {
    onDirtyChange?.(hasDirty);
    return () => onDirtyChange?.(false);
  }, [hasDirty, onDirtyChange]);
  useEffect(() => {
    if (!hasDirty) return;
    const guard = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", guard);
    return () => window.removeEventListener("beforeunload", guard);
  }, [hasDirty]);
  const next = upcomingGate(item.gate);
  const locked = readOnly || item.status === "closed";
  const setSectionDirty = (name: string, value: boolean) =>
    setDirty((old) => (old[name] === value ? old : { ...old, [name]: value }));
  const navigateAction = (to: Tab) => {
    if (compact) onExpand?.(to);
    else setTab(to);
  };
  const action =
    item.status === "blocked" ? (
      <Action disabled icon={TriangleAlert}>
        受阻：不可推进
      </Action>
    ) : item.status === "closed" ? (
      <Chip tone="success">已复盘关闭</Chip>
    ) : item.gate === "production_validated" ? (
      <Action
        disabled={readOnly}
        primary
        icon={CheckCircle}
        onClick={() => navigateAction("close")}
      >
        提交复盘并关闭
      </Action>
    ) : next ? (
      <Action
        disabled={readOnly}
        primary
        icon={ArrowRight}
        onClick={() => navigateAction("gate")}
      >
        提交{gateText(next)}证据
      </Action>
    ) : (
      <Action disabled>关卡状态不可用</Action>
    );
  const header = (
    <div className="item-heading">
      <div className="item-id mono">
        {item.id} <span className="muted">/ {kindText(item.kind)}</span>
      </div>
      <div className="title-line">
        <h2>{item.title}</h2>
        {!compact ? <Status value={item.status} /> : null}
      </div>
    </div>
  );
  if (compact)
    return (
      <aside className="peek">
        <header className="peek-header">
          <h3>事项详情</h3>
          <Action
            variant="ghost"
            className="ghost"
            icon={ExternalLink}
            onClick={() => onExpand?.()}
          >
            展开
          </Action>
        </header>
        <div className="peek-body">
          {header}
          <Status value={item.status} />
          <Section title="当前关卡">
            <div className="actions">
              <CheckCircle className="icon" />
              <span>{gateText(item.gate)}</span>
              {next ? (
                <>
                  <ArrowRight className="icon" />
                  <span>{gateText(next)}</span>
                </>
              ) : null}
            </div>
            <dl>
              <Prop label="负责人">
                <Person name={item.owner} />
              </Prop>
              <Prop label="目标日期">{displayDate(item.dueDate)}</Prop>
            </dl>
          </Section>
          <Section title="规划">
            <p>{item.plan || "尚未填写规划。"}</p>
          </Section>
          {item.blocker ? (
            <Section title="受阻原因">
              <p className="danger-text">{item.blocker}</p>
            </Section>
          ) : null}
          <Section title="IoT 范围">
            <div className="actions">
              {[...new Set(item.iotBindings?.map((b) => b.kind) ?? [])].map(
                (k) => (
                  <Chip key={k}>{k}</Chip>
                ),
              )}
              {!item.iotBindings?.length ? (
                <p className="caption">尚未绑定范围。</p>
              ) : null}
            </div>
          </Section>
          <Section title="下一步">{action}</Section>
        </div>
      </aside>
    );
  return (
    <div className="detail-page">
      <nav className="tabs" aria-label="事项详情页签">
        <button
          className="back-tab"
          type="button"
          aria-label="返回事项列表"
          onClick={onBack}
        >
          <ChevronLeft className="icon" />
        </button>
        {tabs.map(([key, label]) => (
          <button
            key={key}
            className={tab === key ? "active" : undefined}
            type="button"
            aria-current={tab === key ? "page" : undefined}
            onClick={() => setTab(key)}
          >
            {label}
          </button>
        ))}
      </nav>
      <div className="detail-split">
        <article className="document">
          {header}
          <div hidden={tab !== "overview"}>
            <GateTrack current={item.gate} />
            {item.blocker ? (
              <Notice title="当前事项受阻" tone="warning" icon={TriangleAlert}>
                {item.blocker}
              </Notice>
            ) : null}
            <Section title="交付概况">
              <dl className="receipt-grid">
                <Prop label="当前状态">
                  <Status value={item.status} />
                </Prop>
                <Prop label="完成进度">{item.progressPercent ?? 0}%</Prop>
                <Prop label="开始日期">{displayDate(item.startDate)}</Prop>
                <Prop label="目标日期">{displayDate(item.dueDate)}</Prop>
              </dl>
            </Section>
            <Section title="规划与方案">
              <p className="prose-copy">{item.plan || "尚未填写规划。"}</p>
              <p className="prose-copy muted">
                {item.solution || "尚未填写方案。"}
              </p>
              <Action onClick={() => setTab("plan")} icon={FileText}>
                查看规划与方案
              </Action>
            </Section>
            <Section title="关卡证据" description="证据来自已提交的交付记录。">
              {item.evidence?.length ? (
                item.evidence.map((e, i) => (
                  <div className="evidence-row" key={`${e.recordedAt}-${i}`}>
                    <FileText className="icon" />
                    <div>
                      <h3>{e.title}</h3>
                      <p className="caption">
                        {e.kind} · {e.recordedAt || "未返回时间"}
                      </p>
                      {e.reference ? <Reference value={e.reference} /> : null}
                    </div>
                  </div>
                ))
              ) : (
                <p className="caption">尚未记录关卡证据。</p>
              )}
            </Section>
          </div>
          <div hidden={tab !== "plan"}>
            <ContextEditor
              item={item}
              locked={locked}
              onSave={commands.onUpdateContext}
              onDirty={(v) => setSectionDirty("plan", v)}
            />
          </div>
          <div hidden={tab !== "decisions"}>
            <DecisionEditor
              item={item}
              locked={locked}
              onSave={commands.onUpdateContext}
              onDirty={(v) => setSectionDirty("decisions", v)}
            />
          </div>
          <div hidden={tab !== "schedule"}>
            <ScheduleEditor
              item={item}
              planning={planning}
              locked={locked}
              onSave={commands.onUpdateItem}
              onDirty={(v) => setSectionDirty("schedule", v)}
            />
          </div>
          <div hidden={tab !== "iot"}>
            <AssociationEditor
              item={item}
              mode="iot"
              locked={locked}
              onSave={commands.onUpdateItem}
              onDirty={(v) => setSectionDirty("iot", v)}
            />
          </div>
          <div hidden={tab !== "trace"}>
            <AssociationEditor
              item={item}
              mode="trace"
              locked={locked}
              onSave={commands.onUpdateItem}
              onDirty={(v) => setSectionDirty("trace", v)}
            />
          </div>
          <div hidden={tab !== "activity"}>
            <ActivityEditor
              item={item}
              locked={locked}
              onSave={commands.onAddComment}
              onDirty={(v) => setSectionDirty("activity", v)}
            />
          </div>
          <div hidden={tab !== "gate"}>
            <GateEditor
              item={item}
              locked={locked}
              onSave={commands.onAdvance}
              onDone={() => setTab("overview")}
              onDirty={(v) => setSectionDirty("gate", v)}
            />
          </div>
          <div hidden={tab !== "close"}>
            <CloseEditor
              item={item}
              locked={locked}
              onSave={commands.onClose}
              onDone={() => setTab("archive")}
              onDirty={(v) => setSectionDirty("close", v)}
            />
          </div>
          <div hidden={tab !== "archive"}>
            <ArchiveView item={item} />
          </div>
        </article>
        <aside className="inspector">
          <h3>事项属性</h3>
          <dl>
            <Prop label="状态">
              <Status value={item.status} />
            </Prop>
            <Prop label="负责人">
              <Person name={item.owner} />
            </Prop>
            <Prop label="优先级">
              <Chip>{item.priority || "未指定"}</Chip>
            </Prop>
            <Prop label="所属项目">
              {projectName || item.projectId || "未归属"}
            </Prop>
            <Prop label="所属板块">{item.board}</Prop>
            <Prop label="当前关卡">{gateText(item.gate)}</Prop>
            <Prop label="目标日期">{displayDate(item.dueDate)}</Prop>
            <Prop label="估算">{item.estimatePoints ?? 0} pts</Prop>
            <Prop label="事项版本">
              <code>revision {item.revision ?? "未知"}</code>
            </Prop>
          </dl>
          <Section title="关联排期">
            <dl>
              <Prop label="发布版本">
                {planning.releases.find((p) => p.id === item.releaseId)
                  ?.version ||
                  item.releaseId ||
                  "未关联"}
              </Prop>
              <Prop label="Sprint">
                {planning.sprints.find((p) => p.id === item.sprintId)?.name ||
                  item.sprintId ||
                  "未关联"}
              </Prop>
              <Prop label="里程碑">
                {planning.milestones.find((p) => p.id === item.milestoneId)
                  ?.name ||
                  item.milestoneId ||
                  "未关联"}
              </Prop>
            </dl>
          </Section>
          <Section title="交付动作">
            <div className="stack-actions">
              {action}
              <Action icon={Archive} onClick={() => setTab("archive")}>
                查看 Obsidian 档案
              </Action>
            </div>
          </Section>
          <p className="caption">
            所有写操作携带编辑时的
            expectedRevision；冲突后保留草稿，不自动覆盖新版本。
          </p>
        </aside>
      </div>
    </div>
  );
}
function Reference({ value }: { value: string }) {
  const url = safeReference(value);
  return url ? (
    <a
      className="reference-link"
      href={url}
      target="_blank"
      rel="noopener noreferrer"
    >
      {value}
      <ExternalLink className="icon" />
    </a>
  ) : (
    <code className="break-anywhere">{value}</code>
  );
}
function useDraft(item: WorkItem, onDirty: (v: boolean) => void) {
  const [draft, setDraft] = useState(item);
  const [dirty, setDirty] = useState(false);
  const [revision, setRevision] = useState(item.revision);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>("");
  useEffect(() => {
    if (!dirty) {
      setDraft(item);
      setRevision(item.revision);
    }
  }, [item, dirty]);
  useEffect(() => {
    onDirty(dirty);
  }, [dirty, onDirty]);
  const update = (patch: Partial<WorkItem>) => {
    setDraft((v) => ({ ...v, ...patch }));
    setDirty(true);
  };
  async function submit(event: FormEvent, action: () => Promise<unknown>) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const result = await action();
      if (result) {
        setDirty(false);
        return result;
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "提交未完成。");
    } finally {
      setBusy(false);
    }
    return null;
  }
  return {
    draft,
    dirty,
    revision,
    busy,
    error,
    update,
    submit,
    reset: () => {
      setDraft(item);
      setRevision(item.revision);
      setDirty(false);
      setError("");
    },
    conflict: dirty && revision !== item.revision,
  };
}
function DraftNotice({ state }: { state: ReturnType<typeof useDraft> }) {
  return (
    <>
      {state.conflict ? (
        <Notice title="来源版本已更新" tone="warning">
          当前草稿仍基于 revision {state.revision}
          ；请先核对差异，再决定是否丢弃草稿。
          <Action onClick={state.reset}>使用最新版本重新编辑</Action>
        </Notice>
      ) : null}
      <Failure error={state.error} />
    </>
  );
}
function ContextEditor({
  item,
  locked,
  onSave,
  onDirty,
}: {
  item: WorkItem;
  locked: boolean;
  onSave: Mutation;
  onDirty: (v: boolean) => void;
}) {
  const s = useDraft(item, onDirty);
  return (
    <form
      onSubmit={(e) =>
        void s.submit(e, () =>
          onSave(
            item.id,
            {
              plan: s.draft.plan || "",
              solution: s.draft.solution || "",
              blocker: s.draft.blocker || "",
            },
            s.revision,
          ),
        )
      }
      aria-label="规划与方案"
    >
      <DraftNotice state={s} />
      <FieldGroup>
        <FormField
          label="规划"
          multiline
          value={s.draft.plan || ""}
          disabled={locked}
          onChange={(e) => s.update({ plan: e.target.value })}
          hint="目标、成功标准与不做的范围。"
        />
        <FormField
          label="方案"
          multiline
          value={s.draft.solution || ""}
          disabled={locked}
          onChange={(e) => s.update({ solution: e.target.value })}
          hint="技术方案、接口与验证策略。"
        />
        <FormField
          label="当前阻塞项"
          value={s.draft.blocker || ""}
          disabled={locked}
          onChange={(e) => s.update({ blocker: e.target.value })}
          hint="清空阻塞原因后，服务端重新判定可推进状态。"
        />
      </FieldGroup>
      <div className="form-actions">
        <Action
          type="submit"
          primary
          busy={s.busy}
          disabled={locked || !s.dirty}
          icon={Save}
        >
          保存交付上下文
        </Action>
      </div>
    </form>
  );
}
function DecisionEditor({
  item,
  locked,
  onSave,
  onDirty,
}: {
  item: WorkItem;
  locked: boolean;
  onSave: Mutation;
  onDirty: (v: boolean) => void;
}) {
  const s = useDraft(item, onDirty);
  const [title, setTitle] = useState("");
  const [outcome, setOutcome] = useState("");
  return (
    <>
      <Heading
        title="交付决策"
        description="保留决策结论与影响，通过事项上下文命令追加。"
      />
      <form
        aria-label="新增决策"
        onSubmit={async (e) => {
          const r = await s.submit(e, () =>
            onSave(
              item.id,
              { decision: { title: title.trim(), outcome: outcome.trim() } },
              s.revision,
            ),
          );
          if (r) {
            setTitle("");
            setOutcome("");
          }
        }}
      >
        <DraftNotice state={s} />
        <FieldGroup>
          <FormField
            label="决策标题"
            value={title}
            required
            disabled={locked}
            onChange={(e) => {
              setTitle(e.target.value);
              s.update({});
            }}
          />
          <FormField
            label="决策结论"
            value={outcome}
            multiline
            required
            disabled={locked}
            onChange={(e) => {
              setOutcome(e.target.value);
              s.update({});
            }}
          />
        </FieldGroup>
        <div className="form-actions">
          <Action type="submit" primary busy={s.busy} disabled={locked}>
            记录决策
          </Action>
        </div>
      </form>
      <Section title="已记录决策">
        {item.decisions?.length ? (
          item.decisions.map((d) => (
            <article className="decision-entry" key={d.id}>
              <h3>{d.title}</h3>
              <p className="prose-copy">{d.outcome}</p>
              <span className="caption">{d.createdAt || d.id}</span>
            </article>
          ))
        ) : (
          <p className="caption">尚未记录决策。</p>
        )}
      </Section>
    </>
  );
}
function ScheduleEditor({
  item,
  planning,
  locked,
  onSave,
  onDirty,
}: {
  item: WorkItem;
  planning: Planning;
  locked: boolean;
  onSave: Mutation;
  onDirty: (v: boolean) => void;
}) {
  const s = useDraft(item, onDirty);
  const [deps, setDeps] = useState(
    (item.dependencies ?? [])
      .filter((d) => d.relation === "depends_on")
      .map((d) => d.itemId)
      .join(", "),
  );
  useEffect(() => {
    if (!s.dirty)
      setDeps(
        (item.dependencies ?? [])
          .filter((d) => d.relation === "depends_on")
          .map((d) => d.itemId)
          .join(", "),
      );
  }, [item, s.dirty]);
  const opts = (list: Planning["releases"]) => [
    { value: "", label: "未关联" },
    ...list
      .filter((p) => p.projectId === item.projectId)
      .map((p) => ({
        value: p.id,
        label: p.version ? `${p.version} · ${p.name}` : p.name,
      })),
  ];
  return (
    <form
      aria-label="排期与依赖"
      onSubmit={(e) =>
        void s.submit(e, () => {
          const dependencies = deps
            .split(/[,，\n]/)
            .map((v) => v.trim())
            .filter(Boolean);
          if (dependencies.includes(item.id))
            throw new Error(
              "依赖不能包含当前事项自身；跨事项环路由服务端再次检查。",
            );
          if (
            s.draft.startDate &&
            s.draft.dueDate &&
            s.draft.dueDate < s.draft.startDate
          )
            throw new Error("目标日期不能早于开始日期。");
          return onSave(
            item.id,
            {
              owner: s.draft.owner || "",
              startDate: s.draft.startDate || "",
              dueDate: s.draft.dueDate || "",
              estimatePoints: Number(s.draft.estimatePoints || 0),
              progressPercent: Number(s.draft.progressPercent || 0),
              releaseId: s.draft.releaseId || "",
              sprintId: s.draft.sprintId || "",
              milestoneId: s.draft.milestoneId || "",
              dependencies: [
                ...(item.dependencies ?? []).filter(
                  (d) => d.relation !== "depends_on",
                ),
                ...[...new Set(dependencies)].map((itemId) => ({
                  itemId,
                  relation: "depends_on",
                })),
              ],
            },
            s.revision,
          );
        })
      }
    >
      <DraftNotice state={s} />
      <FieldGroup className="form-grid">
        <FormField
          label="负责人"
          required
          disabled={locked}
          value={s.draft.owner || ""}
          onChange={(e) => s.update({ owner: e.target.value })}
        />
        <FormField
          label="进度 %"
          type="number"
          min={0}
          max={100}
          step={1}
          disabled={locked}
          value={s.draft.progressPercent ?? 0}
          onChange={(e) =>
            s.update({ progressPercent: Number(e.target.value) })
          }
        />
        <FormField
          label="开始日期"
          type="date"
          disabled={locked}
          value={s.draft.startDate || ""}
          onChange={(e) => s.update({ startDate: e.target.value })}
        />
        <FormField
          label="目标日期"
          type="date"
          disabled={locked}
          value={s.draft.dueDate || ""}
          onChange={(e) => s.update({ dueDate: e.target.value })}
        />
        <FormField
          label="估算点"
          type="number"
          min={0}
          step={0.5}
          disabled={locked}
          value={s.draft.estimatePoints ?? 0}
          onChange={(e) => s.update({ estimatePoints: Number(e.target.value) })}
        />
        <FormField
          label="发布版本"
          options={opts(planning.releases)}
          disabled={locked}
          value={s.draft.releaseId || ""}
          onChange={(e) => s.update({ releaseId: e.target.value })}
        />
        <FormField
          label="Sprint"
          options={opts(planning.sprints)}
          disabled={locked}
          value={s.draft.sprintId || ""}
          onChange={(e) => s.update({ sprintId: e.target.value })}
        />
        <FormField
          label="里程碑"
          options={opts(planning.milestones)}
          disabled={locked}
          value={s.draft.milestoneId || ""}
          onChange={(e) => s.update({ milestoneId: e.target.value })}
        />
      </FieldGroup>
      <Section
        title="前置依赖"
        description="仅编辑 depends_on；保留已有 blocks / related 关系，服务端拒绝循环依赖。"
      >
        <FormField
          label="依赖事项 ID（逗号分隔）"
          disabled={locked}
          value={deps}
          onChange={(e) => {
            setDeps(e.target.value);
            s.update({});
          }}
        />
      </Section>
      <div className="form-actions">
        <Action
          type="submit"
          primary
          busy={s.busy}
          disabled={locked || !s.dirty}
          icon={Save}
        >
          保存排期与关联
        </Action>
      </div>
    </form>
  );
}
function AssociationEditor({
  item,
  mode,
  locked,
  onSave,
  onDirty,
}: {
  item: WorkItem;
  mode: "iot" | "trace";
  locked: boolean;
  onSave: Mutation;
  onDirty: (v: boolean) => void;
}) {
  const s = useDraft(item, onDirty);
  const encode = () =>
    mode === "iot"
      ? stringifyIoTBindings(item.iotBindings)
      : stringifyTraceLinks(item.traceLinks);
  const [text, setText] = useState(encode);
  useEffect(() => {
    if (!s.dirty) setText(encode());
  }, [item, mode, s.dirty]);
  const rows =
    mode === "iot" ? (item.iotBindings ?? []) : (item.traceLinks ?? []);
  return (
    <>
      <Notice
        title={
          mode === "iot"
            ? "交付范围，不是设备主数据"
            : "关联外部证据，不接管原系统"
        }
      >
        {mode === "iot"
          ? "设备、固件、客户、环境和灰度批次仅保存引用。"
          : "PR、构建、测试、缺陷、发布保留引用、状态与记录时间。"}
      </Notice>
      <table className="data-table bindings">
        <thead>
          <tr>
            <th>类型</th>
            <th>引用 / 名称</th>
            <th>{mode === "iot" ? "属性" : "状态"}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row, i) => (
            <tr key={i}>
              <td>
                <Chip>{row.kind}</Chip>
              </td>
              <td>
                <code>{row.reference}</code>
                <small>
                  {"label" in row ? row.label : "title" in row ? row.title : ""}
                </small>
              </td>
              <td>
                {"attributes" in row ? (
                  <code>{JSON.stringify(row.attributes ?? {})}</code>
                ) : "status" in row ? (
                  row.status
                ) : (
                  "—"
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {!rows.length ? <p className="caption">尚未添加关联。</p> : null}
      <Section title={mode === "iot" ? "编辑 IoT 范围" : "编辑研发证据关联"}>
        <form
          onSubmit={(e) =>
            void s.submit(e, () => {
              for (const [index, line] of text.split("\n").entries()) {
                if (!line.trim()) continue;
                const parts = line.split("|").map((p) => p.trim());
                if (!parts[0] || !parts[1])
                  throw new Error(`第 ${index + 1} 行缺少类型或引用。`);
                const allowed =
                  mode === "iot"
                    ? [
                        "device",
                        "firmware",
                        "customer",
                        "environment",
                        "rollout_batch",
                      ]
                    : ["pull_request", "build", "test", "defect", "release"];
                if (!allowed.includes(parts[0]))
                  throw new Error(`第 ${index + 1} 行类型不在合同允许范围。`);
                if (parts.length > (mode === "iot" ? 4 : 6))
                  throw new Error(
                    `第 ${index + 1} 行列数过多；属性内的 | 请转义为 \\u007c。`,
                  );
                if (mode === "trace" && parts[3] && !safeReference(parts[3]))
                  throw new Error(
                    `第 ${index + 1} 行 URL 必须为完整 http / https 地址。`,
                  );
                if (
                  mode === "trace" &&
                  parts[5] &&
                  !/^\d{4}-\d{2}-\d{2}T.*(?:Z|[+-]\d{2}:\d{2})$/.test(parts[5])
                )
                  throw new Error(`第 ${index + 1} 行时间必须包含时区。`);
              }
              return onSave(
                item.id,
                mode === "iot"
                  ? { iotBindings: parseIoTBindings(text) }
                  : { traceLinks: parseTraceLinks(text) },
                s.revision,
              );
            })
          }
        >
          <DraftNotice state={s} />
          <FormField
            className="mono-editor"
            label={
              mode === "iot"
                ? "每行：类型 | 引用 | 标签 | JSON 属性"
                : "每行：类型 | 引用 | 标题 | URL | 状态 | 记录时间"
            }
            multiline
            disabled={locked}
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              s.update({});
            }}
            hint={
              mode === "iot"
                ? "JSON 属性仅接受字符串键与字符串值。"
                : "引用不会触发外部执行；服务端再次校验合同。"
            }
          />
          <div className="form-actions">
            <Action
              type="submit"
              primary
              busy={s.busy}
              disabled={locked || !s.dirty}
              icon={Save}
            >
              {mode === "iot" ? "保存 IoT 范围" : "保存研发关联"}
            </Action>
          </div>
        </form>
      </Section>
    </>
  );
}
function ActivityEditor({
  item,
  locked,
  onSave,
  onDirty,
}: {
  item: WorkItem;
  locked: boolean;
  onSave: Props["onAddComment"];
  onDirty: (v: boolean) => void;
}) {
  const s = useDraft(item, onDirty);
  const [comment, setComment] = useState("");
  return (
    <>
      <form
        aria-label="新增评论"
        onSubmit={async (e) => {
          const result = await s.submit(e, () =>
            onSave(item.id, comment.trim(), s.revision),
          );
          if (result) setComment("");
        }}
      >
        <DraftNotice state={s} />
        <FormField
          label="补充评论"
          multiline
          required
          disabled={locked}
          value={comment}
          onChange={(e) => {
            setComment(e.target.value);
            s.update({});
          }}
          hint="补充协作结论、风险或下一步。"
        />
        <div className="form-actions">
          <Action
            type="submit"
            primary
            busy={s.busy}
            disabled={locked || !comment.trim()}
            icon={MessageSquare}
          >
            新增评论
          </Action>
        </div>
      </form>
      <Section title="评论">
        {item.comments?.length ? (
          item.comments.map((c) => (
            <article className="comment-entry" key={c.id}>
              <div className="actions">
                <Person name={c.author} />
                <span className="caption">{c.createdAt}</span>
              </div>
              <p className="prose-copy">{c.body}</p>
            </article>
          ))
        ) : (
          <p className="caption">尚无评论。</p>
        )}
      </Section>
      <Section title="活动审计">
        {item.activities?.length ? (
          <ol className="activity-list">
            {item.activities
              .slice()
              .reverse()
              .map((a) => (
                <li key={a.id}>
                  <span className="caption">{a.occurredAt}</span>
                  <p>
                    {a.actor} · {a.summary}
                  </p>
                </li>
              ))}
          </ol>
        ) : (
          <p className="caption">尚无活动记录。</p>
        )}
      </Section>
    </>
  );
}
function GateEditor({
  item,
  locked,
  onSave,
  onDone,
  onDirty,
}: {
  item: WorkItem;
  locked: boolean;
  onSave: Props["onAdvance"];
  onDone: () => void;
  onDirty: (v: boolean) => void;
}) {
  const s = useDraft(item, onDirty);
  const [title, setTitle] = useState("");
  const [reference, setReference] = useState("");
  const next = upcomingGate(item.gate);
  const allowed = !!next && !locked && item.status !== "blocked";
  return (
    <>
      <Heading
        title={`提交${next ? gateText(next) : "下一关"}证据`}
        description="只推进相邻关卡；评审结果以服务端确认后的状态为准。"
      />
      <GateTrack current={item.gate} />
      {item.status === "blocked" ? (
        <Notice title="事项受阻，不能推进" tone="warning">
          {item.blocker || "请先解决阻塞，并保存事项上下文。"}
        </Notice>
      ) : null}
      {next === "production_validated" ? (
        <Notice title="需要独立生产验证" icon={ShieldCheck}>
          实现者不能验证自己的变更；独立验证者与项目权限由服务端检查，前端不会自动放行。
        </Notice>
      ) : null}
      <form
        aria-label="提交关卡证据"
        onSubmit={async (e) => {
          if (!allowed) {
            e.preventDefault();
            return;
          }
          const r = await s.submit(e, () =>
            onSave(
              item.id,
              next!,
              { title: title.trim(), reference: reference.trim() },
              s.revision,
            ),
          );
          if (r) {
            setTitle("");
            setReference("");
            onDone();
          }
        }}
      >
        <DraftNotice state={s} />
        <FieldGroup>
          <FormField
            label="证据标题"
            required
            value={title}
            disabled={!allowed}
            onChange={(e) => {
              setTitle(e.target.value);
              s.update({});
            }}
          />
          <FormField
            label="证据引用"
            value={reference}
            disabled={!allowed}
            onChange={(e) => {
              setReference(e.target.value);
              s.update({});
            }}
            hint="可填写评审记录、测试报告、PR 或工单链接。"
          />
        </FieldGroup>
        <div className="form-actions">
          <Action
            type="submit"
            primary
            busy={s.busy}
            disabled={!allowed || !title.trim()}
            icon={ArrowRight}
          >
            提交{next ? gateText(next) : "下一关"}证据
          </Action>
        </div>
      </form>
    </>
  );
}
function CloseEditor({
  item,
  locked,
  onSave,
  onDone,
  onDirty,
}: {
  item: WorkItem;
  locked: boolean;
  onSave: Props["onClose"];
  onDone: () => void;
  onDirty: (v: boolean) => void;
}) {
  const s = useDraft(item, onDirty);
  const allowed =
    item.gate === "production_validated" &&
    !locked &&
    item.status !== "blocked";
  return (
    <>
      <Heading
        title="关闭前复盘"
        description="只有生产验证完成后才可关闭；复盘内容进入单向知识沉淀。"
      />
      <Notice
        title={allowed ? "已完成生产验证" : "尚不满足关闭条件"}
        tone={allowed ? "success" : "warning"}
      >
        {allowed
          ? "填写有效做法、偏差与下一次改进动作。"
          : "请先完成生产验证并解除阻塞。"}
      </Notice>
      <form
        onSubmit={async (e) => {
          if (!allowed) {
            e.preventDefault();
            return;
          }
          const r = await s.submit(e, () =>
            onSave(item.id, s.draft.retrospective?.trim() || "", s.revision),
          );
          if (r) onDone();
        }}
      >
        <DraftNotice state={s} />
        <FormField
          label="复盘内容"
          multiline
          required
          disabled={!allowed}
          value={s.draft.retrospective || ""}
          onChange={(e) => s.update({ retrospective: e.target.value })}
        />
        <div className="form-actions">
          <Action
            type="submit"
            primary
            busy={s.busy}
            disabled={!allowed || !s.draft.retrospective?.trim()}
            icon={CheckCircle}
          >
            提交复盘并关闭
          </Action>
        </div>
      </form>
    </>
  );
}
function ArchiveView({ item }: { item: WorkItem }) {
  return (
    <>
      <Heading
        title="Obsidian 档案"
        description="系统管理交付主数据；Obsidian 是事件驱动的只读沉淀层。"
      />
      <Notice title="以下为约定的投影路径，不等同于已核验文件存在">
        当前 API 未提供投影完成回执；不根据路径推断同步成功。
      </Notice>
      <Section title="档案路径">
        <div className="file-tree">
          {archiveEntries(item.id).map((e: { path: string; label: string }) => (
            <div key={e.path}>
              <FileText className="icon" />
              <div>
                <strong>{e.label}</strong>
                <code className="break-anywhere">{e.path}</code>
              </div>
            </div>
          ))}
        </div>
      </Section>
      <Section title="复盘记录">
        <p className="prose-copy">{item.retrospective || "尚未提交复盘。"}</p>
      </Section>
    </>
  );
}
