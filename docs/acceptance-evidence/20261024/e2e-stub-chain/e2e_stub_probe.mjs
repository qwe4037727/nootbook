// Follow-up read-only probe on the same scratch workspace: why records went stale, what the
// thread channel did, and whether "all memory skipped" is distinguishable in the API.
import fs from 'node:fs';

const BASE = process.env.BASE || 'http://127.0.0.1:8097';
const OUT = process.env.OUT || 'docs/acceptance-evidence/20261024/e2e-stub-chain/heal-detail.json';

async function req(path) {
  const res = await fetch(BASE + path);
  const text = await res.text();
  let json = null;
  try { json = JSON.parse(text); } catch {}
  return { path, status: res.status, json, text: json ? undefined : text.slice(0, 300) };
}

const CHAR = process.argv[2] || '';
const out = { at: new Date().toISOString(), charId: CHAR };

if (CHAR) out.characterMemory = await req('/api/memory/characters/' + CHAR);
out.memory = await req('/api/memory');
out.plans = await req('/api/plans');
out.changesets = await req('/api/memory/changesets');
out.candidates = await req('/api/memory/candidates');
out.preview = await req('/api/continue/preview', );

// preview needs POST
const p = await fetch(BASE + '/api/continue/preview', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ direction: '林砚顺着青玉符的刻痕继续追查旧案。', targetChars: 600, useMemory: true, usePlan: true, useRecall: true }),
});
out.preview = { status: p.status, json: await p.json() };

const raw = (f) => JSON.parse(fs.readFileSync('output/e2e-stub-data/memory/' + f, 'utf8'));
for (const f of ['events.json', 'states.json', 'cognitions.json', 'threads.json', 'plans.json', 'meta.json']) {
  try {
    const d = raw(f);
    const items = Array.isArray(d) ? d : (d.items ?? d.events ?? d.states ?? d.cognitions ?? d.threads ?? d.plans ?? []);
    out[f] = items.map((r) => ({ id: r.id, needsReview: r.needsReview, reviewReason: r.reviewReason, recordVersion: r.recordVersion, status: r.status, refs: (r.sourceRefs ?? []).map((s) => ({ ch: s.chapterId, rev: s.chapterRevision, quote: s.quote })) }));
  } catch (e) { out[f] = 'unavailable: ' + e.message; }
}
try {
  const commits = raw('commits.json');
  const list = Array.isArray(commits) ? commits : (commits.commits ?? commits.items ?? []);
  out.commits = list.map((c) => ({ id: c.id, type: c.type, basedOn: c.basedOn, target: c.target, files: Object.keys(c.files ?? {}) }));
} catch (e) { out.commits = 'unavailable: ' + e.message; }

fs.writeFileSync(OUT, JSON.stringify(out, null, 2));
console.log(JSON.stringify({
  charStatus: out.characterMemory?.status,
  reasons: (out.characterMemory?.json?.events || []).concat(out.characterMemory?.json?.states || [], out.characterMemory?.json?.cognitions || [])
    .filter((r) => r.needsReview).map((r) => ({ id: r.id, reason: r.reviewReason })),
  threadsFile: out['threads.json'],
  memoryDisabledReason: out.preview.json?.memoryDisabledReason,
  memoryEnabled: out.preview.json?.memoryEnabled,
  skippedReview: out.preview.json?.skippedReview,
  planCount: out.preview.json?.planCount,
  recallEnabled: out.preview.json?.recallEnabled,
  counts: out.preview.json?.counts,
  commits: out.commits,
}, null, 2));
