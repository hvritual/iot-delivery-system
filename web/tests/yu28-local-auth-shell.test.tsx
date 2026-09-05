// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";

import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/components/delivery-workspace", () => ({
  DeliveryWorkspace: () => <div data-testid="delivery-workspace">workspace</div>,
}));

import { LocalAuthShell } from "@/components/local-auth-shell";

const csrfToken = "csrf_current_session_token_1234567890ABCDEFG";
const localMember = {
  authenticated: true,
  organizationId: "org-a",
  userId: "user-a",
  displayName: "User A",
  email: "user-a@example.test",
  userRevision: 3,
  sessionRevision: 4,
};

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("YU-28 local auth shell", () => {
  it("shows explicit local and OIDC login choices and establishes local session without OIDC configuration", async () => {
    let loggedIn = false;
    const calls: string[] = [];
    vi.stubGlobal("fetch", vi.fn(async (input: string | URL | Request) => {
      const path = requestPath(input);
      calls.push(path);
      if (path === "/auth/session") {
        return loggedIn
          ? Response.json({ authenticated: true, csrfToken })
          : Response.json({ error: "unauthenticated" }, { status: 401 });
      }
      if (path === "/auth/local/login") {
        loggedIn = true;
        return Response.json({ authenticated: true, organizationId: "org-a", userId: "user-a" });
      }
      if (path === "/auth/local/current") return Response.json(localMember);
      throw new Error(`unexpected request ${path}`);
    }));

    const user = userEvent.setup();
    render(<LocalAuthShell />);

    expect(await screen.findByRole("form", { name: "本地成员登录" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "使用 OIDC 登录" })).toHaveAttribute("href", "/auth/login");

    await user.type(screen.getByLabelText("组织 ID"), "org-a");
    await user.type(screen.getByLabelText("成员 UserID"), "user-a");
    await user.type(screen.getByLabelText("密码"), "password-a");
    await user.click(screen.getByRole("button", { name: "使用本地成员账号登录" }));

    expect(await screen.findByText("User A")).toBeInTheDocument();
    expect(screen.getByTestId("delivery-workspace")).toBeInTheDocument();
    expect(calls.filter((path) => path === "/auth/local/login")).toHaveLength(1);
    expect(calls.slice(0, 2)).toEqual(["/auth/session", "/auth/local/login"]);
  });

  it("shows verified current-member facts but does not claim administrator authority before the server decides", async () => {
    vi.stubGlobal("fetch", vi.fn(async (input: string | URL | Request) => {
      const path = requestPath(input);
      if (path === "/auth/session") return Response.json({ authenticated: true, csrfToken });
      if (path === "/auth/local/current") return Response.json(localMember);
      if (path === "/auth/local/admin/members") return Response.json({ error: "forbidden" }, { status: 403 });
      throw new Error(`unexpected request ${path}`);
    }));

    const user = userEvent.setup();
    render(<LocalAuthShell />);

    await user.click(await screen.findByRole("button", { name: "账号与管理" }));
    expect(await screen.findByText("当前 verified member")).toBeInTheDocument();
    expect(screen.getByText("org-a")).toBeInTheDocument();
    expect(screen.getByText("user-a")).toBeInTheDocument();
    expect(screen.getByText(/UI 不授予管理员权限/)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "成员管理" }));
    await user.type(screen.getByLabelText("显示名称"), "Member B");
    await user.type(screen.getByLabelText("邮箱（可选）"), "member-b@example.test");
    await user.type(screen.getByLabelText("初始密码"), "temporary-password");
    await user.click(screen.getByRole("button", { name: "创建" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("权限不足");
    expect(screen.getByText(/提交后由服务端 durable authorization 决定/)).toBeInTheDocument();
  });

  it("returns to login after a successful self password change because the old session is invalid", async () => {
    let changed = false;
    vi.stubGlobal("fetch", vi.fn(async (input: string | URL | Request) => {
      const path = requestPath(input);
      if (path === "/auth/session") return changed
        ? Response.json({ error: "unauthenticated" }, { status: 401 })
        : Response.json({ authenticated: true, csrfToken });
      if (path === "/auth/local/current") return Response.json(localMember);
      if (path === "/auth/local/change-password") {
        changed = true;
        return Response.json({ changed: true, userRevision: 4, credentialRevision: 2 });
      }
      throw new Error(`unexpected request ${path}`);
    }));

    const user = userEvent.setup();
    render(<LocalAuthShell />);

    await user.click(await screen.findByRole("button", { name: "账号与管理" }));
    await user.click(screen.getByRole("button", { name: "修改密码" }));
    await user.type(screen.getByLabelText("当前密码"), "old-password");
    await user.type(screen.getByLabelText("新密码"), "new-password");
    await user.click(screen.getByRole("button", { name: "更新密码" }));

    expect(await screen.findByRole("form", { name: "本地成员登录" })).toBeInTheDocument();
    expect(screen.getByText(/旧会话已失效/)).toBeInTheDocument();
    await waitFor(() => expect(changed).toBe(true));
  });
});

function requestPath(input: string | URL | Request): string {
  if (typeof input === "string") return new URL(input, "https://app.example").pathname;
  if (input instanceof URL) return input.pathname;
  return new URL(input.url).pathname;
}
