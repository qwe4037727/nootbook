// Why does the same data dir inject a different number of memory chars before and after a restart?
// The joint commit writes the new event with storyOrder=0; the next start's reconcile recomputes it.
// This probe captures both rendered memory sections on one dir so the difference is character-level, not inferred.
import fs from 'node:fs';
import path from 'node:path';
import { spawn, spawnSync } from 'node:child_process';

const BIN = process.env.BIN || 'output/novelagent-g6.exe';
const PORT = Number(process.env.PORT || 8097);
const SRC = process.env.SRC || 'output/e2e-g6-rec-base';
const DIR = 'output/e2e-g6-so-probe';
const API = `http://127.0.0.1:${PORT}`;
const DRAFT_TITLE = '第9章 残印';
const DRAFT_CONTENT = '封条印在灯下显出另外半个字。林砚把拓片按在案上，握紧青玉符，左臂旧伤开始发凉。沈青禾说这印属于三年前那桩旧案。';
const DIRECTION = '林砚顺着封条残印继续追查旧案。';

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const readJson = (p) => { try { return JSON.parse(fs.readFileSync(p, 'utf8')); } catch { return null; } };
const arr = (v) => (Array.isArray(v) ? v : Object.values(v ?? {}).find((x) => Array.isArray(x)) ?? []);

async function req(method, urlPath, body) {
  const res = await fetch(API + urlPath, { method, headers: { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) });
  const text = await res.text();
  let json = null;
  try { json = JSON.parse(text); } catch {}
  return { status: res.status, json, text };
}

let child = null;
const start = () => { child = spawn(BIN, ['-addr', `127.0.0.1:${PORT}`, '-data', DIR], { windowsHide: true }); };
async function up() { for (let i = 0; i < 120; i++) { try { const r = await fetch(`${API}/api/memory`); if (r.ok) { await r.text(); return; } } catch {} await sleep(250); } throw new Error('up timeout'); }
function stop() { if (child) { spawnSync('taskkill', ['/PID', String(child.pid), '/T', '/F'], { windowsHide: true }); child = null; } }

fs.rmSync(DIR, { recursive: true, force: true });
fs.cpSync(SRC, DIR, { recursive: true });

const eventState = (id) => {
  const ev = arr(readJson(`${DIR}/memory/events.json`)).find((x) => x.id === id) ?? {};
  return { storyOrder: ev.storyOrder ?? null, recordVersion: ev.recordVersion ?? null };
};

start(); await up();
const mem0 = await req('GET', '/api/memory');
const dres = await req('POST', '/api/drafts', { title: DRAFT_TITLE, content: DRAFT_CONTENT });
const d = dres.json ?? {};
let eres = await req('POST', `/api/drafts/${d.id}/extract`, {});
for (let i = 0; i < 40 && eres.status !== 200; i++) { await sleep(500); eres = await req('POST', `/api/drafts/${d.id}/extract`, {}); }
const newEventId = arr(eres.json?.changeset?.ops).map((o) => o.record?.id).find((x) => String(x).startsWith('evt_'));
const cres = await req('POST', `/api/drafts/${d.id}/confirm`, { draftRevision: d.draftRevision, expectedMemoryRevision: mem0.json?.memoryRevision });
const pv1 = await req('POST', '/api/continue/preview', { direction: DIRECTION, targetChars: 600, useMemory: true, usePlan: true, useRecall: true });
const before = { revision: mem0.json?.memoryRevision, afterConfirm: cres.json?.memoryRevision, commitId: cres.json?.commitId, event: eventState(newEventId), used: pv1.json?.used, order: (pv1.json?.records ?? []).map((r) => `${r.kind}:${r.id?.slice(-6)}:${r.storyOrder ?? ''}`), section: pv1.json?.memorySection };
stop();

start(); await up();
for (let i = 0; i < 60 && (await req('GET', '/api/memory')).json?.memoryRevision === before.afterConfirm; i++) await sleep(500);
const mem2 = await req('GET', '/api/memory');
const pv2 = await req('POST', '/api/continue/preview', { direction: DIRECTION, targetChars: 600, useMemory: true, usePlan: true, useRecall: true });
stop();

const after = { revision: mem2.json?.memoryRevision, event: eventState(newEventId), used: pv2.json?.used, order: (pv2.json?.records ?? []).map((r) => `${r.kind}:${r.id?.slice(-6)}:${r.storyOrder ?? ''}`), section: pv2.json?.memorySection };

const sec = (s) => String(s ?? '').split('\n');
const a = sec(before.section), b = sec(after.section);
const diff = [];
for (let i = 0; i < Math.max(a.length, b.length); i++) if (a[i] !== b[i]) diff.push({ line: i, before: a[i] ?? null, after: b[i] ?? null });

const out = { at: new Date().toISOString(), dir: DIR, newEventId, before, after, sectionDiffLines: diff, summary: {
  usedBefore: before.used, usedAfter: after.used, delta: (after.used ?? 0) - (before.used ?? 0),
  sameRecordSet: JSON.stringify(before.order) === JSON.stringify(after.order),
  storyOrderBefore: before.event.storyOrder, storyOrderAfter: after.event.storyOrder,
  revisionBefore: before.afterConfirm, revisionAfter: after.revision,
} };
fs.writeFileSync('output/e2e-g6-storyorder-probe.json', JSON.stringify(out, null, 2), 'utf8');
console.log(JSON.stringify(out.summary, null, 2));
console.log('\ndiff lines:');
for (const dd of diff) console.log(`  [${dd.line}] - ${dd.before}\n       + ${dd.after}`);
console.log(`\nwrote output/e2e-g6-storyorder-probe.json`);
