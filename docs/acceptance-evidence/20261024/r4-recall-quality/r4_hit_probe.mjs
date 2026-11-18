// R4 recall hit-rate probe on the local real corpus (gitignored corpus/ dir).
// Usage: node output/r4_hit_probe.mjs <label> [gram] [samples] [ctx]
// Talks only to the running server; never touches D:/go/data.
import fs from "node:fs";

const LABEL = process.argv[2] || "before";
const GRAM = Number(process.argv[3] || 3);
const SAMPLES = Number(process.argv[4] || 40);
const CTX = Number(process.argv[5] || 12000);
const BASE = process.env.R4_BASE || "http://127.0.0.1:8096";
const CORPUS = "corpus/\u65e0\u8fb9\u6094\u6068.txt";

const isHan = (ch) => {
  const c = ch.codePointAt(0);
  return (c >= 0x4e00 && c <= 0x9fff) || (c >= 0x3400 && c <= 0x4dbf);
};

// Mirror recall.tokenizeRunes for Han runs: every 2-gram and 3-gram of each run.
function tokens(text) {
  const runes = Array.from(text);
  const out = new Set();
  for (let i = 0; i < runes.length;) {
    if (!isHan(runes[i])) { i++; continue; }
    let j = i + 1;
    while (j < runes.length && isHan(runes[j])) j++;
    const run = runes.slice(i, j);
    if (run.length === 1) out.add(run.join(""));
    else {
      for (let k = 0; k + 1 < run.length; k++) out.add(run.slice(k, k + 2).join(""));
      for (let k = 0; k + 2 < run.length; k++) out.add(run.slice(k, k + 3).join(""));
    }
    i = j;
  }
  return out;
}

async function call(path, method, body) {
  const res = await fetch(BASE + path, {
    method,
    headers: body ? { "content-type": "application/json" } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  if (!res.ok) throw new Error(`${method} ${path} -> ${res.status} ${text.slice(0, 200)}`);
  return text ? JSON.parse(text) : null;
}

const raw = fs.readFileSync(CORPUS, "utf8");
const imported = await call("/api/import", "POST", { title: "", text: raw });
const cfg = await call("/api/config");
await call("/api/config", "PUT", {
  baseUrl: cfg.baseUrl || "",
  model: cfg.model || "probe-only",
  contextChars: CTX,
});

const project = await call("/api/project");
const list = project.chapters || [];
const chapters = [];
for (const item of list) {
  const full = await call(`/api/chapters/${item.id}`);
  chapters.push({ id: item.id, index: full.index ?? item.index, title: full.title, content: full.content ?? "" });
}
chapters.sort((a, b) => a.index - b.index);

const perChapter = chapters.map((c) => tokens(c.content));
const df = new Map();
perChapter.forEach((set) => {
  for (const term of set) df.set(term, (df.get(term) || 0) + 1);
});

// The recall query is the tail of the recent window, so the newest chapters are
// already in context; only plant in chapters the search is allowed to reach.
const tailCount = Math.max(1, Math.min(chapters.length, Math.ceil((CTX / 2) / 3000)));
const maxPlantIndex = chapters.length - 1 - tailCount;

const candidates = [];
for (let ci = 0; ci < perChapter.length; ci++) {
  const chapter = chapters[ci];
  if (chapter.index > maxPlantIndex) continue;
  for (const term of perChapter[ci]) {
    if (Array.from(term).length !== GRAM) continue;
    if (df.get(term) !== 1) continue;
    candidates.push({ term, chapterIndex: chapter.index });
  }
}
candidates.sort((a, b) => (a.term < b.term ? -1 : a.term > b.term ? 1 : 0));

// Deterministic spread across chapters: take every k-th candidate.
const step = Math.max(1, Math.floor(candidates.length / SAMPLES));
const picked = [];
const seenChapters = new Set();
for (let i = 0; i < candidates.length && picked.length < SAMPLES; i += step) {
  const cand = candidates[i];
  if (seenChapters.has(cand.chapterIndex)) continue;
  seenChapters.add(cand.chapterIndex);
  picked.push(cand);
}

const runs = [];
for (const cand of picked) {
  const direction = `\u56f4\u7ed5\u300c${cand.term}\u300d\u7ee7\u7eed\u5199\uff0c\u4ea4\u4ee3\u5b83\u7684\u6765\u5386\u4e0e\u53bb\u5411\uff0c\u8ba9\u524d\u6587\u7684\u5b89\u6392\u5728\u8fd9\u4e00\u7ae0\u5f97\u5230\u56de\u6263\u3002`;
  const body = await call("/api/continue/preview", "POST", {
    direction,
    targetChars: 800,
    useMemory: false,
    usePlan: false,
    useRecall: true,
  });
  const section = body.recallSection || "";
  runs.push({
    term: cand.term,
    sourceChapter: cand.chapterIndex,
    recallEnabled: body.recallEnabled === true,
    usedChars: body.recallUsedChars || 0,
    hits: body.recallHits || 0,
    recallChapters: body.recallChapters || [],
    targetRecalled: (body.recallChapters || []).includes(cand.chapterIndex),
    termInjected: section.includes(cand.term),
    manuscriptChars: body.manuscriptChars || 0,
  });
}

const control = [];
for (let i = 0; i < 5; i++) {
  const body = await call("/api/continue/preview", "POST", {
    direction: "\u7ee7\u7eed\u5199\u4e0b\u53bb\uff0c\u4fdd\u6301\u73b0\u6709\u7684\u53d9\u4e8b\u8282\u594f\u3002",
    targetChars: 800,
    useMemory: false,
    usePlan: false,
    useRecall: true,
  });
  control.push(body.recallChapters || []);
}

const summary = {
  label: LABEL,
  gram: GRAM,
  contextChars: CTX,
  chapterCount: chapters.length,
  maxPlantIndex,
  candidateCount: candidates.length,
  samples: runs.length,
  targetRecalled: runs.filter((r) => r.targetRecalled).length,
  termInjected: runs.filter((r) => r.termInjected).length,
  meanUsedChars: Math.round(runs.reduce((a, r) => a + r.usedChars, 0) / (runs.length || 1)),
  meanManuscriptChars: Math.round(runs.reduce((a, r) => a + r.manuscriptChars, 0) / (runs.length || 1)),
  controlRankings: control,
  distinctControlRankings: new Set(control.map((c) => c.join(","))).size,
  runs,
};
fs.writeFileSync(`output/r4-hit-${LABEL}.json`, JSON.stringify(summary, null, 2));
console.log(JSON.stringify({ ...summary, runs: undefined }, null, 2));
