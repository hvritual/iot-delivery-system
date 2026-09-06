import { randomBytes } from "node:crypto";
import { assert, loginThroughUI } from "./browser-actions.mjs";

export async function verifyUIAccountFlows({
  admin,
  reviewer,
  ui,
  ru,
  fixture,
  binding,
  project,
  webBase,
  ok,
}) {
  await ui.click("账号与管理", ".sidebar");
  await ui.shot("39-account-profile");
  for (const [tab, name] of [
    ["修改密码", "40-account-password"],
    ["创建成员", "41-member-create"],
    ["停用成员", "42-member-disable"],
    ["重置凭据", "43-member-reset"],
    ["项目角色", "44-project-role-assign"],
    ["撤销角色", "45-project-role-revoke"],
    ["操作回执", "46-admin-receipts"],
    ["交付规则", "47-settings-delivery-rules"],
    ["Obsidian 投影", "48-settings-obsidian"],
    ["通知与提醒", "49-settings-notifications"],
    ["运行时与 MCP", "50-settings-runtime-mcp"],
  ]) {
    await ui.click(tab, ".subnav");
    await ui.shot(name);
    if (
      name.startsWith("47") ||
      name.startsWith("48") ||
      name.startsWith("49") ||
      name.startsWith("50")
    )
      assert(
        await admin.evaluate(
          `document.querySelector('.settings-main').querySelectorAll('button,input,select,textarea').length===0`,
        ),
        "policy pages do not invent mutation controls",
      );
  }
  const password = `ui-acceptance-${randomBytes(18).toString("hex")}`;
  await ui.click("创建成员", ".subnav");
  await ui.field("显示名称", "UI 验收成员");
  await ui.field("初始密码", password);
  await ui.click("创建", 'form[aria-label="创建成员"]');
  await ui.text("成员已创建");
  await ui.click("操作回执", ".subnav");
  await ui.shot("46-admin-receipts");
  const receipt = await admin.evaluate(
    `(() => {const root=Array.from(document.querySelectorAll('.settings-main [role="status"]')).find(el=>el.textContent.includes('User revision'));if(!root)throw new Error('member receipt missing');return Object.fromEntries(Array.from(root.querySelectorAll('dt')).map(dt=>[dt.textContent.trim(),dt.nextElementSibling?.textContent.trim()]));})()`,
  );
  const newUser = receipt.UserID;
  assert(newUser, "server returned new member ID");
  await ui.click("重置凭据", ".subnav");
  await ui.field("UserID", newUser);
  await ui.field("Expected user revision", receipt["User revision"]);
  await ui.field(
    "Expected credential revision",
    receipt["Credential revision"],
  );
  await ui.field("新密码", `${password}-rotated`);
  await ui.click("重置", 'form[aria-label="重置成员凭据"]');
  await ui.text("成员凭据已重置");
  const after = await admin.evaluate(
    `(() => {const root=Array.from(document.querySelectorAll('.settings-main [role="status"]')).find(el=>el.textContent.includes('User revision'));return Object.fromEntries(Array.from(root.querySelectorAll('dt')).map(dt=>[dt.textContent.trim(),dt.nextElementSibling?.textContent.trim()]));})()`,
  );
  await ui.click("停用成员", ".subnav");
  await ui.field("UserID", newUser);
  await ui.field("Expected user revision", after["User revision"]);
  await ui.click("停用", 'form[aria-label="停用成员"]');
  await ui.text("成员已停用");
  ok(
    "real create member, reset credential and confirmed disable use returned revisions",
  );
  // Existing disposable member receives/revokes a role through actual admin UI.
  await ui.click("撤销角色", ".subnav");
  await ui.field("BindingID", binding.BindingID ?? binding.bindingId);
  await ui.field(
    "Expected binding revision",
    String(binding.Revision ?? binding.revision),
  );
  await ui.click("撤销", 'form[aria-label="撤销项目角色"]');
  await ui.text("项目角色已撤销");
  await ui.click("项目角色", ".subnav");
  await ui.field("ProjectID", project.id);
  await ui.field("UserID", fixture.memberUserId);
  await ui.field("RoleID", "release-approver");
  await ui.click("分配", 'form[aria-label="分配项目角色"]');
  await ui.text("项目角色已分配");
  ok(
    "confirmed role revoke and reassign are enforced by the real durable authorization service",
  );
  await ru.click("账号与管理", ".sidebar");
  await ru.click("创建成员", ".subnav");
  await ru.field("显示名称", "不可创建的成员");
  await ru.field("初始密码", password);
  await ru.click("创建", 'form[aria-label="创建成员"]');
  await ru.text("权限不足");
  await ru.shot("54-state-forbidden-account");
  await ru.click("修改密码", ".subnav");
  await ru.field("当前密码", fixture.memberPassword);
  await ru.field("新密码", password);
  await ru.click("更新密码", 'form[aria-label="修改本人密码"]');
  await ru.text("旧会话已失效");
  await ru.shot("53-state-session-expired");
  await loginThroughUI(
    reviewer,
    webBase,
    fixture.organizationId,
    fixture.memberUserId,
    password,
    "YU-29 Ordinary Member",
  );
  ok(
    "non-admin action denied and password update invalidates old session, new password logs in",
  );
}
