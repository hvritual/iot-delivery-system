import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { assert, browserRequest } from "./browser-actions.mjs";

// Only disposable fixture data enters this harness; credentials never enter artifacts.
export function uiActions(context, output, report) {
  const visible = `(el) => !!el && el.getClientRects().length > 0 && !el.closest('[hidden]') && getComputedStyle(el).visibility !== 'hidden'`;
  async function click(text, scope = "body") {
    const location = await context.evaluate(`(() => {
      const visible = ${visible};
      const root = document.querySelector(${JSON.stringify(scope)});
      const el = Array.from(root?.querySelectorAll('button,a') || []).find(el => visible(el) && (el.getAttribute('aria-label') === ${JSON.stringify(text)} || el.textContent.trim() === ${JSON.stringify(text)}));
      if (!el) throw new Error('Visible control not found: ' + ${JSON.stringify(text)});
      if (el.disabled) throw new Error('Control disabled: ' + ${JSON.stringify(text)});
      el.scrollIntoView({block:'nearest'});
      const r = el.getBoundingClientRect(); return { x:r.x+r.width/2, y:r.y+r.height/2 };
    })()`);
    await context.client.send(
      "Input.dispatchMouseEvent",
      { type: "mousePressed", button: "left", clickCount: 1, ...location },
      context.sessionId,
    );
    await context.client.send(
      "Input.dispatchMouseEvent",
      { type: "mouseReleased", button: "left", clickCount: 1, ...location },
      context.sessionId,
    );
    await pause(100);
  }
  async function field(label, value, scope = "body") {
    await context.evaluate(`(() => {
      const visible = ${visible};
      const root = document.querySelector(${JSON.stringify(scope)});
      const label = Array.from(root?.querySelectorAll('label') || []).find(el => visible(el) && el.textContent.trim().replace(/\\s*\\*$/, '') === ${JSON.stringify(label)});
      const control = label?.control || (label?.htmlFor ? document.getElementById(label.htmlFor) : label?.querySelector('input,textarea,select'));
      if (!control || !visible(control)) throw new Error('Visible field not found: ' + ${JSON.stringify(label)});
      const proto = control instanceof HTMLSelectElement ? HTMLSelectElement.prototype : control instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
      Object.getOwnPropertyDescriptor(proto,'value').set.call(control, ${JSON.stringify(value)});
      control.dispatchEvent(new Event('input',{bubbles:true}));
      control.dispatchEvent(new Event('change',{bubbles:true}));
      return true;
    })()`);
    await pause(60);
  }
  async function text(value) {
    return context.waitFor(
      `document.body.innerText.includes(${JSON.stringify(value)})`,
    );
  }
  async function gone(selector) {
    return context.waitFor(
      `!document.querySelector(${JSON.stringify(selector)})`,
    );
  }
  async function shot(name) {
    await context.evaluate("document.fonts.ready.then(() => true)");
    await pause(220);
    const checks = await context.evaluate(`(() => {
      const app = document.querySelector('.app');
      const sidebar = document.querySelector('.sidebar');
      const topbar = document.querySelector('.topbar');
      const scrollWidth = document.documentElement.scrollWidth;
      const width = innerWidth;
      return {
        title:document.title,
        meaningful:document.body.innerText.trim().length > 30,
        frameworkError:!!document.querySelector('nextjs-error-overlay, #nextjs__container_errors_label'),
        width, height:innerHeight, scrollWidth,
        sidebar:sidebar?.getBoundingClientRect().width ?? null,
        toolbar:topbar?.getBoundingClientRect().height ?? null,
        font:getComputedStyle(document.body).fontFamily,
        primary:getComputedStyle(document.documentElement).getPropertyValue('--primary').trim(),
        background:getComputedStyle(document.body).backgroundColor,
        button:document.querySelector('.btn.primary') ? getComputedStyle(document.querySelector('.btn.primary')).backgroundColor : null,
        app:!!app,
      };
    })()`);
    assert(
      checks.meaningful && !checks.frameworkError,
      `${name} renders meaningful app content`,
    );
    assert(
      checks.width === 1366 && checks.height === 768,
      `${name} native desktop viewport`,
    );
    assert(
      checks.scrollWidth <= checks.width + 1,
      `${name} document must not horizontally overflow: ${checks.scrollWidth}`,
    );
    if (checks.app) {
      assert(
        Math.abs(checks.sidebar - 236) < 1,
        `${name} sidebar width must remain 236px: ${checks.sidebar}`,
      );
      assert(
        Math.abs(checks.toolbar - 48) < 1,
        `${name} toolbar height must remain 48px: ${checks.toolbar}`,
      );
    }
    const image = await context.client.send(
      "Page.captureScreenshot",
      { format: "png", captureBeyondViewport: false },
      context.sessionId,
    );
    await mkdir(output, { recursive: true });
    await writeFile(
      path.join(output, `${name}.png`),
      Buffer.from(image.data, "base64"),
    );
    report.screens.push({ name, ...checks });
    console.log(`UI screenshot PASS: ${name}`);
  }
  async function request(url, options = {}, expected = 200) {
    const result = await browserRequest(context, url, options);
    // Do not include a full response body in a failure: auth responses can be sensitive.
    assert(
      result.status === expected,
      `fixture request ${options.method || "GET"} ${url.split("?")[0]} expected ${expected}, got ${result.status}`,
    );
    return result.body;
  }
  return { click, field, text, gone, shot, request };
}
export const pause = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
