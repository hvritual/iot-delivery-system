"use client";
import { useId, type ReactNode, type ComponentProps } from "react";
import {
  Circle,
  CircleCheck,
  Clock,
  TriangleAlert,
  ShieldCheck,
  Check,
  LoaderCircle,
  type LucideIcon,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import {
  Empty,
  EmptyHeader,
  EmptyTitle,
  EmptyDescription,
  EmptyMedia,
  EmptyContent,
} from "@/components/ui/empty";
import {
  Field as BaseField,
  FieldLabel,
  FieldDescription,
  FieldGroup,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { GATES, STATUS_LABELS, gateText, type RequestFailure } from "./model";
export { FieldGroup };
export function Action({
  children,
  primary,
  icon: Icon,
  busy,
  className,
  ...props
}: ComponentProps<typeof Button> & {
  primary?: boolean;
  icon?: LucideIcon;
  busy?: boolean;
}) {
  return (
    <Button
      type="button"
      variant={primary ? "default" : "outline"}
      {...props}
      disabled={busy || props.disabled}
      className={cn("btn", primary && "primary", className)}
      aria-busy={busy || undefined}
    >
      {busy ? (
        <LoaderCircle data-icon="inline-start" className="spin" />
      ) : Icon ? (
        <Icon data-icon="inline-start" />
      ) : null}
      {children}
    </Button>
  );
}
export function Chip({
  children,
  tone = "neutral",
}: {
  children: ReactNode;
  tone?: string;
}) {
  return (
    <Badge variant="secondary" className={cn("chip", tone)}>
      {children}
    </Badge>
  );
}
export function Status({ value }: { value?: string }) {
  const label = STATUS_LABELS[value ?? ""] ?? "未知状态";
  const tone =
    value === "blocked"
      ? "danger"
      : value === "verifying"
        ? "warning"
        : ["released", "closed"].includes(value ?? "")
          ? "success"
          : value === "in_progress"
            ? "info"
            : "";
  const Icon =
    value === "blocked"
      ? TriangleAlert
      : value === "verifying"
        ? Clock
        : ["released", "closed"].includes(value ?? "")
          ? CircleCheck
          : Circle;
  return (
    <span className={cn("status", tone)}>
      <Icon className="icon" aria-hidden="true" />
      {label}
    </span>
  );
}
export function Person({ name }: { name?: string }) {
  return (
    <span className="person" title={name || "未指定"}>
      <span className="avatar" aria-hidden="true">
        {Array.from(name || "未")[0]}
      </span>
      <span className="person-name">{name || "未指定"}</span>
    </span>
  );
}
export function Heading({
  title,
  description,
  actions,
}: {
  title: string;
  description?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <div className="page-heading">
      <div>
        <h2>{title}</h2>
        {description ? <p>{description}</p> : null}
      </div>
      {actions ? <div className="actions">{actions}</div> : null}
    </div>
  );
}
export function Section({
  title,
  description,
  children,
}: {
  title: string;
  description?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="section">
      <div className="section-title">
        <h3>{title}</h3>
        {description ? <p>{description}</p> : null}
      </div>
      {children}
    </section>
  );
}
export function Notice({
  title,
  children,
  tone = "neutral",
  icon: Icon = ShieldCheck,
  ...props
}: {
  title: string;
  children?: ReactNode;
  tone?: string;
  icon?: LucideIcon;
} & ComponentProps<typeof Alert>) {
  return (
    <Alert {...props} className={cn("notice", tone)}>
      <Icon className="icon" aria-hidden="true" />
      <div>
        <AlertTitle>{title}</AlertTitle>
        {children ? <AlertDescription>{children}</AlertDescription> : null}
      </div>
    </Alert>
  );
}
export function Failure({
  error,
  onRetry,
}: {
  error: RequestFailure | string | null;
  onRetry?: () => void;
}) {
  if (!error) return null;
  const code = typeof error === "string" ? undefined : error.status;
  const title =
    code === 409
      ? "数据版本冲突"
      : code === 403
        ? "操作未获授权"
        : code === 429
          ? "操作已临时限流"
          : "操作未完成";
  return (
    <Notice title={title} tone="danger" icon={TriangleAlert}>
      <span>{code ? `HTTP ${code} · ` : ""}{typeof error === "string" ? error : error.message}</span>
      {typeof error !== "string" && error.traceId ? (
        <code>Trace ID: {error.traceId}</code>
      ) : null}
      {onRetry ? <Action onClick={onRetry}>重新读取</Action> : null}
    </Notice>
  );
}
export function NoData({
  title,
  children,
  action,
  icon: Icon = Circle,
}: {
  title: string;
  children?: ReactNode;
  action?: ReactNode;
  icon?: LucideIcon;
}) {
  return (
    <Empty className="empty-state">
      <EmptyHeader>
        <EmptyMedia>
          <Icon className="icon" />
        </EmptyMedia>
        <EmptyTitle>{title}</EmptyTitle>
        {children ? <EmptyDescription>{children}</EmptyDescription> : null}
      </EmptyHeader>
      {action ? <EmptyContent>{action}</EmptyContent> : null}
    </Empty>
  );
}
export function Prop({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  return (
    <div className="prop">
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}
export function FormField({
  label,
  hint,
  options,
  multiline,
  className,
  ...props
}: ComponentProps<typeof Input> & {
  label: string;
  hint?: ReactNode;
  options?: { value: string; label: string }[];
  multiline?: boolean;
}) {
  const uid = useId();
  const id = props.id || uid;
  return (
    <BaseField
      className={cn("field", className)}
      data-disabled={props.disabled || undefined}
      data-invalid={props["aria-invalid"] || undefined}
    >
      <FieldLabel htmlFor={id} data-required={props.required || undefined}>
        {label}
      </FieldLabel>
      {options ? (
        <select
          {...(props as ComponentProps<"select">)}
          id={id}
          aria-describedby={hint ? `${id}-help` : undefined}
        >
          {options.map((o) => (
            <option value={o.value} key={o.value}>
              {o.label}
            </option>
          ))}
        </select>
      ) : multiline ? (
        <Textarea
          {...(props as ComponentProps<typeof Textarea>)}
          id={id}
          aria-describedby={hint ? `${id}-help` : undefined}
        />
      ) : (
        <Input
          {...props}
          id={id}
          aria-describedby={hint ? `${id}-help` : undefined}
        />
      )}
      {hint ? (
        <FieldDescription id={`${id}-help`}>{hint}</FieldDescription>
      ) : null}
    </BaseField>
  );
}
export function GateTrack({
  current,
  vertical = false,
}: {
  current?: string;
  vertical?: boolean;
}) {
  const position = GATES.indexOf(current as (typeof GATES)[number]);
  return (
    <div
      className={cn("gate-track", vertical && "vertical")}
      aria-label="交付关卡"
    >
      {GATES.map((gate, index) => (
        <div
          key={gate}
          className={cn(
            "gate-step",
            index < position && "complete",
            index === position && "current",
          )}
        >
          <span>
            {index < position ? <Check className="icon" /> : index + 1}
          </span>
          <div>
            {gateText(gate)}
            <small>
              {index < position
                ? "已完成"
                : index === position
                  ? "当前关卡"
                  : "待提交证据"}
            </small>
          </div>
        </div>
      ))}
    </div>
  );
}
export function Metric({
  label,
  value,
  hint,
  tone,
  onClick,
}: {
  label: string;
  value: ReactNode;
  hint?: string;
  tone?: string;
  onClick?: () => void;
}) {
  const content = (
    <>
      <span>{label}</span>
      <strong className={tone}>{value}</strong>
      {hint ? <small>{hint}</small> : null}
    </>
  );
  return onClick ? (
    <button type="button" className="metric" onClick={onClick}>
      {content}
    </button>
  ) : (
    <div className="metric">{content}</div>
  );
}
