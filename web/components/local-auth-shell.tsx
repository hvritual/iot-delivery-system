"use client";

import { type FormEvent, type ReactNode, useCallback, useEffect, useState } from "react";
import { KeyRoundIcon, LogOutIcon, ShieldCheckIcon, UserRoundCogIcon } from "lucide-react";

import { DeliveryWorkspace } from "@/components/delivery-workspace";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button, buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
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

type AuthMode = "loading" | "unauthenticated" | "local" | "oidc" | "unavailable";
type AccountTab = "profile" | "security" | "members" | "roles";
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
        const current = await fetchLocalCurrent() as LocalMember;
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
      await refreshSession();
      setMessage("已使用本地成员账号登录。");
    } catch (cause) {
      setError(humanizeError(cause, "登录未完成。"));
    } finally {
      setBusy(false);
    }
  }

  async function handleLogout() {
    setBusy(true);
    setError("");
    setMessage("");
    try {
      if (mode === "local") await localLogout();
      else if (mode === "oidc") await oidcLogout();
      setMember(null);
      setAccountOpen(false);
      setMode("unauthenticated");
      setMessage("已退出当前会话。");
    } catch (cause) {
      if (errorStatus(cause) === 401) {
        setMember(null);
        setMode("unauthenticated");
      }
      setError(humanizeError(cause, "退出未完成。"));
    } finally {
      setBusy(false);
    }
  }

  async function handleChangePassword(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
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
      setMember(null);
      setAccountOpen(false);
      setMode("unauthenticated");
      setMessage("密码已更新，旧会话已失效，请重新登录。");
    } catch (cause) {
      handleProtectedError(cause, "密码修改未完成。");
    } finally {
      setBusy(false);
    }
  }

  async function handleCreateMember(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    setMessage("");
    const form = event.currentTarget;
    const data = new FormData(form);
    try {
      const result = await createLocalMember({
        displayName: String(data.get("displayName") ?? "").trim(),
        email: String(data.get("email") ?? "").trim(),
        password: requireNewPassword(String(data.get("password") ?? "")),
      }) as MemberResult;
      setLastMember(result);
      form.reset();
      setMessage("成员已创建；请保存返回的 UserID 与 revision 用于后续 CAS 操作。");
    } catch (cause) {
      handleProtectedError(cause, "成员创建未完成。");
    } finally {
      setBusy(false);
    }
  }

  async function handleDisableMember(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    setMessage("");
    const data = new FormData(event.currentTarget);
    try {
      const result = await disableLocalMember(
        String(data.get("userId") ?? "").trim(),
        positiveRevision(data.get("expectedRevision")),
      ) as MemberResult;
      setLastMember(result);
      setMessage("成员已停用；服务端 session validity 将在后续请求中立即生效。");
    } catch (cause) {
      handleProtectedError(cause, "成员停用未完成。");
    } finally {
      setBusy(false);
    }
  }

  async function handleResetCredential(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    setMessage("");
    const form = event.currentTarget;
    const data = new FormData(form);
    try {
      const result = await resetLocalMemberCredential(String(data.get("userId") ?? "").trim(), {
        expectedUserRevision: positiveRevision(data.get("expectedUserRevision")),
        expectedCredentialRevision: positiveRevision(data.get("expectedCredentialRevision")),
        password: requireNewPassword(String(data.get("password") ?? "")),
      }) as MemberResult;
      setLastMember(result);
      form.reset();
      setMessage("成员凭据已重置；旧会话按服务端集中有效性规则失效。");
    } catch (cause) {
      handleProtectedError(cause, "凭据重置未完成。");
    } finally {
      setBusy(false);
    }
  }

  async function handleAssignRole(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    setMessage("");
    const form = event.currentTarget;
    const data = new FormData(form);
    try {
      const result = await assignLocalProjectRole({
        projectId: String(data.get("projectId") ?? "").trim(),
        userId: String(data.get("userId") ?? "").trim(),
        roleId: String(data.get("roleId") ?? "").trim(),
      }) as BindingResult;
      setLastBinding(result);
      form.reset();
      setMessage("项目角色已分配；实际权限由 durable RoleBinding 与服务端 Guard 决定。");
    } catch (cause) {
      handleProtectedError(cause, "项目角色分配未完成。");
    } finally {
      setBusy(false);
    }
  }

  async function handleRevokeRole(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    setMessage("");
    const data = new FormData(event.currentTarget);
    try {
      const result = await revokeLocalProjectRole(
        String(data.get("bindingId") ?? "").trim(),
        positiveRevision(data.get("expectedRevision")),
      ) as BindingResult;
      setLastBinding(result);
      setMessage("项目角色已撤销；下一次受保护请求将按 durable grant 重新判定。");
    } catch (cause) {
      handleProtectedError(cause, "项目角色撤销未完成。");
    } finally {
      setBusy(false);
    }
  }

  function handleProtectedError(cause: unknown, fallback: string) {
    if (errorStatus(cause) === 401) {
      setMember(null);
      setAccountOpen(false);
      setMode("unauthenticated");
    }
    setError(humanizeError(cause, fallback));
  }

  if (mode === "loading") {
    return (
      <main className="grid min-h-screen place-items-center bg-background px-4" aria-busy="true">
        <p className="text-sm text-muted-foreground">正在验证当前会话…</p>
      </main>
    );
  }

  if (mode === "unavailable") {
    return (
      <main className="grid min-h-screen place-items-center bg-background px-4">
        <Card className="w-full max-w-md">
          <CardHeader>
            <CardTitle>身份服务暂不可用</CardTitle>
            <CardDescription>当前无法确认会话有效性，因此不会进入业务工作区。</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <Button onClick={() => void refreshSession()}>重新检查</Button>
            {error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
          </CardContent>
        </Card>
      </main>
    );
  }

  if (mode === "unauthenticated") {
    return (
      <main className="grid min-h-screen place-items-center bg-muted/30 px-4 py-10">
        <Card className="w-full max-w-md">
          <CardHeader className="space-y-3">
            <div className="flex size-10 items-center justify-center rounded-lg border bg-background" aria-hidden="true">
              <KeyRoundIcon className="size-5" />
            </div>
            <div>
              <CardTitle>登录 IoT 研发管理</CardTitle>
              <CardDescription className="mt-2">本地成员账号无需配置 OIDC。OIDC 保留为显式的另一登录入口。</CardDescription>
            </div>
          </CardHeader>
          <CardContent className="space-y-5">
            {message ? <Alert><AlertTitle>状态</AlertTitle><AlertDescription>{message}</AlertDescription></Alert> : null}
            {error ? <p role="alert" className="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive">{error}</p> : null}
            <form className="space-y-4" onSubmit={handleLogin} aria-label="本地成员登录">
              <Field id="local-organization" label="组织 ID">
                <Input id="local-organization" name="organizationId" autoComplete="organization" required />
              </Field>
              <Field id="local-user" label="成员 UserID">
                <Input id="local-user" name="userId" autoComplete="username" required />
              </Field>
              <Field id="local-password" label="密码">
                <Input id="local-password" name="password" type="password" autoComplete="current-password" required />
              </Field>
              <Button className="w-full" type="submit" disabled={busy}>使用本地成员账号登录</Button>
            </form>
            <div className="relative text-center text-xs text-muted-foreground before:absolute before:top-1/2 before:left-0 before:w-full before:border-t">
              <span className="relative bg-card px-2">或</span>
            </div>
            <a href="/auth/login" className={buttonVariants({ variant: "outline", className: "w-full" })}>使用 OIDC 登录</a>
          </CardContent>
        </Card>
      </main>
    );
  }

  return (
    <div className="min-h-screen bg-background">
      <header className="sticky top-0 z-40 flex min-h-12 items-center justify-between gap-3 border-b bg-background/95 px-4 backdrop-blur" aria-label="当前会话">
        <div className="min-w-0">
          <p className="truncate text-sm font-medium">
            {mode === "local" ? (member?.displayName?.trim() || member?.userId || "本地成员") : "OIDC 会话"}
          </p>
          <p className="truncate text-xs text-muted-foreground">
            {mode === "local" ? `${member?.organizationId ?? ""} · ${member?.userId ?? ""}` : "身份由现有 OIDC BFF 会话提供"}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {mode === "local" ? (
            <Button variant="outline" size="sm" onClick={() => { setTab("profile"); setAccountOpen(true); }}>
              <UserRoundCogIcon />账号与管理
            </Button>
          ) : null}
          <Button variant="ghost" size="sm" onClick={() => void handleLogout()} disabled={busy}>
            <LogOutIcon />退出
          </Button>
        </div>
      </header>

      {message && !accountOpen ? <div className="px-4 pt-3"><p role="status" className="text-sm text-muted-foreground">{message}</p></div> : null}
      {error && !accountOpen ? <div className="px-4 pt-3"><p role="alert" className="text-sm text-destructive">{error}</p></div> : null}
      <DeliveryWorkspace />

      {mode === "local" && member ? (
        <Dialog open={accountOpen} onOpenChange={setAccountOpen}>
          <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-3xl">
            <DialogHeader>
              <DialogTitle>账号与受保护管理操作</DialogTitle>
              <DialogDescription>
                UI 不授予管理员权限。成员与项目角色操作仍由 YU-20/YU-24 的 durable system-administrator Guard 最终判定。
              </DialogDescription>
            </DialogHeader>

            <nav className="flex flex-wrap gap-2" aria-label="账号管理分区">
              <TabButton active={tab === "profile"} onClick={() => setTab("profile")}>当前成员</TabButton>
              <TabButton active={tab === "security"} onClick={() => setTab("security")}>修改密码</TabButton>
              <TabButton active={tab === "members"} onClick={() => setTab("members")}>成员管理</TabButton>
              <TabButton active={tab === "roles"} onClick={() => setTab("roles")}>项目角色</TabButton>
            </nav>

            {error ? <p role="alert" className="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive">{error}</p> : null}
            {message ? <p role="status" className="rounded-md border bg-muted/30 p-3 text-sm">{message}</p> : null}

            {tab === "profile" ? <Profile member={member} /> : null}
            {tab === "security" ? <PasswordForm busy={busy} onSubmit={handleChangePassword} /> : null}
            {tab === "members" ? (
              <MemberAdministration
                busy={busy}
                lastResult={lastMember}
                onCreate={handleCreateMember}
                onDisable={handleDisableMember}
                onReset={handleResetCredential}
              />
            ) : null}
            {tab === "roles" ? (
              <ProjectRoleAdministration
                busy={busy}
                lastResult={lastBinding}
                onAssign={handleAssignRole}
                onRevoke={handleRevokeRole}
              />
            ) : null}
          </DialogContent>
        </Dialog>
      ) : null}
    </div>
  );
}

function Profile({ member }: { member: LocalMember }) {
  return (
    <section className="grid gap-3 rounded-lg border p-4" aria-labelledby="current-member-heading">
      <div>
        <h2 id="current-member-heading" className="font-medium">当前 verified member</h2>
        <p className="text-sm text-muted-foreground">显示值来自当前服务端 session 重验，不从浏览器角色快照恢复。</p>
      </div>
      <dl className="grid gap-3 sm:grid-cols-2">
        <Value label="OrganizationID" value={member.organizationId} />
        <Value label="UserID" value={member.userId} />
        <Value label="显示名称" value={member.displayName || "—"} />
        <Value label="邮箱" value={member.email || "—"} />
        <Value label="User revision" value={String(member.userRevision)} />
        <Value label="Session revision" value={String(member.sessionRevision)} />
      </dl>
    </section>
  );
}

function PasswordForm({ busy, onSubmit }: { busy: boolean; onSubmit: (event: FormEvent<HTMLFormElement>) => void }) {
  return (
    <form className="space-y-4 rounded-lg border p-4" onSubmit={onSubmit} aria-label="修改本人密码">
      <p className="text-sm text-muted-foreground">修改成功后服务端会使旧会话失效，本页面立即返回登录状态。</p>
      <Field id="current-password" label="当前密码">
        <Input id="current-password" name="currentPassword" type="password" autoComplete="current-password" required />
      </Field>
      <Field id="new-password" label="新密码">
        <Input id="new-password" name="newPassword" type="password" autoComplete="new-password" aria-describedby="new-password-policy" required />
        <p id="new-password-policy" className="text-sm text-muted-foreground">至少 15 个字符，支持空格、中文和密码管理器。</p>
      </Field>
      <Button type="submit" disabled={busy}>更新密码</Button>
    </form>
  );
}

function MemberAdministration({ busy, lastResult, onCreate, onDisable, onReset }: {
  busy: boolean;
  lastResult: MemberResult | null;
  onCreate: (event: FormEvent<HTMLFormElement>) => void;
  onDisable: (event: FormEvent<HTMLFormElement>) => void;
  onReset: (event: FormEvent<HTMLFormElement>) => void;
}) {
  return (
    <section className="space-y-4" aria-labelledby="member-admin-heading">
      <GuardNotice id="member-admin-heading" title="成员管理">
        当前 UI 不判断你是否为管理员；提交后由服务端 durable authorization 决定。403 表示没有该操作权限。
      </GuardNotice>
      {lastResult ? <ResultBlock title="最近成员结果" values={memberResultValues(lastResult)} /> : null}
      <div className="grid gap-4 lg:grid-cols-3">
        <form className="space-y-3 rounded-lg border p-4" onSubmit={onCreate} aria-label="创建成员">
          <h3 className="font-medium">创建成员</h3>
          <Field id="member-display-name" label="显示名称"><Input id="member-display-name" name="displayName" required /></Field>
          <Field id="member-email" label="邮箱（可选）"><Input id="member-email" name="email" type="email" autoComplete="off" /></Field>
          <Field id="member-password" label="初始密码"><Input id="member-password" name="password" type="password" autoComplete="new-password" aria-describedby="member-password-policy" required /><p id="member-password-policy" className="text-sm text-muted-foreground">至少 15 个字符。</p></Field>
          <Button type="submit" disabled={busy}>创建</Button>
        </form>

        <form className="space-y-3 rounded-lg border p-4" onSubmit={onDisable} aria-label="停用成员">
          <h3 className="font-medium">停用成员</h3>
          <Field id="disable-user-id" label="UserID"><Input id="disable-user-id" name="userId" required /></Field>
          <Field id="disable-user-revision" label="Expected user revision"><Input id="disable-user-revision" name="expectedRevision" type="number" min="1" step="1" required /></Field>
          <Button type="submit" variant="destructive" disabled={busy}>停用</Button>
        </form>

        <form className="space-y-3 rounded-lg border p-4" onSubmit={onReset} aria-label="重置成员凭据">
          <h3 className="font-medium">重置凭据</h3>
          <Field id="reset-user-id" label="UserID"><Input id="reset-user-id" name="userId" required /></Field>
          <Field id="reset-user-revision" label="Expected user revision"><Input id="reset-user-revision" name="expectedUserRevision" type="number" min="1" step="1" required /></Field>
          <Field id="reset-credential-revision" label="Expected credential revision"><Input id="reset-credential-revision" name="expectedCredentialRevision" type="number" min="1" step="1" required /></Field>
          <Field id="reset-password" label="新密码"><Input id="reset-password" name="password" type="password" autoComplete="new-password" aria-describedby="reset-password-policy" required /><p id="reset-password-policy" className="text-sm text-muted-foreground">至少 15 个字符。</p></Field>
          <Button type="submit" disabled={busy}>重置</Button>
        </form>
      </div>
    </section>
  );
}

function ProjectRoleAdministration({ busy, lastResult, onAssign, onRevoke }: {
  busy: boolean;
  lastResult: BindingResult | null;
  onAssign: (event: FormEvent<HTMLFormElement>) => void;
  onRevoke: (event: FormEvent<HTMLFormElement>) => void;
}) {
  return (
    <section className="space-y-4" aria-labelledby="project-role-heading">
      <GuardNotice id="project-role-heading" title="项目角色">
        RoleID 只是提交给服务端的 canonical identifier；UI 不维护授权快照，也不会把输入的角色当成已授予权限。
      </GuardNotice>
      {lastResult ? <ResultBlock title="最近 RoleBinding 结果" values={bindingResultValues(lastResult)} /> : null}
      <div className="grid gap-4 lg:grid-cols-2">
        <form className="space-y-3 rounded-lg border p-4" onSubmit={onAssign} aria-label="分配项目角色">
          <h3 className="font-medium">分配角色</h3>
          <Field id="role-project-id" label="ProjectID"><Input id="role-project-id" name="projectId" required /></Field>
          <Field id="role-user-id" label="UserID"><Input id="role-user-id" name="userId" required /></Field>
          <Field id="role-id" label="RoleID"><Input id="role-id" name="roleId" placeholder="例如 contributor" required /></Field>
          <Button type="submit" disabled={busy}><ShieldCheckIcon />分配</Button>
        </form>

        <form className="space-y-3 rounded-lg border p-4" onSubmit={onRevoke} aria-label="撤销项目角色">
          <h3 className="font-medium">撤销 RoleBinding</h3>
          <Field id="binding-id" label="BindingID"><Input id="binding-id" name="bindingId" required /></Field>
          <Field id="binding-revision" label="Expected binding revision"><Input id="binding-revision" name="expectedRevision" type="number" min="1" step="1" required /></Field>
          <Button type="submit" variant="destructive" disabled={busy}>撤销</Button>
        </form>
      </div>
    </section>
  );
}

function Field({ id, label, children }: { id: string; label: string; children: ReactNode }) {
  return <div className="space-y-1.5"><Label htmlFor={id}>{label}</Label>{children}</div>;
}

function TabButton({ active, onClick, children }: { active: boolean; onClick: () => void; children: ReactNode }) {
  return <Button type="button" variant={active ? "default" : "outline"} size="sm" aria-pressed={active} onClick={onClick}>{children}</Button>;
}

function GuardNotice({ id, title, children }: { id: string; title: string; children: ReactNode }) {
  return (
    <Alert role="note">
      <ShieldCheckIcon />
      <AlertTitle id={id}>{title}</AlertTitle>
      <AlertDescription>{children}</AlertDescription>
    </Alert>
  );
}

function Value({ label, value }: { label: string; value: string }) {
  return <div><dt className="text-xs text-muted-foreground">{label}</dt><dd className="break-all text-sm font-medium">{value}</dd></div>;
}

function ResultBlock({ title, values }: { title: string; values: Array<[string, string]> }) {
  return (
    <div className="rounded-lg border bg-muted/20 p-4" role="status">
      <p className="mb-2 text-sm font-medium">{title}</p>
      <dl className="grid gap-2 text-sm sm:grid-cols-2">
        {values.map(([label, value]) => <Value key={label} label={label} value={value} />)}
      </dl>
    </div>
  );
}

function memberResultValues(result: MemberResult): Array<[string, string]> {
  return [
    ["OrganizationID", String(result.OrganizationID ?? result.organizationId ?? "")],
    ["UserID", String(result.UserID ?? result.userId ?? "")],
    ["User revision", String(result.UserRevision ?? result.userRevision ?? "")],
    ["Credential revision", String(result.CredentialRevision ?? result.credentialRevision ?? "")],
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
  if (Array.from(value).length < 15 || new TextEncoder().encode(value).length > 4096) {
    throw new Error("新密码至少需要 15 个字符，最长 4096 字节；支持空格、中文和密码管理器。");
  }
  return value;
}

function positiveRevision(value: FormDataEntryValue | null): number {
  const revision = Number(value);
  if (!Number.isInteger(revision) || revision < 1) throw new Error("revision 必须是正整数");
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
  if (status === 409) return "数据 revision 已变化或目标状态冲突，请使用最新 revision 后重试。";
  if (status === 429) return "尝试次数过多，已临时限流。请等待后再试；已有会话不受影响。";
  if (status === 503) return "身份或权限服务暂不可用，请稍后重试。";
  if (cause instanceof Error && cause.message) return `${fallback} ${cause.message}`;
  return fallback;
}
