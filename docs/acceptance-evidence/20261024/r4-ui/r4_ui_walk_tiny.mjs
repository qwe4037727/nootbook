import { createRequire } from "node:module";
import fs from "node:fs";
const require = createRequire((process.env.APPDATA + "/npm/"));
const { chromium } = require("playwright");

const BASE = "http://127.0.0.1:8095";
const OUT = "D:/go/output";
const results = {};
const consoleErrors = [];

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

async function setSwitches({ recall, memory, plan }) {
  await page.locator("#useRecall").selectOption(recall);
  if (memory !== undefined) {
    const el = page.locator("#useMemory");
    if ((await el.isChecked()) !== memory) await el.click();
  }
  if (plan !== undefined) {
    const el = page.locator("#usePlan");
    if ((await el.isChecked()) !== plan) await el.click();
  }
}

async function readPanel() {
  await waitFor(() => page.evaluate(() => {
    const body = document.getElementById("memoryPreviewBody");
    return !document.getElementById("memoryPreview").hidden && body.children.length > 0;
  }), "preview panel");
  return page.evaluate(() => {
    const body = document.getElementById("memoryPreviewBody");
    const foot = body.querySelector(".memory-preview-foot");
    const empty = body.querySelector(".memory-empty");
    return {
      headings: [...body.querySelectorAll("h4")].map((h) => h.textContent.trim()),
      recall: !!body.querySelector(".memory-recall-section"),
      emptyText: empty ? empty.textContent : null,
      foot: foot ? foot.innerText.replace(/\s+/g, " ").trim() : "",
    };
  });
}

try {
  await setSwitches({ recall: "on", memory: true, plan: true });
  await page.locator("#memoryPreviewButton").click();
  const withMemory = await readPanel();
  record("tiny_forced_on_with_memory_headings", withMemory.headings);
  record("tiny_forced_on_with_memory_recall_group", withMemory.recall);
  record("tiny_forced_on_with_memory_foot", withMemory.foot);
  await page.locator("#memoryPreview").scrollIntoViewIfNeeded();
  await page.screenshot({ path: OUT + "/r4-ui-10-tiny-foot-warning.png" });

  await setSwitches({ recall: "on", memory: false, plan: false });
  await page.locator("#memoryPreviewButton").click();
  const allOff = await readPanel();
  record("tiny_all_off_empty_text", allOff.emptyText);
  await page.screenshot({ path: OUT + "/r4-ui-11-tiny-empty-warning.png" });

  await page.evaluate(() => { window.__toasts = []; });
  await setSwitches({ recall: "on", memory: true, plan: true });
  await page.locator("#generateButton").click({ timeout: 90000 });
  const note = await waitFor(() => page.evaluate(() => {
    const s = document.getElementById("draftStatus").textContent.trim();
    return s.includes("已连接") ? s : null;
  }), "meta note on 2-chapter manuscript");
  await page.waitForTimeout(700);
  const warn = await page.evaluate(() => window.__toasts.slice());
  await page.screenshot({ path: OUT + "/r4-ui-12-tiny-continue-warning.png" });
  record("tiny_continue_note", note);
  record("tiny_continue_toasts", warn);
} catch (err) {
  record("walk_error", String(err && err.message ? err.message : err));
  await page.screenshot({ path: OUT + "/r4-ui-tiny-error.png" }).catch(() => {});
}

record("console_errors", consoleErrors);
fs.writeFileSync(OUT + "/r4-ui-walk-tiny-result.json", JSON.stringify({ results, consoleErrors }, null, 2));
await browser.close();
process.exit(results.walk_error ? 1 : 0);
