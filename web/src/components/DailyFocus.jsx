import { todayInChina } from "../lib/presentation.mjs";

const focusOptions = [
  { id: "attention", label: "需关注", metric: (focus) => focus.blocked + focus.overdue, tone: "attention" },
  { id: "blocked", label: "受阻", metric: (focus) => focus.blocked, tone: "blocked" },
  { id: "overdue", label: "已逾期", metric: (focus) => focus.overdue, tone: "overdue" },
  { id: "verification", label: "待验证", metric: (focus) => focus.verifying, tone: "verification" },
];

export function DailyFocus({ activeFilter, focus, onSelect }) {
  return (
    <section className="daily-focus" aria-label="每日交付关注项">
      <div className="daily-focus-copy">
        <span className="eyebrow">每日状态</span>
        <h2>{todayInChina()} 今日需关注</h2>
        <p>先处理受阻与逾期事项，再进入验证和发布节奏。</p>
      </div>
      <div className="daily-focus-actions" role="group" aria-label="按关注项筛选交付事项">
        {focusOptions.map((option) => (
          <button
            aria-pressed={activeFilter === option.id}
            className={`focus-filter ${option.tone} ${activeFilter === option.id ? "selected" : ""}`}
            key={option.id}
            onClick={() => onSelect(option.id)}
            type="button"
          >
            <strong>{option.metric(focus)}</strong>
            <span>{option.label}</span>
          </button>
        ))}
        {activeFilter !== "all" ? <button className="clear-filter" onClick={() => onSelect("all")} type="button">清除筛选</button> : null}
      </div>
    </section>
  );
}
