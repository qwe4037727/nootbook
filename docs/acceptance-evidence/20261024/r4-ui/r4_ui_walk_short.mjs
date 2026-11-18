import { createRequire } from "node:module";
import fs from "node:fs";
const require = createRequire((process.env.APPDATA + "/npm/"));
const { chromium } = require("playwright");

const BASE = "http://127.0.0.1:8093";
const OUT = "D:/go/output";
const results = {};
const consoleErrors = [];
const toasts = [];

function record(name, value) {
  results[name] = value;
  console.log(name + " => " + JSON.stringify(value));
}

async function waitFor(fn, label, timeout = 30000) {
  const started = Date.now();
  for (;;) {
    const v = await fn();
    if (v) return v;
    if (Date.now() - started > timeout) throw new Error("timeout: " + label);
    await new Promise((r) => setTimeout(r, 120));
  }
}

const browser = await chromium.launch({
  headless: true,
  executablePath: (process.env.LOCALAPPDATA + "/ms-playwright/chromium-1208/chrome-win64/chrome.exe"),
});
const page = await browser.newPage({ viewport: { width: 1360, height: 950 } });
page.on("console", (m) => m.type() === "error" && consoleErrors.push(m.text()));
page.on("pageerror", (e) => consoleErrors.push("pageerror: " + e.message));

await page.goto(BASE + "/", { waitUntil: "networkidle" });
await page.evaluate(() => {
  const t = document.getElementById("toast");
  window.__toasts = [];
  new MutationObserver(() => {
    if (!t.hidden && t.textContent) {
      const last = window.__toasts[window.__toasts.length - 1];
      if (last !== t.textContent) window.__toasts.push(t.textContent);
    }
  }).observe(t, { childList: true, subtree: true, characterData: true, attributes: true });
});
await page.locator("#direction").fill("继续追查青玉符的来历，让它在章末再次出现。");

async function readPanel() {
  await waitFor(() => page.evaluate(() => {
    const body = document.getElementById("memoryPreviewBody");
    return !document.getElementById("memoryPreview").hidden && body.children.length > 0;
  }), "preview panel");
  return page.evaluate(() => {
    const body = document.getElementById("memoryPreviewBody");
    const foot = body.querySelector(".memory-preview-foot");
    return {
      headings: [...body.querySelectorAll("h4")].map((h) => h.textContent.trim()),
      recall: !!body.querySelector(".memory-recall-section"),
      empty: body.querySelector(".memory-empty") ? body.querySelector(".memory-empty").textContent : null,
      foot: foot ? foot.innerText.replace(/\s+/g, " ").trim() : "",
      toasts: window.__toasts.slice(),
    };
  });
}

async function setSwitches({ recall, memory = true, plan = true }) {
  await page.locator("#useRecall").selectOption(recall);
  const mem = page.locator("#useMemory");
  if ((await mem.isChecked()) !== memory) await mem.click();
  const pl = page.locator("#usePlan");
  if ((await pl.isChecked()) !== plan) await pl.click();
}

try {
  await setSwitches({ recall: "on" });
  await page.waitForTimeout(400);
  const toastOnSelect = await page.evaluate(() => window.__toasts.slice());
  record("toast_when_forced_on", toastOnSelect);

  await page.locator("#memoryPreviewButton").click();
  const forced = await readPanel();
  record("forced_on_headings", forced.headings);
  record("forced_on_recall_group", forced.recall);
  record("forced_on_foot", forced.foot);
  record("forced_on_toasts", forced.toasts);
  await page.locator("#memoryPreview").scrollIntoViewIfNeeded();
  await page.screenshot({ path: OUT + "/r4-ui-7-short-forced-on.png" });

  await setSwitches({ recall: "auto" });
  await page.locator("#memoryPreviewButton").click();
  const auto = await readPanel();
  record("auto_recall_group", auto.recall);
  record("auto_foot", auto.foot);

  await setSwitches({ recall: "on", memory: false, plan: false });
  await page.locator("#memoryPreviewButton").click();
  const empty = await readPanel();
  record("empty_all_off_text", empty.empty);
  record("empty_all_off_recall_group", empty.recall);
  await page.screenshot({ path: OUT + "/r4-ui-8-short-empty.png" });

  await page.evaluate(() => { window.__toasts = []; });
  await page.locator("#generateButton").click({ timeout: 90000 });
  const note = await waitFor(() => page.evaluate(() => {
    const s = document.getElementById("draftStatus").textContent.trim();
    return s.includes("已连接") ? s : null;
  }), "meta note on short manuscript");
  await page.waitForTimeout(600);
  const warn = await page.evaluate(() => ({ toasts: window.__toasts.slice(), status: document.getElementById("draftStatus").textContent }));
  await page.screenshot({ path: OUT + "/r4-ui-9-short-continue-warning.png" });
  record("short_continue_note", note);
  record("short_continue_toasts", warn.toasts);
  record("short_continue_status_after", warn.status);
} catch (err) {
  record("walk_error", String(err && err.message ? err.message : err));
  await page.screenshot({ path: OUT + "/r4-ui-short-error.png" }).catch(() => {});
}

record("console_errors", consoleErrors);
fs.writeFileSync(OUT + "/r4-ui-walk-short-result.json", JSON.stringify({ results, consoleErrors }, null, 2));
await browser.close();
process.exit(results.walk_error ? 1 : 0);
