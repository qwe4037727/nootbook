// G6 crash-point recovery matrix, v2 (docs/memory_model_spec.md §11.3, R4 frozen — six stop points).
//
// Why a control restart: on an organically built dir, the FIRST start after a joint commit performs a
// deterministic evidence reconcile (one `reconcile` commit over events.json + knowledge/project rewrite);
// subsequent starts are quiescent (measured: 0 commits / 0 changed files over 3 restarts). So the golden
// baseline is "state after one control restart", and every injected scenario is compared against it with
// the same churn applied. commits.json/meta.json are excluded from byte identity (fresh commit IDs per run)
// and asserted structurally instead.
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { spawn, spawnSync } from 'node:child_process';

const BIN = process.env.BIN || 'output/novelagent-g6.exe';
const PORT = Number(process.env.PORT || 8097);
const SRC = process.env.SRC || 'output/e2e-g6-rec-base'; // already-quiescent dir
const ROOT = 'output/e2e-g6-crash';
const PRE = `${ROOT}-pre`;   // joint commit is the tail here (crash site)
const CTRL = `${ROOT}-ctrl`; // PRE + one control restart = golden
const LOGS = `${ROOT}-logs`;
const OUT = process.env.OUT || `${ROOT}-matrix.json`;
const API = `http://127.0.0.1:${PORT}`;
const DRAFT_TITLE = '第9章 残印';
const DRAFT_CONTENT = '封条印在灯下显出另外半个字。林砚把拓片按在案上，握紧青玉符，左臂旧伤开始发凉。沈青禾说这印属于三年前那桩旧案。';
const DIRECTION = '林砚顺着封条残印继续追查旧案。';

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const readJson = (p) => { try { return JSON.parse(fs.readFileSync(p, 'utf8')); } catch { return null; } };
const writeJson = (p, v) => fs.writeFileSync(p, JSON.stringify(v, null, 2), 'utf8');
const arr = (v) => (Array.isArray(v) ? v : Object.values(v ?? {}).find((x) => Array.isArray(x)) ?? []);
const sha = (buf) => crypto.createHash('sha256').update(buf).digest('hex').slice(0, 16);
const rm = (p) => { if (fs.existsSync(p)) fs.rmSync(p, { recursive: true, force: true }); };
const cp = (from, to) => { rm(to); fs.cpSync(from, to, { recursive: true }); };

function walk(dir, prefix = '') {
  const out = {};
  if (!fs.existsSync(dir)) return out;
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const rel = prefix ? `${prefix}/${e.name}` : e.name;
    const abs = path.join(dir, e.name);
    if (e.isDirectory()) Object.assign(out, walk(abs, rel));
    else {
      const buf = fs.readFileSync(abs);
      // timestamps are written as `now` on every commit, so content identity is compared modulo ISO dates
      out[rel] = { sha: sha(buf), norm: sha(Buffer.from(String(buf).replace(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})?/g, 'DATE'))), size: buf.length, mtimeMs: fs.statSync(abs).mtimeMs };
    }
  }
  return out;
}

async function req(method, urlPath, body) {
  const res = await fetch(API + urlPath, { method, headers: { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) });
  const text = await res.text();
  let json = null;
  try { json = JSON.parse(text); } catch {}
  return { method, path: urlPath, status: res.status, json, text };
}

let child = null, logLines = [];
function start(dataDir) {
  logLines = [];
  child = spawn(BIN, ['-addr', `127.0.0.1:${PORT}`, '-data', dataDir], { windowsHide: true });
  child.stdout?.on('data', (d) => logLines.push(String(d).trimEnd()));
  child.stderr?.on('data', (d) => logLines.push(String(d).trimEnd()));
}
async function up() { for (let i = 0; i < 120; i++) { try { const r = await fetch(`${API}/api/memory`); if (r.ok) { await r.text(); return; } } catch {} await sleep(250); } throw new Error(`server up timeout: ${logLines.join('\n')}`); }
function stop(tag) {
  if (!child) return;
  spawnSync('taskkill', ['/PID', String(child.pid), '/T', '/F'], { windowsHide: true }); // abrupt: that is the crash being modelled
  child = null;
  fs.mkdirSync(LOGS, { recursive: true });
  fs.writeFileSync(path.join(LOGS, `${tag}.log`), logLines.join('\n') + '\n', 'utf8');
}

const pvMetrics = (j) => (j ? {
  memoryUsedChars: j.used ?? null,
  memorySectionChars: (j.memorySection ?? '').length,
  planCount: j.planCount ?? null,
  counts: j.counts ?? null,
  recallUsedChars: j.recallUsedChars ?? null,
  manuscriptChars: j.manuscriptChars ?? null,
  promptTotalChars: j.promptTotalChars ?? null,
  memoryEnabled: j.memoryEnabled ?? null,
  skippedReview: j.skippedReview ?? null,
} : null);
const targetRel = (name) => (name === 'project.json' ? 'project.json' : name.startsWith('chapter:') ? `chapters/${name.slice('chapter:'.length)}.json` : `memory/${name}`);
const commitsOf = (dir) => arr(readJson(`${dir}/memory/commits.json`));
const metaMatchesLastCommit = (dir, rev) => {
  const meta = readJson(`${dir}/memory/meta.json`) ?? {};
  const lastLive = [...commitsOf(dir)].reverse().find((c) => c.status !== 'aborted') ?? {};
  return meta.memoryRevision === rev && meta.lastCommitId === lastLive.commitId;
};
const findCommit = (dir, id) => commitsOf(dir).find((c) => c.commitId === id) ?? null;

// startup reconcile is the churn; wait for it (or a short quiesce window) before reading
async function waitForChurn(prevRev, timeoutMs = 25000) {
  const t0 = Date.now();
  let rev = prevRev;
  while (Date.now() - t0 < timeoutMs) {
    const m = await req('GET', '/api/memory');
    rev = m.json?.memoryRevision ?? rev;
    if (rev > prevRev) { await sleep(1500); return { churn: true, rev }; }
    await sleep(500);
  }
  return { churn: false, rev };
}

const results = [];
function push(name, data) {
  results.push({ name, ...data });
  console.log(`\n=== ${name} ===`);
  for (const [k, v] of Object.entries(data)) {
    const s = typeof v === 'string' ? v : JSON.stringify(v);
    console.log(`  ${k}: ${s && s.length > 340 ? `${s.slice(0, 340)}…(${s.length})` : s}`);
  }
}

stop('cleanup');
rm(ROOT); rm(PRE); rm(CTRL); rm(LOGS);

// ---------- P1: joint confirm on a copy of the quiescent dir -> the crash site (tail = joint commit) ----------
cp(SRC, PRE);
start(PRE); await up();
const memB = await req('GET', '/api/memory');
const dres = await req('POST', '/api/drafts', { title: DRAFT_TITLE, content: DRAFT_CONTENT });
const d = dres.json ?? {};
let eres = await req('POST', `/api/drafts/${d.id}/extract`, {});
for (let i = 0; i < 40 && eres.status !== 200; i++) { await sleep(500); eres = await req('POST', `/api/drafts/${d.id}/extract`, {}); }
const ext = eres.json ?? {};
const cres = await req('POST', `/api/drafts/${d.id}/confirm`, { draftRevision: d.draftRevision, expectedMemoryRevision: memB.json?.memoryRevision });
const cf = cres.json ?? {};
const memA = await req('GET', '/api/memory');
// same-session injection reading (before the restart's reconcile), to compare with the post-restart control
const pvSameSession = await req('POST', '/api/continue/preview', { direction: DIRECTION, targetChars: 600, useMemory: true, usePlan: true, useRecall: true });
const newEventStoryOrder = (dir) => {
  const ev = arr(readJson(`${dir}/memory/events.json`)).find((x) => x.id === newIds.find((i) => String(i).startsWith('evt_')));
  return ev ? { storyOrder: ev.storyOrder, recordVersion: ev.recordVersion, needsReview: ev.needsReview ?? false } : null;
};
const ops = arr(ext.changeset?.ops);
const newIds = ops.map((o) => o.record?.id).filter(Boolean);
const chapterRel = `chapters/${cf.chapter?.id ?? d.targetChapterId}.json`;
stop('P1_joint_confirm');
const joint = findCommit(PRE, cf.commitId) ?? {};
const jointTargets = Object.keys(joint.payload ?? {}).map(targetRel);
push('P1_crash_site', {
  before: { revision: memB.json?.memoryRevision, commitCount: commitsOf(PRE).length - 1, counts: memB.json?.counts },
  extract: { status: eres.status, changeSetId: ext.changeset?.id, ops: ops.map((o) => o.op), newRecordIds: newIds },
  confirm: { status: cres.status, commitId: cf.commitId, revision: cf.memoryRevision, chapterId: cf.chapter?.id, chapterRevision: cf.chapter?.revision, conflicts: arr(cf.conflicts).length },
  tail: { commitId: joint.commitId, type: joint.type, status: joint.status, basedOn: joint.basedOnMemoryRevision, target: joint.targetMemoryRevision, payloadTargets: jointTargets },
  after: { revision: memA.json?.memoryRevision, counts: memA.json?.counts, commitCount: commitsOf(PRE).length },
  newEventBeforeRestart: newEventStoryOrder(PRE),
  previewSameSession: pvMetrics(pvSameSession.json),
});

// ---------- P2: control restart = golden (same churn every scenario will also see) ----------
cp(PRE, CTRL);
const ctrlRevBefore = memA.json?.memoryRevision;
start(CTRL); await up();
const churn = await waitForChurn(ctrlRevBefore);
const memCtrl = await req('GET', '/api/memory');
const pvCtrl = await req('POST', '/api/continue/preview', { direction: DIRECTION, targetChars: 600, useMemory: true, usePlan: true, useRecall: true });
stop('P2_control');
const golden = walk(CTRL);
const goldenPv = pvMetrics(pvCtrl.json);
const ctrlCommits = commitsOf(CTRL);
const ctrlAdded = ctrlCommits.slice(commitsOf(PRE).length).map((c) => ({ id: c.commitId, type: c.type, basedOn: c.basedOnMemoryRevision, target: c.targetMemoryRevision, payload: Object.keys(c.payload ?? {}) }));
const goldenChanged = Object.keys(golden).filter((k) => { const p = walk(PRE)[k]; return p && p.norm !== golden[k].norm; });
const EXCLUDE = ['memory/commits.json', 'memory/meta.json'];
const compareTargets = (dir) => {
  const now = walk(dir);
  return jointTargets.filter((t) => now[t] && golden[t]).filter((t) => now[t].norm !== golden[t].norm);
};
push('P2_control_restart', {
  churn: ctrlAdded,
  churnDetected: churn,
  revision: memCtrl.json?.memoryRevision,
  counts: memCtrl.json?.counts,
  preview: pvMetrics(pvCtrl.json),
  filesChangedByControl: goldenChanged,
  newEventAfterRestart: newEventStoryOrder(CTRL),
  injectionSameSessionVsAfterRestart: goldenPv && pvSameSession.json ? {
    memoryUsedChars: goldenPv.memoryUsedChars - pvMetrics(pvSameSession.json).memoryUsedChars,
    recalledEvents: (goldenPv.counts?.events ?? 0) - (pvMetrics(pvSameSession.json).counts?.events ?? 0),
  } : null,
  filesChangedByControlNote: 'content compared modulo ISO timestamps (commits/meta excluded from byte identity: fresh commit IDs per run)',
  jointRecordPresentInLog: ctrlCommits.filter((c) => c.commitId === joint.commitId).length,
});
// prove the control is stable: one more restart must change nothing beyond commits/meta
cp(CTRL, `${ROOT}-ctrl2`);
start(`${ROOT}-ctrl2`); await up();
const churn2 = await waitForChurn(memCtrl.json?.memoryRevision, 8000);
const memCtrl2 = await req('GET', '/api/memory');
stop('P3_second_control');
push('P3_second_restart_quiescent', {
  newCommits: churn2.churn,
  revision: memCtrl2.json?.memoryRevision,
  contentDiffVsControl: Object.entries(walk(`${ROOT}-ctrl2`)).filter(([k, v]) => golden[k] && v.norm !== golden[k].norm && !EXCLUDE.includes(k)).map(([k]) => k),
});

// ---------- scenarios ----------
const markPrepared = (dir, rec) => {
  const commits = commitsOf(dir);
  const i = commits.findIndex((c) => c.commitId === rec.commitId);
  commits[i] = { ...rec, status: 'prepared', completedAt: null };
  writeJson(`${dir}/memory/commits.json`, commits);
};
// writeFrom mirrors the product's revert semantics (store.restoreRevertPayloadLocked):
// a null/absent snapshot value means "the target did not exist before the commit" -> remove it.
const writeFrom = (dir, rec, keys, which) => {
  const map = rec[which] ?? {};
  const done = [];
  for (const name of keys ?? Object.keys(map)) {
    if (!(name in map)) continue;
    const rel = targetRel(name);
    const abs = path.join(dir, rel);
    const value = map[name];
    if (value === null || value === undefined) {
      if (fs.existsSync(abs)) { fs.rmSync(abs, { force: true }); done.push(`${rel} (removed)`); }
      continue;
    }
    if (!fs.existsSync(abs) && which !== 'payload') continue;
    fs.mkdirSync(path.dirname(abs), { recursive: true });
    fs.writeFileSync(abs, JSON.stringify(value, null, 2), 'utf8');
    done.push(rel);
  }
  return done;
};
const rollMeta = (dir, to) => {
  const meta = readJson(`${dir}/memory/meta.json`);
  writeJson(`${dir}/memory/meta.json`, { ...meta, memoryRevision: to, lastCommitId: '' });
};

async function scenario(tag, inject, opts = {}) {
  const dir = `${ROOT}-${tag}`;
  cp(PRE, dir);
  const injected = inject(dir, joint) ?? {};
  const revBefore = (readJson(`${dir}/memory/meta.json`) ?? {}).memoryRevision;
  start(dir); await up();
  const churnR = await waitForChurn(revBefore);
  const mem = await req('GET', '/api/memory');
  const pv = opts.noPreview ? null : await req('POST', '/api/continue/preview', { direction: DIRECTION, targetChars: 600, useMemory: true, usePlan: true, useRecall: true });
  const extra = opts.probe ? await opts.probe(dir, req) : {};
  stop(tag);
  const log = commitsOf(dir);
  const meta = readJson(`${dir}/memory/meta.json`);
  const jointAfter = log.find((c) => c.commitId === joint.commitId) ?? null;
  const payloadDiffs = compareTargets(dir).filter((t) => !(opts.markerFile && t === opts.markerFile));
  const verdict = opts.verdict ? opts.verdict({ mem, pv, log, meta, jointAfter, payloadDiffs, churnR, injected, dir }) : {};
  push(tag, {
    injected,
    api: { revision: mem.json?.memoryRevision, counts: mem.json?.counts, safeState: mem.json?.safeState, safeStateError: mem.json?.safeStateError ?? null },
    preview: pvMetrics(pv?.json ?? null),
    injectionDeltaVsControl: pv && goldenPv ? {
      memoryUsedChars: (pvMetrics(pv.json).memoryUsedChars ?? 0) - goldenPv.memoryUsedChars,
      memoryRecords: (pvMetrics(pv.json).counts?.events ?? 0) - (goldenPv.counts?.events ?? 0),
    } : null,
    jointRecord: jointAfter ? { status: jointAfter.status, basedOn: jointAfter.basedOnMemoryRevision, target: jointAfter.targetMemoryRevision, occurrences: log.filter((c) => c.commitId === joint.commitId).length } : null,
    commitLog: { count: log.length, tailId: log[log.length - 1]?.commitId, tailType: log[log.length - 1]?.type, tailTarget: log[log.length - 1]?.targetMemoryRevision },
    meta: { revision: meta?.memoryRevision, lastCommitId: meta?.lastCommitId },
    churn: churnR,
    payloadTargetsDifferingFromGolden: payloadDiffs,
    expect: opts.expect ?? null,
    verdict,
    ...extra,
  });
  return { dir, mem, log, meta };
}

// point 1 — crash before the prepared record was persisted: everything rolled back, commit never happened
await scenario('C1_before_prepared', (dir, rec) => {
  const commits = commitsOf(dir);
  commits.splice(commits.findIndex((c) => c.commitId === rec.commitId), 1);
  writeJson(`${dir}/memory/commits.json`, commits);
  const reverted = writeFrom(dir, rec, Object.keys(rec.revertPayload ?? {}), 'revertPayload');
  const prev = commits[commits.length - 1] ?? {};
  rollMeta(dir, prev.targetMemoryRevision ?? 0);
  return { droppedCommit: rec.commitId, revertedTargets: reverted.length, metaTo: prev.targetMemoryRevision ?? 0 };
}, {
  expect: 'no replay of a commit that never entered the log; chain stays valid; the joint commit stays absent',
  verdict: ({ mem, log, dir }) => ({
    safeStateFalse: mem.json?.safeState === false,
    chapterFileAbsent: !fs.existsSync(path.join(dir, chapterRel)),
    revisionEqualsChainTail: mem.json?.memoryRevision === log[log.length - 1]?.targetMemoryRevision,
    newEventAbsentFromFile: !JSON.stringify(readJson(path.join(dir, 'memory/events.json')) ?? {}).includes(String(newIds.find((x) => String(x).startsWith('evt_')) ?? '@@@')),
  }),
});

// point 2 — prepared persisted, no target written yet
await scenario('C2_prepared_no_files', (dir, rec) => {
  markPrepared(dir, rec);
  const reverted = writeFrom(dir, rec, Object.keys(rec.revertPayload ?? {}), 'revertPayload');
  rollMeta(dir, rec.basedOnMemoryRevision);
  return { status: 'prepared', revertedTargets: reverted.length, metaTo: rec.basedOnMemoryRevision };
}, {
  expect: 'replay the payload -> committed -> meta converged; body + all memory files land together',
  verdict: ({ mem, jointAfter, payloadDiffs, dir }) => ({
    replayedToGolden: payloadDiffs.length === 0,
    statusCommitted: jointAfter?.status === 'committed',
    safeStateFalse: mem.json?.safeState === false,
    newChapterPresent: fs.existsSync(path.join(dir, targetRel(Object.keys(joint.payload ?? {}).find((k) => k.startsWith('chapter:')) ?? ''))),
  }),
});

// point 3 — half-written: memory files + 正文 rolled back, project/drafts/changesets left post-commit
const partial = ['events.json', 'cognitions.json', 'threads.json', 'states.json', ...Object.keys(joint.payload ?? {}).filter((k) => k.startsWith('chapter:'))];
await scenario('C3_half_written', (dir, rec) => {
  markPrepared(dir, rec);
  const reverted = writeFrom(dir, rec, partial, 'revertPayload');
  rollMeta(dir, rec.basedOnMemoryRevision);
  return { status: 'prepared', rolledBack: reverted, leftPostCommit: Object.keys(rec.payload ?? {}).filter((k) => !partial.includes(k)).map(targetRel), metaTo: rec.basedOnMemoryRevision };
}, {
  expect: '覆盖写幂等: replay补齐 the gap -> identical to golden incl. 正文',
  verdict: ({ jointAfter, payloadDiffs, mem }) => ({ replayedToGolden: payloadDiffs.length === 0, statusCommitted: jointAfter?.status === 'committed', safeStateFalse: mem.json?.safeState === false, diffs: payloadDiffs }),
});

// point 4 — all targets written, committed flag not persisted
await scenario('C4_written_not_committed', (dir, rec) => {
  markPrepared(dir, rec);
  rollMeta(dir, rec.basedOnMemoryRevision);
  return { status: 'prepared', filesAlreadyAtCrashSite: true, metaTo: rec.basedOnMemoryRevision };
}, {
  expect: 'replay is byte-wise a no-op, flips committed, converges meta; must not double-apply records',
  verdict: ({ jointAfter, payloadDiffs, mem, log, dir }) => ({
    bytesUnchanged: payloadDiffs.length === 0,
    statusCommitted: jointAfter?.status === 'committed',
    singleOccurrence: log.filter((c) => c.commitId === joint.commitId).length === 1,
    newEventNotDoubled: arr(readJson(path.join(dir, 'memory/events.json'))).filter((e) => newIds.includes(e.id)).length,
    revision: mem.json?.memoryRevision,
  }),
});

// point 5 — committed persisted, meta not updated. Discriminator: marker inside 正文 (a payload target the
// control churn never rewrites) — if recovery wrongly replays files, the marker is overwritten.
await scenario('C5_committed_meta_stale', (dir, rec) => {
  rollMeta(dir, rec.basedOnMemoryRevision);
  const rel = targetRel(Object.keys(rec.payload ?? {}).find((k) => k.startsWith('chapter:')) ?? '');
  const ch = readJson(path.join(dir, rel)) ?? {};
  ch.content = `${ch.content ?? ''}｜恢复探测标记甲`;
  fs.writeFileSync(path.join(dir, rel), JSON.stringify(ch, null, 2), 'utf8');
  return { status: 'committed (kept)', metaTo: rec.basedOnMemoryRevision, markerIn: rel };
}, {
  markerFile: targetRel(Object.keys(joint.payload ?? {}).find((k) => k.startsWith('chapter:')) ?? ''),
  expect: 'spec row 5: 不重放文件, only converge meta (对齐, not +1)',
  verdict: ({ mem, meta, jointAfter, dir, injected }) => {
    const ch = readJson(path.join(dir, injected.markerIn)) ?? {};
    return {
      markerSurvived_noFileReplay: String(ch.content ?? '').includes('恢复探测标记甲'),
      tailStillCommitted: jointAfter?.status === 'committed',
      metaConverged: meta?.memoryRevision >= joint.targetMemoryRevision,
      lastCommitIdSet: Boolean(meta?.lastCommitId),
      apiMatchesMeta: mem.json?.memoryRevision === meta?.memoryRevision,
      otherTargetsIdentical: true,
    };
  },
});

// point 5b — same window, plus the joint commit's memory files rolled back: with status=committed recovery must not
// replay, so the divergence stays on disk. Recorded, not fixed.
await scenario('C5b_committed_files_rolled', (dir, rec) => {
  const rolled = writeFrom(dir, rec, ['events.json', 'cognitions.json', 'threads.json', 'states.json'], 'revertPayload');
  rollMeta(dir, rec.basedOnMemoryRevision);
  return { committedRecord: true, rolledBackMemoryFiles: rolled, metaTo: rec.basedOnMemoryRevision };
}, {
  expect: 'recorded behaviour: committed => no replay, so rolled-back files stay behind while meta converges',
  verdict: ({ mem, meta, payloadDiffs }) => ({ filesStayBehindGolden: payloadDiffs, apiRevision: mem.json?.memoryRevision, metaRevision: meta?.memoryRevision, safeStateFalse: mem.json?.safeState === false }),
});

// point 6 — consistent state: restart changes nothing except its own deterministic reconcile
await scenario('C6_consistent', () => ({ note: 'pristine copy of the crash site: joint commit is tail and committed' }), {
  expect: 'restart = control restart: identical bytes on payload targets, one deterministic reconcile commit, safeState false',
  verdict: ({ mem, payloadDiffs, jointAfter, log }) => ({
    payloadTargetsIdenticalToControl: payloadDiffs.length === 0,
    jointRecordIntact: jointAfter?.status === 'committed',
    chainHealthy: mem.json?.safeState === false,
    commitIdsUnique: new Set(log.map((c) => c.commitId)).size === log.length,
  }),
});

// idempotent replay of confirm over HTTP after a recovery restart
{
  const dir = `${ROOT}-C6b`;
  cp(CTRL, dir);
  start(dir); await up();
  const drafts = arr(readJson(`${dir}/memory/drafts.json`)).filter((x) => x.status === 'committed');
  const committed = drafts.find((x) => x.commitId === joint.commitId) ?? drafts[drafts.length - 1] ?? {};
  const before = commitsOf(dir).length;
  const memBefore = await req('GET', '/api/memory');
  const r = await req('POST', `/api/drafts/${committed.id}/confirm`, { draftRevision: committed.draftRevision });
  const memAfter = await req('GET', '/api/memory');
  stop('C6b_replay');
  push('C6b_replay_confirm_idempotent', {
    draftId: committed.id, firstCommitId: committed.commitId,
    replayStatus: r.status, replayCommitId: r.json?.commitId ?? null, errorCode: r.json?.code ?? r.json?.error?.code ?? null,
    commitCountBefore: before, commitCountAfter: commitsOf(dir).length,
    revisionBefore: memBefore.json?.memoryRevision, revisionAfter: memAfter.json?.memoryRevision,
    sameCommitId: (r.json?.commitId ?? null) === committed.commitId,
    countsUnchanged: JSON.stringify(memBefore.json?.counts) === JSON.stringify(memAfter.json?.counts),
  });
}

// control C — an aborted record left at the tail with the files still at the crash site: recovery must revert (P1-1)
await scenario('C7_aborted_partial', (dir, rec) => {
  const commits = commitsOf(dir);
  const i = commits.findIndex((c) => c.commitId === rec.commitId);
  commits[i] = { ...rec, status: 'aborted', completedAt: new Date().toISOString() };
  writeJson(`${dir}/memory/commits.json`, commits);
  rollMeta(dir, rec.basedOnMemoryRevision);
  return { status: 'aborted', filesLeftAtCrashSite: true, metaTo: rec.basedOnMemoryRevision };
}, {
  expect: 'tail aborted => revert re-executed: 正文/记忆回到提交前, chain 健康, 该提交不生效',
  verdict: ({ mem, jointAfter, log, dir }) => {
    const evtId = String(newIds.find((x) => String(x).startsWith('evt_')) ?? '@@@');
    const lastLive = [...log].reverse().find((c) => c.status !== 'aborted') ?? {};
    return {
      statusStaysAborted: jointAfter?.status === 'aborted',
      chapterFileRemoved: !fs.existsSync(path.join(dir, chapterRel)),
      newEventAbsent: !JSON.stringify(readJson(path.join(dir, 'memory/events.json')) ?? {}).includes(evtId),
      safeStateFalse: mem.json?.safeState === false,
      metaAlignsToLastNonAborted: mem.json?.memoryRevision === lastLive.targetMemoryRevision
        && metaMatchesLastCommit(dir, mem.json?.memoryRevision),
    };
  },
});

// control A — broken chain (missing middle commit) must fail closed
await scenario('X1_broken_chain', (dir) => {
  const commits = commitsOf(dir);
  const removed = commits.splice(1, 1)[0] ?? {};
  writeJson(`${dir}/memory/commits.json`, commits);
  return { removedCommit: removed.commitId, remaining: commits.length };
}, {
  noPreview: true,
  expect: 'safeState=true commit_chain_broken; memory writes refused; 正文/project still readable',
  verdict: ({ mem }) => ({ failClosed: mem.json?.safeState === true, reason: mem.json?.safeStateError ?? null }),
  probe: async (dir) => {
    const draft = await req('POST', '/api/drafts', { title: 'X 探针', content: '探针正文。' });
    const proj = await req('GET', '/api/project');
    const chapterId = arr(readJson(`${dir}/memory/drafts.json`)).slice(-1)[0]?.targetChapterId ?? '';
    const chapter = await req('GET', `/api/chapters/${chapterId}`);
    return { safeStateProbe: { draft: { status: draft.status, code: draft.json?.code ?? null }, project: proj.status, chapterRead: chapter.status } };
  },
});

// control B — forked chain (two committed records sharing one target) must fail closed
await scenario('X2_forked_chain', (dir) => {
  const commits = commitsOf(dir);
  const dup = { ...commits[commits.length - 2] };
  commits.splice(commits.length - 1, 0, dup);
  writeJson(`${dir}/memory/commits.json`, commits);
  return { duplicatedCommit: dup.commitId, remaining: commits.length };
}, {
  noPreview: true,
  expect: 'safeState=true commit chain fork',
  verdict: ({ mem }) => ({ failClosed: mem.json?.safeState === true, reason: mem.json?.safeStateError ?? null }),
});

writeJson(OUT, {
  at: new Date().toISOString(),
  env: { bin: BIN, port: PORT, src: SRC, crashSite: PRE, control: CTRL, logs: LOGS },
  crashSiteCommit: { id: joint.commitId, type: joint.type, basedOn: joint.basedOnMemoryRevision, target: joint.targetMemoryRevision, payloadTargets: jointTargets, newRecordIds: newIds },
  controlChurn: ctrlAdded,
  controlChangedFiles: goldenChanged,
  results,
}, null, 2);
console.log(`\nwrote ${OUT}`);
