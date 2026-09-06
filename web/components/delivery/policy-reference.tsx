"use client";
import { LockKeyhole } from "lucide-react";
import { Chip, Heading, Notice, Section } from "./ui";
const policies = {
  rules: {
    title: "交付规则",
    description: "由服务端强制执行的交付边界，不提供开关。",
    groups: [
      [
        "关卡治理",
        [
          [
            "相邻关卡推进",
            "规划确认 → 方案评审 → 研发完成 → 测试通过 → 生产验证。",
          ],
          ["每次推进附证据", "提交证据标题及可选引用，服务端验证后更新关卡。"],
          ["受阻事项禁止推进", "必须先解除阻塞，再重新提交。"],
        ],
      ],
      [
        "关闭与并发保护",
        [
          [
            "生产验证与职责分离",
            "实现者不能自行完成生产验证，服务端核验独立验证者。",
          ],
          ["关闭前复盘", "生产验证完成且填写复盘后才可关闭。"],
          [
            "版本校验",
            "所有更新提交编辑时的 expectedRevision；冲突后不自动覆盖。",
          ],
        ],
      ],
    ],
  },
  obsidian: {
    title: "Obsidian 投影",
    description: "交付系统持有主数据，笔记为单向生成的知识沉淀。",
    groups: [
      [
        "事实来源",
        [
          ["SQLite", "交付事项、证据、活动与排期在系统中修改。"],
          [
            "单向投影",
            "Outbox 事件触发投影；不要在 Obsidian 生成页反向编辑业务状态。",
          ],
          [
            "投影回执",
            "当前业务 API 不返回文件同步完成回执，不把路径存在视为同步成功。",
          ],
        ],
      ],
      [
        "沉淀内容",
        [
          ["规划、方案、决策", "保留目标、实现方案和 ADR 结论。"],
          ["发布与验证、复盘", "保留关卡证据和关闭前复盘。"],
          [
            "Vault 路径",
            "实际路径由运行环境决定，本页面不读取或修改密钥与环境变量。",
          ],
        ],
      ],
    ],
  },
  notifications: {
    title: "通知与提醒",
    description: "通道配置由运行环境提供，此页面只说明能力边界。",
    groups: [
      [
        "通道策略",
        [
          ["本地收件箱", "事项、项目及截止日事件的本地耐久投递记录。"],
          [
            "外部通道",
            "Webhook、企业微信、SMTP 只有完整配置且获授权后才启用。",
          ],
          [
            "凭据与地址",
            "不在浏览器展示或保存通道密钥、SMTP 密码和实际接收目标。",
          ],
        ],
      ],
      [
        "截止提醒",
        [
          ["扫描与提前量", "具体值由部署配置决定；此处不推断正在使用的值。"],
          ["去重", "同一开放事项每天使用稳定提醒事件 ID。"],
          ["投递操作", "现有 UI 只读取通知，不提供已读、删除或重发接口。"],
        ],
      ],
    ],
  },
  runtime: {
    title: "运行时与 MCP",
    description: "运行架构说明；不是实时健康检查或配置管理界面。",
    groups: [
      [
        "Yunka 运行时",
        [
          ["生命周期", "Yunka 管理应用启动、健康检查和有序关闭。"],
          [
            "事务 Outbox",
            "业务更新与事件在同一事务中写入，再由 dispatcher 投递。",
          ],
          [
            "生产身份",
            "浏览器通过 BFF 会话访问，权限由服务端 Guard 最终判定。",
          ],
        ],
      ],
      [
        "MCP 边界",
        [
          ["开发环境入口", "当前 stdio MCP 仅用于 development。"],
          ["数据库隔离", "MCP 与 HTTP 服务不应同时打开同一个 SQLite 文件。"],
          ["非本轮交付", "不添加 Agent 调度、探针开关或生产 MCP 启用功能。"],
        ],
      ],
    ],
  },
} as const;
export type PolicyKey = keyof typeof policies;
export function PolicyReference({ view }: { view: PolicyKey }) {
  const data = policies[view];
  return (
    <>
      <Heading
        title={data.title}
        description={data.description}
        actions={<Chip>只读说明</Chip>}
      />
      <Notice title="能力说明，不是实时配置" role="note">
        现有前端合同不提供对应的配置读取和写入；此页不会推断启用状态，也不会发起配置变更。
      </Notice>
      {data.groups.map(([title, rows]) => (
        <Section key={title} title={title}>
          {rows.map(([label, help]) => (
            <div className="setting-row" key={label}>
              <div>
                <div className="setting-label">{label}</div>
                <p>{help}</p>
              </div>
              <span className="readonly-state">
                <LockKeyhole className="icon" />
                服务端约束
              </span>
            </div>
          ))}
        </Section>
      ))}
    </>
  );
}
