const base = "http://127.0.0.1:8090";
const direction = "继续追查青玉符的来历，让它在章末再次出现。";

async function preview(body) {
  const res = await fetch(base + "/api/continue/preview", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const data = await res.json();
  return { status: res.status, data };
}

const cases = [
  ["recall-auto", { direction, targetChars: 600 }],
  ["recall-on-memory-off", { direction, targetChars: 600, useMemory: false, useRecall: true }],
  ["recall-off-memory-off", { direction, targetChars: 600, useMemory: false, useRecall: false }],
  ["recall-on-memory-on", { direction, targetChars: 600, useMemory: true, useRecall: true }],
];

const picks = [
  "recallEnabled", "recallBudgetChars", "recallUsedChars", "recallHits", "recallChapters",
  "memoryEnabled", "used", "budget", "planEnabled", "planCount",
  "promptTotalChars", "promptOverBudget", "memoryDisabledReason",
];

for (const [name, body] of cases) {
  const { status, data } = await preview(body);
  const summary = Object.fromEntries(picks.map((k) => [k, data[k]]));
  console.log(name, status, JSON.stringify(summary));
  const section = String(data.recallSection || "");
  console.log("  recallSection.len=" + [...section].length);
  console.log("  head=" + JSON.stringify(section.slice(0, 90)));
}

const cfg = await (await fetch(base + "/api/config")).json();
console.log("config", JSON.stringify({ baseUrl: cfg.baseUrl, model: cfg.model, contextChars: cfg.contextChars, hasApiKey: cfg.hasApiKey ?? cfg.apiKeySet }));
