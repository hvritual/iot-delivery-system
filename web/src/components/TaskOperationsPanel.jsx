import { useState } from "react";

import { workItemKinds } from "../lib/r2-presentation.mjs";

export function TaskOperationsPanel({ filter, notifications = [], onApplyView, onFilterChange, onLoadMemberWeek, onSaveView, savedViews = [], week }) {
  const [viewName, setViewName] = useState("");
  const [member, setMember] = useState("");
  const [weekStart, setWeekStart] = useState("");

  async function saveCurrentView(event) {
    event.preventDefault();
    if (!viewName.trim()) return;
    const saved = await onSaveView({ name: viewName.trim(), filter });
    if (saved) setViewName("");
  }

  async function loadWeek(event) {
    event.preventDefault();
    if (!member.trim()) return;
    await onLoadMemberWeek(member.trim(), weekStart);
  }

  return (
    <section className="task-operations" aria-label="任务筛选、成员周视图与通知">
      <div className="task-filter-row">
        <label>
          搜索事项
          <input onChange={(event) => onFilterChange({ ...filter, query: event.target.value })} placeholder="标题、设备、PR、构建或测试证据" value={filter.query ?? ""} />
        </label>
        <label>
          负责人
          <input onChange={(event) => onFilterChange({ ...filter, owner: event.target.value })} placeholder="成员名称" value={filter.owner ?? ""} />
        </label>
        <label>
          状态
          <select onChange={(event) => onFilterChange({ ...filter, status: event.target.value })} value={filter.status ?? ""}>
            <option value="">全部状态</option>
            <option value="planned">待规划</option>
            <option value="in_progress">推进中</option>
            <option value="blocked">受阻</option>
            <option value="verifying">验证中</option>
            <option value="released">已发布</option>
            <option value="closed">已关闭</option>
          </select>
        </label>
        <label>
          类型
          <select onChange={(event) => onFilterChange({ ...filter, kind: event.target.value })} value={filter.kind ?? ""}>
            <option value="">全部类型</option>
            {workItemKinds.map((kind) => <option key={kind.value} value={kind.value}>{kind.label}</option>)}
          </select>
        </label>
        <button className="quiet-button" onClick={() => onFilterChange({ projectId: filter.projectId ?? "", owner: "", status: "", kind: "", query: "" })} type="button">清空筛选</button>
      </div>
      <div className="task-utility-grid">
        <form className="utility-card" onSubmit={saveCurrentView}>
          <span className="eyebrow">保存视图</span>
          <h3>复用这组筛选</h3>
          <div className="inline-action"><input onChange={(event) => setViewName(event.target.value)} placeholder="例如：本周 OTA 冲刺" value={viewName} /><button className="secondary-button" type="submit">保存</button></div>
          {savedViews.length ? <div className="saved-view-list">{savedViews.map((view) => <button key={view.id} onClick={() => onApplyView(view)} type="button">{view.name}</button>)}</div> : <p>尚未保存视图。</p>}
        </form>
        <form className="utility-card" onSubmit={loadWeek}>
          <span className="eyebrow">成员周视图</span>
          <h3>查看本周任务</h3>
          <div className="inline-action"><input onChange={(event) => setMember(event.target.value)} placeholder="成员名称" required value={member} /><input onChange={(event) => setWeekStart(event.target.value)} type="date" value={weekStart} /><button className="secondary-button" type="submit">查看</button></div>
          {week ? <p>{week.member} · {week.weekStart} 至 {week.weekEnd} · {week.items?.length ?? 0} 项</p> : <p>输入成员后查看排期内及未排期的开放事项。</p>}
        </form>
        <section className="utility-card notification-card">
          <span className="eyebrow">本地通知收件箱</span>
          <h3>{notifications.length} 条最近投递</h3>
          {notifications.length ? <ul>{notifications.slice(0, 3).map((notification) => <li key={`${notification.deliveryId}-${notification.channel}`}><strong>{notification.title || notification.eventType}</strong><span>{notification.subject || "未关联事项"}</span></li>)}</ul> : <p>任务事件会先可靠投递到这里；可再接入企业微信、邮件或 Webhook 通道。</p>}
        </section>
      </div>
      {week?.items?.length ? <div className="week-items"><strong>{week.member} 的周任务</strong>{week.items.map((item) => <span key={item.id}>{item.id} · {item.title} · {item.status}</span>)}</div> : null}
    </section>
  );
}
