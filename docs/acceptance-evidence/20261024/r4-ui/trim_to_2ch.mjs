const base = "http://127.0.0.1:8095";
const project = await (await fetch(base + "/api/project")).json();
const doomed = (project.chapters || []).filter((c) => c.index > 2);
for (const c of doomed) {
  const res = await fetch(base + "/api/chapters/" + c.id, { method: "DELETE" });
  if (!res.ok) console.log("delete failed", c.index, res.status);
}
const after = await (await fetch(base + "/api/project")).json();
console.log("chapters=" + (after.chapters || []).length);
const dir = "继续追查青玉符的来历，让它在章末再次出现。";
for (const useRecall of [true, false, null]) {
  const res = await fetch(base + "/api/continue/preview", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ direction: dir, targetChars: 600, useMemory: false, usePlan: false, useRecall }),
  });
  const d = await res.json();
  console.log(JSON.stringify({ useRecall, recallEnabled: d.recallEnabled, hits: d.recallHits, used: d.recallUsedChars, budget: d.recallBudgetChars, memoryEnabled: d.memoryEnabled, planEnabled: d.planEnabled }));
}
