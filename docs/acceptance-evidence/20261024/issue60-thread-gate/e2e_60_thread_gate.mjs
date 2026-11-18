// issues #60 differential: why does an agent-extracted thread never reach the continuation prompt?
//
// Only two product channels matter here: the relevance gate in internal/store/memory_retrieve.go
// (status -> relevance -> needsReview, in that order) and the record assembled by
// internal/analysis/extract.go for add_thread. Each variant is a fresh process on its own copy of a
// quiescent dir, so a patched threads.json cannot be masked by an in-memory cache, and any startup
// churn is detected instead of assumed.
import fs from 'node:fs';
import path from 'node:path';
import { spawn, spawnSync } from 'node:child_process';

const BIN = process.env.BIN || 'output/novelagent-g6.exe';
const PORT = Number(process.env.PORT || 8097);
const SRC = process.env.SRC || 'output/e2e-g6-rec-base';
const ROOT = 'output/e2e-60-thread-gate';
const API = `http://127.0.0.1:${PORT}`;
const DIRECTION = '林砚顺着封条残印继续追查旧案。';

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const readJson = (p) => { try { return JSON.parse(fs.readFileSync(p, 'utf8')); } catch { return null; } };
const writeJson = (p, v) => fs.writeFileSync(p, JSON.stringify(v, null, 2), 'utf8');
const cp = (from, to) => { if (fs.existsSync(to)) fs.rmSync(to, { recursive: true, force: true }); fs.cpSync(from, to, { recursive: true }); };

async function req(method, urlPath, body) {
  const res = await fetch(API + urlPath, { method, headers: { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) });
  const text = await res.text();
  let json = null; try { json = JSON.parse(text); } catch {}
  return { status: res.status, json, text };
}

let child = null, logLines = [];
function start(dataDir) {
  logLines = [];
  child = spawn(BIN, ['-addr', `127.0.0.1:${PORT}`, '-data', dataDir], { windowsHide: true });
  child.stdout?.on('data', (d) => logLines.push(String(d).trimEnd()));
  child.stderr?.on('data', (d) => logLines.push(String(d).trimEnd()));
}
async function up() { for (let i = 0; i < 160; i++) { try { const r = await fetch(`${API}/api/memory`); if (r.ok) { await r.text(); return; } } catch {} await sleep(250); } throw new Error(`up timeout: ${logLines.join('\n')}`); }
function stop(tag) {
  if (!child) return;
  spawnSync('taskkill', ['/PID', String(child.pid), '/T', '/F'], { windowsHide: true });
  child = null;
  fs.mkdirSync(`${ROOT}-logs`, { recursive: true });
  fs.writeFileSync(path.join(`${ROOT}-logs`, `${tag}.log`), logLines.join('\n') + '\n', 'utf8');
}

// a pure-CJK run of >=6 chars taken from the newest chapter: compact(title) is then guaranteed to be
// a substring of compact(recentText), which is exactly what mentionsText tests.
function verbatimPhrase(dir) {
  const files = fs.readdirSync(path.join(dir, 'chapters')).map((f) => readJson(path.join(dir, 'chapters', f))).filter(Boolean);
  files.sort((a, b) => (a.index ?? 0) - (b.index ?? 0));
  const content = String(files.at(-1)?.content ?? '');
  const runs = content.match(/[\u4e00-\u9fff]{6,}/g) ?? [];
  return runs.sort((a, b) => b.length - a.length)[0]?.slice(0, 8) ?? '';
}

async function measure(tag, dir) {
  start(dir);
  await up();
  await sleep(1200); // let any startup reconcile land so the reading is post-churn, not mid-churn
  const mem = await req('GET', '/api/memory');
  const pv = await req('POST', '/api/continue/preview', { direction: DIRECTION, targetChars: 600 });
  const rev2 = await req('GET', '/api/memory');
  const j = pv.json ?? {};
  const threadsIn = (j.records ?? []).filter((r) => r.kind === 'thread').map((r) => r.text);
  const out = {
    variant: tag,
    memoryRevision: mem.json?.memoryRevision,
    churnedDuringRun: mem.json?.memoryRevision !== rev2.json?.memoryRevision,
    safeState: mem.json?.safeState,
    counts_threads: j.counts?.threads ?? mem.json?.counts?.threads,
    recalled_threads: j.counts?.threads != null ? (j.records ?? []).filter((r) => r.kind === 'thread').length : null,
    recalledByKind_threads: mem.json?.recalledByKind?.threads,
    skippedReview: j.skippedReview,
    skippedAmbiguous: j.skippedAmbiguous,
    memoryUsedChars: j.used,
    memorySectionChars: (j.memorySection ?? '').length,
    memoryDisabledReason: j.memoryDisabledReason ?? null,
    charSeeds: (j.seeds ?? []).map((s) => ({ id: s.entityId, name: s.entityName, score: s.score, reason: s.reason })),
    threadRecordTexts: threadsIn,
    threadLinesInSection: (j.memorySection ?? '').split('\n').filter((l) => l.includes('伏笔') || l.includes('来历') || l.includes('青玉符')),
  };
  stop(tag);
  console.log(`\n=== ${tag} ===`);
  for (const [k, v] of Object.entries(out)) console.log(`  ${k}: ${typeof v === 'object' ? JSON.stringify(v) : v}`);
  return out;
}

function patchThreads(dir, fn) {
  const p = path.join(dir, 'memory/threads.json');
  const threads = readJson(p) ?? [];
  writeJson(p, fn(threads));
  return readJson(p);
}

const results = [];
const base = { ...readJson(`${SRC}/memory/threads.json`)[0] };
console.log('base thread:', JSON.stringify(base));
const phrase = (() => { const d = `${ROOT}-probe`; cp(SRC, d); const v = verbatimPhrase(d); return v; })();
console.log('verbatim phrase from newest chapter:', phrase);

// V0: untouched, real extraction shape
results.push(await measure('V0_as_is', (() => { const d = `${ROOT}-V0`; cp(SRC, d); return d; })()));

const knowledgeCharIDs = new Set((readJson(`${SRC}/knowledge.json`)?.characters ?? []).map((c) => c.id).filter(Boolean));
const seedID = results[0].charSeeds?.find((s) => knowledgeCharIDs.has(s.id))?.id;
const mk = (name, fn) => { const d = `${ROOT}-${name}`; cp(SRC, d); const t = patchThreads(d, fn); console.log(`\n--- ${name} threads.json ---\n${JSON.stringify(t, null, 2)}`); return d; };

// V1: only the relevance gate is satisfied (title verbatim); needsReview is left as extracted
results.push(await measure('V1_title_verbatim_review_kept', mk('V1', (ts) => ts.map((t) => ({ ...t, title: phrase })))));
// V2: relevance satisfied AND review cleared -> should inject
results.push(await measure('V2_title_verbatim_review_cleared', (() => { const d = `${ROOT}-V2`; cp(SRC, d); patchThreads(d, (ts) => ts.map((t) => ({ ...t, title: phrase, needsReview: false, reviewReason: '' }))); return d; })()));
// V3: original summarizing title, review cleared, character ID added (the fixture's route)
results.push(await measure('V3_orig_title_involvedID', (() => { const d = `${ROOT}-V3`; cp(SRC, d); if (!seedID) throw new Error('no character seed in V0 preview'); patchThreads(d, (ts) => ts.map((t) => ({ ...t, needsReview: false, reviewReason: '', involvedCharacters: [seedID] }))); return d; })()));
// V4: original title, review cleared, no ID -> the control that isolates the summarizing-title route
results.push(await measure('V4_orig_title_review_cleared_only', (() => { const d = `${ROOT}-V4`; cp(SRC, d); patchThreads(d, (ts) => ts.map((t) => ({ ...t, needsReview: false, reviewReason: '' }))); return d; })()));

const summary = results.map((r) => ({
  variant: r.variant,
  counts_threads: r.counts_threads,
  recalled_threads: r.recalled_threads,
  skippedReview: r.skippedReview,
  used: r.memoryUsedChars,
  sectionChars: r.memorySectionChars,
  churned: r.churnedDuringRun,
  safeState: r.safeState,
  injectedThreadLines: r.threadLinesInSection,
}));
console.log('\n===== SUMMARY =====');
console.table(summary);
fs.writeFileSync(`${ROOT}-summary.json`, JSON.stringify({ baseThread: base, phrase, seedID, results, summary }, null, 2), 'utf8');
