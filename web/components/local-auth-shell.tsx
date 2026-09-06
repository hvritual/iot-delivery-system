"use client";

import {
  type FormEvent,
  type ReactNode,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import {
  KeyRoundIcon,
  LogOutIcon,
  ShieldCheckIcon,
  UserRoundCogIcon,
} from "lucide-react";

import { DeliveryWorkspace } from "@/components/delivery-workspace";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button, buttonVariants } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Field as BaseField } from "@/components/ui/field";
import { Action, Chip, Heading, Notice, Section } from "./delivery/ui";
import { PolicyReference, type PolicyKey } from "./delivery/policy-reference";
import {
  assignLocalProjectRole,
  changeLocalPassword,
  createLocalMember,
  disableLocalMember,
  fetchLocalCurrent,
  localLogin,
  localLogout,
  oidcLogout,
  resetLocalMemberCredential,
  revokeLocalProjectRole,
} from "@/src/api.js";

type LocalMember = Readonly<{
  authenticated: true;
  organizationId: string;
  userId: string;
  displayName?: string;
  email?: string;
  userRevision: number;
  sessionRevision: number;
  sessionExpiresAt?: string;
  traceId?: string;
}>;

type MemberResult = Readonly<{
  OrganizationID?: string;
  UserID?: string;
  UserRevision?: number;
  CredentialRevision?: number;
  organizationId?: string;
  userId?: string;
  userRevision?: number;
  credentialRevision?: number;
}>;

type BindingResult = Readonly<{
  BindingID?: string;
  OrganizationID?: string;
  ProjectID?: string;
  UserID?: string;
  RoleID?: string;
  Status?: string;
  Revision?: number;
  bindingId?: string;
  organizationId?: string;
  projectId?: string;
  userId?: string;
  roleId?: string;
  status?: string;
  revision?: number;
}>;

type AuthMode =
  | "loading"
  | "unauthenticated"
  | "local"
  | "oidc"
  | "unavailable";
type AccountTab =
  | "profile"
  | "security"
  | "members"
  | "disable"
  | "reset"
  | "roles"
  | "revoke"
  | "receipts"
  | PolicyKey;
type RequestError = Error & { status?: number; traceId?: string };

export function LocalAuthShell() {
  const [mode, setMode] = useState<AuthMode>("loading");
  const [member, setMember] = useState<LocalMember | null>(null);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [accountOpen, setAccountOpen] = useState(false);
  const [tab, setTab] = useState<AccountTab>("profile");
  const [lastMember, setLastMember] = useState<MemberResult | null>(null);
  const [lastBinding, setLastBinding] = useState<BindingResult | null>(null);
  const accountDirty = useRef(false);
  const commandLock = useRef(false);
  const expireSession = useCallback(() => {
    setMember(null);
    setAccountOpen(false);
    setLastMember(null);
    setLastBinding(null);
    accountDirty.current = false;
    setMode("unauthenticated");
    setMessage("会话已失效，请重新登录。");
  }, []);
  function leaveAccount() {
    if (
      accountDirty.current &&
      !window.confirm("有未提交的账号表单，确定放弃吗？")
    )
      return false;
    accountDirty.current = false;
    setAccountOpen(false);
    return true;
  }
  function selectAccountTab(next: AccountTab) {
    if (busy) return;
    if (
      accountDirty.current &&
      !window.confirm("有未提交的账号表单，确定放弃吗？")
    )
      return;
    accountDirty.current = false;
    setError("");
    setMessage("");
    setTab(next);
  }

  const refreshSession = useCallback(async () => {
    setError("");
    setMode("loading");
    try {
      const session = await fetch("/auth/session", {
        method: "GET",
        headers: { Accept: "application/json" },
        credentials: "same-origin",
        cache: "no-store",
      });
      if (session.status === 401) {
        setMember(null);
        setMode("unauthenticated");
        return;
      }
      if (!session.ok) {
        setMember(null);
        setMode("unavailable");
        return;
      }

      try {
        const current = (await fetchLocalCurrent()) as LocalMember;
        setMember(current);
        setMode("local");
      } catch (cause) {
        if (errorStatus(cause) === 401) {
          setMember(null);
          setMode("oidc");
          return;
        }
        setMember(null);
        setMode("unavailable");
      }
    } catch {
      setMember(null);
      setMode("unavailable");
    }
  }, []);

  useEffect(() => {
    void refreshSession();
  }, [refreshSession]);

  async function handleLogin(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (commandLock.current) return;
    commandLock.current = true;
    setBusy(true);
    setError("");
    setMessage("");
    const form = event.currentTarget;
    const data = new FormData(form);
    try {
      await localLogin({
        organizationId: String(data.get("organizationId") ?? "").trim(),
        userId: String(data.get("userId") ?? "").trim(),
        password: String(data.get("password") ?? ""),
      });
      form.reset();
      accountDirty.current = false;
      await refreshSession();
      setMessage("已使用本地成员账号登录。");
    } catch (cause) {
      setError(humanizeError(cause, "登录未完成。"));
    } finally {
      commandLock.current = false;
      setBusy(false);
    }
  }

  async function handleLogout() {
    if (commandLock.current) return;
    commandLock.current = true;
    setBusy(true);
    setError("");
    setMessage("");
    try {
      if (mode === "local") await localLogout();
      else if (mode === "oidc") await oidcLogout();
      setMember(null);
      setAccountOpen(false);
      setLastMember(null);
      setLastBinding(null);
      accountDirty.current = false;
      setMode("unauthenticated");
      setMessage("已退出当前会话。");
    } catch (cause) {
      if (errorStatus(cause) === 401) {
        setMember(null);
        setMode("unauthenticated");
      }
      setError(humanizeError(cause, "退出未完成。"));
    } finally {
      commandLock.current = false;
      setBusy(false);
    }
  }

  async function handleChangePassword(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (commandLock.current) return;
    commandLock.current = true;
    setBusy(true);
    setError("");
    setMessage("");
    const form = event.currentTarget;
    const data = new FormData(form);
    try {
      await changeLocalPassword({
        currentPassword: String(data.get("currentPassword") ?? ""),
        newPassword: requireNewPassword(String(data.get("newPassword") ?? "")),
      });
      form.reset();
      accountDirty.current = false;
      setMember(null);
      setAccountOpen(false);
      setLastMember(null);
      setLastBinding(null);
      accountDirty.current = false;
      setMode("unauthenticated");
      setMessage("密码已更新，旧会话已失效，请重新登录。");
    } catch (cause) {
      handleProtectedError(cause, "密码修改未完成。");
    } finally {
      commandLock.current = false;
      setBusy(false);
    }
  }

  async function handleCreateMember(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (commandLock.current) return;
    commandLock.current = true;
    setBusy(true);
    setError("");
    setMessage("");
    const form = event.currentTarget;
    const data = new FormData(form);
    try {
      const result = (await createLocalMember({
        displayName: String(data.get("displayName") ?? "").trim(),
        email: String(data.get("email") ?? "").trim(),
        password: requireNewPassword(String(data.get("password") ?? "")),
      })) as MemberResult;
      setLastMember(result);
      accountDirty.current = false;
      form.reset();
      accountDirty.current = false;
      setMessage(
        "成员已创建；请保存返回的 UserID 与 revision 用于后续 CAS 操作。",
      );
    } catch (cause) {
      handleProtectedError(cause, "成员创建未完成。");
    } finally {
      commandLock.current = false;
      setBusy(false);
    }
  }

  async function handleDisableMember(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (commandLock.current) return;
    commandLock.current = true;
    setBusy(true);
    setError("");
    setMessage("");
    const data = new FormData(event.currentTarget);
    if (
      !window.confirm(
        `停用成员 ${String(data.get("userId") ?? "")}？该成员的旧会话将失效。`,
      )
    ) {
      commandLock.current = false;
      setBusy(false);
      return;
    }
    try {
      const result = (await disableLocalMember(
        String(data.get("userId") ?? "").trim(),
        positiveRevision(data.get("expectedRevision")),
      )) as MemberResult;
      setLastMember(result);
      accountDirty.current = false;
      setMessage(
        "成员已停用；服务端 session validity 将在后续请求中立即生效。",
      );
    } catch (cause) {
      handleProtectedError(cause, "成员停用未完成。");
    } finally {
      commandLock.current = false;
      setBusy(false);
    }
  }

  async function handleResetCredential(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (commandLock.current) return;
    commandLock.current = true;
    setBusy(true);
    setError("");
    setMessage("");
    const form = event.currentTarget;
    const data = new FormData(form);
    try {
      const result = (await resetLocalMemberCredential(
        String(data.get("userId") ?? "").trim(),
        {
          expectedUserRevision: positiveRevision(
            data.get("expectedUserRevision"),
          ),
          expectedCredentialRevision: positiveRevision(
            data.get("expectedCredentialRevision"),
          ),
          password: requireNewPassword(String(data.get("password") ?? "")),
        },
      )) as MemberResult;
      setLastMember(result);
      accountDirty.current = false;
      form.reset();
      accountDirty.current = false;
      setMessage("成员凭据已重置；旧会话按服务端集中有效性规则失效。");
    } catch (cause) {
      handleProtectedError(cause, "凭据重置未完成。");
    } finally {
      commandLock.current = false;
      setBusy(false);
    }
  }

  async function handleAssignRole(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (commandLock.current) return;
    commandLock.current = true;
    setBusy(true);
    setError("");
    setMessage("");
    const form = event.currentTarget;
    const data = new FormData(form);
    try {
      const result = (await assignLocalProjectRole({
        projectId: String(data.get("projectId") ?? "").trim(),
        userId: String(data.get("userId") ?? "").trim(),
        roleId: String(data.get("roleId") ?? "").trim(),
      })) as BindingResult;
      setLastBinding(result);
      accountDirty.current = false;
      form.reset();
      accountDirty.current = false;
      setMessage(
        "项目角色已分配；实际权限由 durable RoleBinding 与服务端 Guard 决定。",
      );
    } catch (cause) {
      handleProtectedError(cause, "项目角色分配未完成。");
    } finally {
      commandLock.current = false;
      setBusy(false);
    }
  }

  async function handleRevokeRole(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (commandLock.current) return;
    commandLock.current = true;
    setBusy(true);
    setError("");
    setMessage("");
    const data = new FormData(event.currentTarget);
    if (
      !window.confirm(`撤销角色绑定 ${String(data.get("bindingId") ?? "")}？`)
    ) {
      commandLock.current = false;
      setBusy(false);
      return;
    }
    try {
      const result = (await revokeLocalProjectRole(
        String(data.get("bindingId") ?? "").trim(),
        positiveRevision(data.get("expectedRevision")),
      )) as BindingResult;
      setLastBinding(result);
      accountDirty.current = false;
      setMessage(
        "项目角色已撤销；下一次受保护请求将按 durable grant 重新判定。",
      );
    } catch (cause) {
      handleProtectedError(cause, "项目角色撤销未完成。");
    } finally {
      commandLock.current = false;
      setBusy(false);
    }
  }

  function handleProtectedError(cause: unknown, fallback: string) {
    if (errorStatus(cause) === 401) {
      setMember(null);
      setAccountOpen(false);
      setLastMember(null);
      setLastBinding(null);
      accountDirty.current = false;
      setMode("unauthenticated");
    }
    setError(humanizeError(cause, fallback));
  }

  const brand = (
    <div className="auth-brand">
      <span className="brand-mark">I</span>
      <strong>IoT Delivery</strong>
      <span>研发交付工作台</span>
    </div>
  );
  if (mode === "loading")
    return (
      <main className="auth-page" aria-busy="true">
        {brand}
        <section className="login-panel">
          <Heading
            title="正在验证当前会话…"
            description="确认服务端身份后进入交付工作空间。"
          />
        </section>
      </main>
    );
  if (mode === "unavailable")
    return (
      <main className="auth-page">
        {brand}
        <section className="login-panel">
          <div className="auth-glyph">
            <ShieldCheckIcon className="icon" />
          </div>
          <Heading
            title="身份服务暂不可用"
            description="当前无法确认会话有效性，因此不会进入业务工作区。"
          />
          <Action primary onClick={() => void refreshSession()}>
            重新检查
          </Action>
          {error ? <p role="alert">{error}</p> : null}
        </section>
      </main>
    );
  if (mode === "unauthenticated")
    return (
      <main className="auth-page">
        {brand}
        <section className="login-panel">
          <div className="auth-glyph">
            <KeyRoundIcon className="icon" />
          </div>
          <Heading
            title="登录 IoT 研发管理"
            description="使用本地成员账号登录，或选择组织配置的 OIDC。"
          />
          {message ? (
            <Notice title="状态" role="status">
              {message}
            </Notice>
          ) : null}
          {error ? (
            <p role="alert" className="auth-error">
              {error}
            </p>
          ) : null}
          <form onSubmit={handleLogin} aria-label="本地成员登录">
            <Field id="local-organization" label="组织 ID">
              <Input
                id="local-organization"
                name="organizationId"
                autoComplete="organization"
                required
              />
            </Field>
            <Field id="local-user" label="成员 UserID">
              <Input
                id="local-user"
                name="userId"
                autoComplete="username"
                required
              />
            </Field>
            <Field id="local-password" label="密码">
              <Input
                id="local-password"
                name="password"
                type="password"
                autoComplete="current-password"
                required
              />
            </Field>
            <Action className="full" primary type="submit" busy={busy}>
              使用本地成员账号登录
            </Action>
          </form>
          <div className="or">
            <span>或</span>
          </div>
          <a href="/auth/login" className="btn full">
            使用 OIDC 登录
          </a>
          <p className="caption">
            身份与会话由服务端确认，浏览器不保存管理员密钥。
          </p>
        </section>
      </main>
    );
  const accountTitles: Record<AccountTab, string> = {
    profile: "当前成员",
    security: "修改密码",
    members: "创建成员",
    disable: "停用成员",
    reset: "重置凭据",
    roles: "项目角色",
    revoke: "撤销角色",
    receipts: "操作回执",
    rules: "交付规则",
    obsidian: "Obsidian 投影",
    notifications: "通知与提醒",
    runtime: "运行时与 MCP",
  };
  const accountTabs: [AccountTab, string][] = [
    ["profile", "当前成员"],
    ["security", "修改密码"],
    ["members", "创建成员"],
    ["disable", "停用成员"],
    ["reset", "重置凭据"],
    ["roles", "项目角色"],
    ["revoke", "撤销角色"],
    ["receipts", "操作回执"],
  ];
  const account =
    mode === "local" && member ? (
      <div className="settings-layout">
        <nav className="subnav" aria-label="账号管理分区">
          <div className="subnav-group">账号与成员</div>
          {accountTabs.map(([key, title]) => (
            <button
              key={key}
              className={`subnav-item${tab === key ? " active" : ""}`}
              type="button"
              disabled={busy}
              onClick={() => selectAccountTab(key)}
              aria-current={tab === key ? "page" : undefined}
            >
              {title}
            </button>
          ))}
          <div className="subnav-group">系统规则 · 只读</div>
          {(
            ["rules", "obsidian", "notifications", "runtime"] as AccountTab[]
          ).map((key) => (
            <button
              key={key}
              className={`subnav-item${tab === key ? " active" : ""}`}
              type="button"
              disabled={busy}
              onClick={() => selectAccountTab(key)}
              aria-current={tab === key ? "page" : undefined}
            >
              {accountTitles[key]}
            </button>
          ))}
        </nav>
        <section
          className="settings-main"
          key={tab}
          onInput={() => {
            accountDirty.current = true;
          }}
        >
          {["rules", "obsidian", "notifications", "runtime"].includes(tab) ? (
            <PolicyReference view={tab as PolicyKey} />
          ) : (
            <>
              <Heading
                title={accountTitles[tab]}
                description={
                  tab === "profile"
                    ? "服务端重新验证的成员与当前会话信息。"
                    : "账号与受保护管理操作"
                }
                actions={<Chip>服务端授权</Chip>}
              />
              <p className="caption account-guard">
                UI 不授予管理员权限。成员与项目角色操作由 durable
                system-administrator Guard 最终判定。
              </p>
              {error ? (
                <p role="alert" className="auth-error">
                  {error}
                </p>
              ) : null}
              {message ? (
                <Notice title="操作回执" role="status">
                  {message}
                </Notice>
              ) : null}
              {tab === "profile" ? (
                <Profile member={member} />
              ) : tab === "security" ? (
                <PasswordForm busy={busy} onSubmit={handleChangePassword} />
              ) : ["members", "disable", "reset"].includes(tab) ? (
                <MemberAdministration
                  mode={tab as "members" | "disable" | "reset"}
                  busy={busy}
                  lastResult={lastMember}
                  onCreate={handleCreateMember}
                  onDisable={handleDisableMember}
                  onReset={handleResetCredential}
                />
              ) : ["roles", "revoke"].includes(tab) ? (
                <ProjectRoleAdministration
                  mode={tab as "roles" | "revoke"}
                  busy={busy}
                  lastResult={lastBinding}
                  onAssign={handleAssignRole}
                  onRevoke={handleRevokeRole}
                />
              ) : (
                <>
                  <Notice title="仅展示当前会话的最近返回值" role="note">
                    不是完整审计列表。请保留目标 ID 与
                    revision，后续操作仍由服务端重新核验。
                  </Notice>
                  {lastMember ? (
                    <ResultBlock
                      title="最近成员结果"
                      values={memberResultValues(lastMember)}
                    />
                  ) : null}
                  {lastBinding ? (
                    <ResultBlock
                      title="最近 RoleBinding 结果"
                      values={bindingResultValues(lastBinding)}
                    />
                  ) : null}
                  {!lastMember && !lastBinding ? (
                    <p className="caption">当前会话尚无管理操作回执。</p>
                  ) : null}
                </>
              )}
            </>
          )}
        </section>
      </div>
    ) : null;
  return (
    <DeliveryWorkspace
      sessionName={
        mode === "local"
          ? member?.displayName?.trim() || member?.userId || "本地成员"
          : "OIDC 会话"
      }
      sessionDescription={
        mode === "local"
          ? `${member?.organizationId ?? ""} · ${member?.userId ?? ""}`
          : "组织 OIDC BFF 会话"
      }
      onAccount={
        mode === "local"
          ? () => {
              if (accountOpen && !leaveAccount()) return;
              setTab("profile");
              setAccountOpen(true);
            }
          : undefined
      }
      onLogout={() => {
        if (leaveAccount()) void handleLogout();
      }}
      sessionBusy={busy}
      accountOpen={accountOpen}
      onLeaveAccount={leaveAccount}
      accountContent={account}
      onSessionExpired={expireSession}
      notice={
        !accountOpen && (message || error) ? (
          <>
            {message ? (
              <p role="status" className="caption">
                {message}
              </p>
            ) : null}
            {error ? (
              <p role="alert" className="auth-error">
                {error}
              </p>
            ) : null}
          </>
        ) : undefined
      }
    />
  );
}

function Profile({ member }: { member: LocalMember }) {
  return (
    <section
      className="profile-section"
      aria-labelledby="current-member-heading"
    >
      <div>
        <h2 id="current-member-heading" className="font-medium">
          当前 verified member
        </h2>
        <p className="text-sm text-muted-foreground">
          显示值来自当前服务端 session 重验，不从浏览器角色快照恢复。
        </p>
      </div>
      <dl className="property-grid">
        <Value label="OrganizationID" value={member.organizationId} />
        <Value label="UserID" value={member.userId} />
        <Value label="显示名称" value={member.displayName || "—"} />
        <Value label="邮箱" value={member.email || "—"} />
        <Value label="User revision" value={String(member.userRevision)} />
        <Value
          label="Session revision"
          value={String(member.sessionRevision)}
        />
      </dl>
    </section>
  );
}

function PasswordForm({
  busy,
  onSubmit,
}: {
  busy: boolean;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
}) {
  return (
    <form className="admin-form" onSubmit={onSubmit} aria-label="修改本人密码">
      <p className="text-sm text-muted-foreground">
        修改成功后服务端会使旧会话失效，本页面立即返回登录状态。
      </p>
      <Field id="current-password" label="当前密码">
        <Input
          id="current-password"
          name="currentPassword"
          type="password"
          autoComplete="current-password"
          required
        />
      </Field>
      <Field id="new-password" label="新密码">
        <Input
          id="new-password"
          name="newPassword"
          type="password"
          autoComplete="new-password"
          aria-describedby="new-password-policy"
          required
        />
        <p id="new-password-policy" className="text-sm text-muted-foreground">
          至少 15 个字符，支持空格、中文和密码管理器。
        </p>
      </Field>
      <Button type="submit" disabled={busy}>
        更新密码
      </Button>
    </form>
  );
}

function MemberAdministration({
  mode,
  busy,
  lastResult,
  onCreate,
  onDisable,
  onReset,
}: {
  mode: "members" | "disable" | "reset";
  busy: boolean;
  lastResult: MemberResult | null;
  onCreate: (event: FormEvent<HTMLFormElement>) => void;
  onDisable: (event: FormEvent<HTMLFormElement>) => void;
  onReset: (event: FormEvent<HTMLFormElement>) => void;
}) {
  return (
    <section className="account-section" aria-labelledby="member-admin-heading">
      <GuardNotice id="member-admin-heading" title="成员管理">
        当前 UI 不判断你是否为管理员；提交后由服务端 durable authorization
        决定。403 表示没有该操作权限。
      </GuardNotice>
      {lastResult ? (
        <ResultBlock
          title="最近成员结果"
          values={memberResultValues(lastResult)}
        />
      ) : null}
      <div className="admin-forms">
        {mode === "members" ? (
          <form
            className="admin-form"
            onSubmit={onCreate}
            aria-label="创建成员"
          >
            <h3 className="font-medium">创建成员</h3>
            <Field id="member-display-name" label="显示名称">
              <Input id="member-display-name" name="displayName" required />
            </Field>
            <Field id="member-email" label="邮箱（可选）">
              <Input
                id="member-email"
                name="email"
                type="email"
                autoComplete="off"
              />
            </Field>
            <Field id="member-password" label="初始密码">
              <Input
                id="member-password"
                name="password"
                type="password"
                autoComplete="new-password"
                aria-describedby="member-password-policy"
                required
              />
              <p
                id="member-password-policy"
                className="text-sm text-muted-foreground"
              >
                至少 15 个字符。
              </p>
            </Field>
            <Button type="submit" disabled={busy}>
              创建
            </Button>
          </form>
        ) : null}

        {mode === "disable" ? (
          <form
            className="admin-form"
            onSubmit={onDisable}
            aria-label="停用成员"
          >
            <h3 className="font-medium">停用成员</h3>
            <Field id="disable-user-id" label="UserID">
              <Input id="disable-user-id" name="userId" required />
            </Field>
            <Field id="disable-user-revision" label="Expected user revision">
              <Input
                id="disable-user-revision"
                name="expectedRevision"
                type="number"
                min="1"
                step="1"
                required
              />
            </Field>
            <Button type="submit" variant="destructive" disabled={busy}>
              停用
            </Button>
          </form>
        ) : null}

        {mode === "reset" ? (
          <form
            className="admin-form"
            onSubmit={onReset}
            aria-label="重置成员凭据"
          >
            <h3 className="font-medium">重置凭据</h3>
            <Field id="reset-user-id" label="UserID">
              <Input id="reset-user-id" name="userId" required />
            </Field>
            <Field id="reset-user-revision" label="Expected user revision">
              <Input
                id="reset-user-revision"
                name="expectedUserRevision"
                type="number"
                min="1"
                step="1"
                required
              />
            </Field>
            <Field
              id="reset-credential-revision"
              label="Expected credential revision"
            >
              <Input
                id="reset-credential-revision"
                name="expectedCredentialRevision"
                type="number"
                min="1"
                step="1"
                required
              />
            </Field>
            <Field id="reset-password" label="新密码">
              <Input
                id="reset-password"
                name="password"
                type="password"
                autoComplete="new-password"
                aria-describedby="reset-password-policy"
                required
              />
              <p
                id="reset-password-policy"
                className="text-sm text-muted-foreground"
              >
                至少 15 个字符。
              </p>
            </Field>
            <Button type="submit" disabled={busy}>
              重置
            </Button>
          </form>
        ) : null}
      </div>
    </section>
  );
}

function ProjectRoleAdministration({
  mode,
  busy,
  lastResult,
  onAssign,
  onRevoke,
}: {
  mode: "roles" | "revoke";
  busy: boolean;
  lastResult: BindingResult | null;
  onAssign: (event: FormEvent<HTMLFormElement>) => void;
  onRevoke: (event: FormEvent<HTMLFormElement>) => void;
}) {
  return (
    <section className="account-section" aria-labelledby="project-role-heading">
      <GuardNotice id="project-role-heading" title="项目角色">
        RoleID 只是提交给服务端的 canonical identifier；UI
        不维护授权快照，也不会把输入的角色当成已授予权限。
      </GuardNotice>
      {lastResult ? (
        <ResultBlock
          title="最近 RoleBinding 结果"
          values={bindingResultValues(lastResult)}
        />
      ) : null}
      <div className="admin-forms">
        {mode === "roles" ? (
          <form
            className="admin-form"
            onSubmit={onAssign}
            aria-label="分配项目角色"
          >
            <h3 className="font-medium">分配角色</h3>
            <Field id="role-project-id" label="ProjectID">
              <Input id="role-project-id" name="projectId" required />
            </Field>
            <Field id="role-user-id" label="UserID">
              <Input id="role-user-id" name="userId" required />
            </Field>
            <Field id="role-id" label="RoleID">
              <Input
                id="role-id"
                name="roleId"
                placeholder="例如 contributor"
                required
              />
            </Field>
            <Button type="submit" disabled={busy}>
              <ShieldCheckIcon />
              分配
            </Button>
          </form>
        ) : null}

        {mode === "revoke" ? (
          <form
            className="admin-form"
            onSubmit={onRevoke}
            aria-label="撤销项目角色"
          >
            <h3 className="font-medium">撤销 RoleBinding</h3>
            <Field id="binding-id" label="BindingID">
              <Input id="binding-id" name="bindingId" required />
            </Field>
            <Field id="binding-revision" label="Expected binding revision">
              <Input
                id="binding-revision"
                name="expectedRevision"
                type="number"
                min="1"
                step="1"
                required
              />
            </Field>
            <Button type="submit" variant="destructive" disabled={busy}>
              撤销
            </Button>
          </form>
        ) : null}
      </div>
    </section>
  );
}

function Field({
  id,
  label,
  children,
}: {
  id: string;
  label: string;
  children: ReactNode;
}) {
  return (
    <BaseField className="field">
      <Label htmlFor={id}>{label}</Label>
      {children}
    </BaseField>
  );
}

function TabButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <Button
      type="button"
      variant={active ? "default" : "outline"}
      size="sm"
      aria-pressed={active}
      onClick={onClick}
    >
      {children}
    </Button>
  );
}

function GuardNotice({
  id,
  title,
  children,
}: {
  id: string;
  title: string;
  children: ReactNode;
}) {
  return (
    <Alert role="note" className="account-note">
      <ShieldCheckIcon />
      <AlertTitle id={id}>{title}</AlertTitle>
      <AlertDescription>{children}</AlertDescription>
    </Alert>
  );
}

function Value({ label, value }: { label: string; value: string }) {
  return (
    <div className="prop">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="break-all text-sm font-medium">{value}</dd>
    </div>
  );
}

function ResultBlock({
  title,
  values,
}: {
  title: string;
  values: Array<[string, string]>;
}) {
  return (
    <div className="rounded-lg border bg-muted/20 p-4" role="status">
      <p className="mb-2 text-sm font-medium">{title}</p>
      <dl className="grid gap-2 text-sm sm:grid-cols-2">
        {values.map(([label, value]) => (
          <Value key={label} label={label} value={value} />
        ))}
      </dl>
    </div>
  );
}

function memberResultValues(result: MemberResult): Array<[string, string]> {
  return [
    [
      "OrganizationID",
      String(result.OrganizationID ?? result.organizationId ?? ""),
    ],
    ["UserID", String(result.UserID ?? result.userId ?? "")],
    ["User revision", String(result.UserRevision ?? result.userRevision ?? "")],
    [
      "Credential revision",
      String(result.CredentialRevision ?? result.credentialRevision ?? ""),
    ],
  ];
}

function bindingResultValues(result: BindingResult): Array<[string, string]> {
  return [
    ["BindingID", String(result.BindingID ?? result.bindingId ?? "")],
    ["ProjectID", String(result.ProjectID ?? result.projectId ?? "")],
    ["UserID", String(result.UserID ?? result.userId ?? "")],
    ["RoleID", String(result.RoleID ?? result.roleId ?? "")],
    ["Status", String(result.Status ?? result.status ?? "")],
    ["Revision", String(result.Revision ?? result.revision ?? "")],
  ];
}

function requireNewPassword(value: string): string {
  if (
    Array.from(value).length < 15 ||
    new TextEncoder().encode(value).length > 4096
  ) {
    throw new Error(
      "新密码至少需要 15 个字符，最长 4096 字节；支持空格、中文和密码管理器。",
    );
  }
  return value;
}

function positiveRevision(value: FormDataEntryValue | null): number {
  const revision = Number(value);
  if (!Number.isInteger(revision) || revision < 1)
    throw new Error("revision 必须是正整数");
  return revision;
}

function errorStatus(cause: unknown): number | undefined {
  return cause instanceof Error ? (cause as RequestError).status : undefined;
}

function humanizeError(cause: unknown, fallback: string): string {
  const status = errorStatus(cause);
  if (status === 400) return "输入不符合服务端合同，请检查字段与密码要求。";
  if (status === 401) return "会话已失效，请重新登录。";
  if (status === 403) return "权限不足，或 Origin / CSRF 安全校验未通过。";
  if (status === 404) return "目标成员、项目或 RoleBinding 不存在。";
  if (status === 409)
    return "数据 revision 已变化或目标状态冲突，请使用最新 revision 后重试。";
  if (status === 429)
    return "尝试次数过多，已临时限流。请等待后再试；已有会话不受影响。";
  if (status === 503) return "身份或权限服务暂不可用，请稍后重试。";
  if (cause instanceof Error && cause.message)
    return `${fallback} ${cause.message}`;
  return fallback;
}
