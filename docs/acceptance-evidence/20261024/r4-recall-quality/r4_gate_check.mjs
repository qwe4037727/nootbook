// Check the rare-clue gate: a vague direction with no traceable clue must inject
// nothing and return the whole recall budget to the manuscript window.
import fs from "node:fs";

const BASE = process.env.R4_BASE || "http://127.0.0.1:8096";
const hit = JSON.parse(fs.readFileSync("output/r4-hit-after-3g.json", "utf8"));
const good = hit.runs.find((r) => r.termInjected) || hit.runs[0];
const clueTerm = good.term;

async function preview(body) {
  const res = await fetch(BASE + "/api/continue/preview", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`${res.status} ${await res.text()}`);
  return res.json();
}

const common = { targetChars: 800, useMemory: false, usePlan: false, useRecall: true };
const vague = await preview({ ...common, direction: "继续写下去，保持现有的叙事节奏，让情节自然推进。" });
const clue = await preview({ ...common, direction: `围绕前文埋下的「${clueTerm}」这条线索继续写，交代它的来历。` });

const out = {
  vague: {
    recallEnabled: vague.recallEnabled,
    recallUsedChars: vague.recallUsedChars,
    recallHits: vague.recallHits,
    manuscriptChars: vague.manuscriptChars,
    recallChapters: vague.recallChapters,
  },
  clue: {
    recallEnabled: clue.recallEnabled,
    recallUsedChars: clue.recallUsedChars,
    recallHits: clue.recallHits,
    manuscriptChars: clue.manuscriptChars,
    containsClue: (clue.recallSection || "").includes(clueTerm),
  },
};
console.log(JSON.stringify(out, null, 2));
