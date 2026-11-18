import { createRequire } from "node:module";
import fs from "node:fs";
const require = createRequire((process.env.APPDATA + "/npm/"));
const { chromium } = require("playwright");

const BASE = "http://127.0.0.1:8090";
const OUT = "D:/go/output";
const DIRECTION = "继续追查青玉符的来历，让它在章末再次出现。";

const consoleErrors = [];
const requests = [];
const results = {};

function record(name, value) {
  results[name] = value;
  console.log(name + " => " + JSON.stringify(value));
}

async function waitFor(predicate, label, timeout = 20000) {
  const started = Date.now();
  for (;;) {
    const value = await predicate();
    if (value) return value;
    if (Date.now() - started > timeout) throw new Error("timeout waiting for " + label);
    await new Promise((r) => setTimeout(r, 120));
  }
}

const browser = await chromium.launch({
  headless: true,
  executablePath: (process.env.LOCALAPPDATA + "/ms-playwright/chromium-1208/chrome-win64/chrome.exe"),
});
const page = await browser.newPage({ viewport: { width: 1360, height: 950 } });

page.on("console", (msg) => {
  if (msg.type() === "error") consoleErrors.push(msg.text());
});
page.on("pageerror", (err) => consoleErrors.push("pageerror: " + err.message));
page.on("request", (req) => {
  const url = req.url();
  if (url.endsWith("/api/continue") || url.endsWith("/api/continue/preview")) {
    requests.push({ api: url.replace(BASE, ""), body: req.postData() });
  }
});

await page.goto(BASE + "/", { waitUntil: "networkidle" });
await page.locator("#direction").fill(DIRECTION);

async function setCheckbox(id, checked) {
  const box = page.locator(id);
  if ((await box.isChecked()) !== checked) await box.click();
}

async function previewWith(recallValue, options = {}) {
  await page.locator("#useRecall").selectOption(recallValue);
  if (options.memory !== undefined) await setCheckbox("#useMemory", options.memory);
  if (options.plan !== undefined) await setCheckbox("#usePlan", options.plan);
  await page.locator("#memoryPreviewButton").click();
  await waitFor(() => page.evaluate(() => {
    const panel = document.getElementById("memoryPreview");
    const body = document.getElementById("memoryPreviewBody");
    return !panel.hidden && body && body.children.length > 0;
  }), "preview panel");
  const info = await page.evaluate(() => {
    const body = document.getElementById("memoryPreviewBody");
    const recall = [...body.querySelectorAll(".memory-recall-section")];
    const foot = body.querySelector(".memory-preview-foot");
    const node = recall.length ? recall[0] : null;
    const style = node ? getComputedStyle(node) : null;
    return {
      headings: [...body.querySelectorAll("h4")].map((h) => h.textContent.trim()),
      recallPresent: recall.length > 0,
      recallHeading: (() => {
        if (!node) return null;
        const group = node.closest(".memory-group");
        const h = group ? group.querySelector("h4") : null;
        return h ? h.textContent.trim() : null;
      })(),
      recallChars: node ? [...node.textContent].length : 0,
      recallBorder: style ? style.borderLeftWidth + " " + style.borderLeftStyle + " " + style.borderLeftColor : null,
      recallMaxHeight: style ? style.maxHeight + " / overflowY=" + style.overflowY : null,
      recallScrollable: node ? node.scrollHeight > node.clientHeight : null,
      foot: foot ? foot.innerText.replace(/\s+/g, " ").trim() : "",
      empty: body.innerText.includes("本次没有") || !!body.querySelector(".memory-empty"),
      bodyTextHead: body.innerText.slice(0, 260),
    };
  });
  if (options.screenshot) {
    await page.locator("#memoryPreview").scrollIntoViewIfNeeded();
    await page.screenshot({ path: OUT + "/" + options.screenshot });
  }
  return info;
}

try {
  const auto = await previewWith("auto", { screenshot: "r4-ui-1-preview-auto.png" });
  record("auto_recall_heading", auto.recallHeading);
  record("auto_recall_chars", auto.recallChars);
  record("auto_recall_border", auto.recallBorder);
  record("auto_recall_maxheight", auto.recallMaxHeight);
  record("auto_headings", auto.headings);
  record("auto_foot", auto.foot);
  fs.writeFileSync(OUT + "/r4-ui-1-preview-auto-body.txt", auto.bodyTextHead);

  const forcedOn = await previewWith("on", { screenshot: "r4-ui-2-preview-recall-on.png" });
  record("on_recall_chars", forcedOn.recallChars);
  record("on_foot", forcedOn.foot);

  const forcedOff = await previewWith("off", { screenshot: "r4-ui-3-preview-recall-off.png" });
  record("off_recall_present", forcedOff.recallPresent);
  record("off_headings", forcedOff.headings);
  record("off_foot", forcedOff.foot);

  const toastAfterOff = await page.evaluate(() => {
    const t = document.getElementById("toast");
    return t && !t.hidden ? t.textContent : null;
  });
  record("toast_after_off", toastAfterOff);

  const recallOnly = await previewWith("on", { memory: false, screenshot: "r4-ui-4-recall-only.png" });
  record("recall_only_headings", recallOnly.headings);
  record("recall_only_foot", recallOnly.foot);
  record("recall_only_scrollable", recallOnly.recallScrollable);

  const toastAfterOn = await page.evaluate(() => {
    const t = document.getElementById("toast");
    return t && !t.hidden ? t.textContent : null;
  });
  record("toast_after_on", toastAfterOn);

  async function continueWith(recallValue, screenshot) {
    await page.evaluate(() => {
      window.__statusLog = [];
      const el = document.getElementById("draftStatus");
      window.__statusObserver?.disconnect();
      window.__statusObserver = new MutationObserver(() => {
        const text = el.textContent.trim();
        if (text && window.__statusLog[window.__statusLog.length - 1] !== text) window.__statusLog.push(text);
      });
      window.__statusObserver.observe(el, { childList: true, subtree: true, characterData: true });
    });
    await page.locator("#useRecall").selectOption(recallValue);
    await page.locator("#generateButton").click({ timeout: 90000 });
    const note = await waitFor(() => page.evaluate(() => {
      const live = document.getElementById("draftStatus").textContent.trim();
      if (live.includes("已连接")) return live;
      const logged = (window.__statusLog || []).find((t) => t.includes("已连接"));
      return logged || null;
    }), "meta note rendered for " + recallValue, 40000);
    await page.locator("#draftPanel").scrollIntoViewIfNeeded();
    await page.screenshot({ path: OUT + "/" + screenshot });
    const log = await page.evaluate(() => window.__statusLog);
    return { note, log };
  }

  const off = await continueWith("off", "r4-ui-5-continue-recall-off.png");
  record("continue_note_off", off.note);
  record("continue_status_log_off", off.log);

  const on = await continueWith("on", "r4-ui-6-continue-recall-on.png");
  record("continue_note_on", on.note);
  record("continue_status_log_on", on.log);
  record("continue_note_on_has_recall", on.note.includes("前文召回 3 段"));
  record("continue_note_off_has_recall", off.note.includes("前文召回"));

  record("api_requests", requests);
} catch (err) {
  record("walk_error", String(err && err.message ? err.message : err));
  await page.screenshot({ path: OUT + "/r4-ui-error.png" }).catch(() => {});
}

record("console_errors", consoleErrors);
fs.writeFileSync(OUT + "/r4-ui-walk-result.json", JSON.stringify({ results, requests, consoleErrors }, null, 2));
await browser.close();
process.exit(results.walk_error ? 1 : 0);
