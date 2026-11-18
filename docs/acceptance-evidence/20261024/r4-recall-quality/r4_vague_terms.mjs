// Diagnostic: for a vague direction, which of its n-grams are rare enough to pass
// the intent-clue gate, and which chapters contain them? Numbers only, no corpus text.
const BASE = process.env.R4_BASE || "http://127.0.0.1:8096";
const VAGUE = "\u7ee7\u7eed\u5199\u4e0b\u53bb\uff0c\u4fdd\u6301\u73b0\u6709\u7684\u53d9\u4e8b\u8282\u594f\uff0c\u8ba9\u60c5\u8282\u81ea\u7136\u63a8\u8fdb\u3002";

const isHan = (ch) => {
  const c = ch.codePointAt(0);
  return (c >= 0x4e00 && c <= 0x9fff) || (c >= 0x3400 && c <= 0x4dbf);
};

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

async function call(path) {
  const res = await fetch(BASE + path);
  if (!res.ok) throw new Error(`${path} -> ${res.status}`);
  return res.json();
}

const project = await call("/api/project");
const chapters = [];
for (const item of project.chapters || []) {
  const full = await call(`/api/chapters/${item.id}`);
  chapters.push({ index: full.index ?? item.index, content: full.content ?? "" });
}
chapters.sort((a, b) => a.index - b.index);

const perChapter = chapters.map((c) => tokens(c.content));
const df = new Map();
perChapter.forEach((set) => { for (const t of set) df.set(t, (df.get(t) || 0) + 1); });

const rareLimit = Math.max(1, Math.ceil(0.05 * perChapter.length));
const dirTerms = Array.from(tokens(VAGUE));
const rare = dirTerms.filter((t) => (df.get(t) || 0) <= rareLimit);
const detail = rare.map((t) => {
  const where = [];
  perChapter.forEach((set, i) => { if (set.has(t)) where.push(i + 1); });
  return { term: t, runes: Array.from(t).length, df: df.get(t) || 0, chapters: where };
});

console.log(JSON.stringify({
  chapterCount: chapters.length,
  rareLimit,
  directionTermCount: dirTerms.length,
  rareDirectionTerms: rare.length,
  absentTerms: rare.filter((d) => d.df === 0).length,
  matched: detail.filter((d) => d.df > 0),
}, null, 2));
