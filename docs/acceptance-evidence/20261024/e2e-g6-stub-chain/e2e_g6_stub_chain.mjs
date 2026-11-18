// Acceptance driver for G6: draft + memory changeset confirmed as ONE atomic commit,
// over the real server binary and real HTTP against the local stub provider (no model key).
// Guards probed: draft-managed changeset cannot be accepted/rebased directly;
// stale memory baseline / stale draft revision / edited-but-not-re-extracted are all refused
// WITHOUT landing anything; confirm lands chapter + memory in a single commit;
// reject lands nothing; heal then marks the draft-derived memory.
import fs from 'node:fs';

const BASE = process.env.BASE || 'http://127.0.0.1:8097';
const DATA = process.env.DATA || 'output/e2e-g6-data';
const OUT_DIR = process.env.OUT_DIR || 'docs/acceptance-evidence/20261024/e2e-g6-stub-chain';
const STUB_LOG = process.env.STUB_LOG || 'output/stub-requests-g6.jsonl';
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function req(method, path, body) {
  const res = await fetch(BASE + path, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  let json = null;
  try { json = JSON.parse(text); } catch {}
  return { method, path, status: res.status, json, text };
}

async function poll(path, isDone, { timeoutMs = 120000, everyMs = 400 } = {}) {
  const t0 = Date.now();
  let last = null;
  while (Date.now() - t0 < timeoutMs) {
    last = await req('GET', path);
    if (isDone(last.json)) return { ...last, waitedMs: Date.now() - t0 };
    await sleep(everyMs);
  }
  return { ...last, timedOut: true, waitedMs: Date.now() - t0 };
}

const steps = [];
function record(name, r, extra = {}) {
  const item = {
    name,
    status: r.status ?? null,
    timedOut: r.timedOut ?? false,
    waitedMs: r.waitedMs ?? null,
    body: r.json ?? (r.text ? r.text.slice(0, 300) : null),
    ...extra,
  };
  steps.push(item);
  console.log(`${name}: status=${item.status}${item.timedOut ? ' TIMEOUT' : ''}`);
  return item;
}

const readJson = (p) => {
  try { return JSON.parse(fs.readFileSync(p, 'utf8')); } catch { return null; }
};
const listWrap = (v) => (Array.isArray(v) ? v : []);
function commitsFile() {
  const c = readJson(`${DATA}/memory/commits.json`);
  return listWrap(Array.isArray(c) ? c : (c?.commits ?? c?.items ?? []));
}
function eventsFile() {
  const e = readJson(`${DATA}/memory/events.json`);
  return listWrap(Array.isArray(e) ? e : (e?.records ?? e?.events ?? []));
}
function draftsFile() {
  const d = readJson(`${DATA}/memory/drafts.json`);
  return listWrap(Array.isArray(d) ? d : (d?.drafts ?? []));
}

const CHAPTERS = [
  ['第1章 值房', '雨落在青石板上。林砚在县衙值房核对三年前的卷宗，灯芯爆了一下。他把抄好的纸叠起来，吹熄了灯。'],
  ['第2章 医者', '沈青禾提着药箱上门，替林砚换掉左臂的旧纱布。她说这伤遇雨会发凉，劝他别再提刀。林砚没有答应。'],
  ['第3章 旧案', '夜里的县衙外头很静。林砚站在槐树下，右手握紧青玉符，左臂旧伤开始发凉。他想起卷宗里被撕去的那一页，也想起沈青禾欲言又止的神情。青玉符上的刻痕像一只没有闭上的眼睛。'],
  ['第4章 卷房', '卷房的门闩是新的。林砚推门进去，发现登记簿少了两页。他把青玉符压在镇纸下面，逐格翻检。'],
  ['第5章 对峙', '有人在廊下等他。林砚停下脚步，对方只留下一句"别再查了"便转身离开。'],
  ['第6章 伤药', '沈青禾把一瓶伤药放在门槛上。林砚拿起瓶子，闻到一股苦味。'],
].map(([title, content]) => `${title}\n${content}`).join('\n\n');

// Draft text deliberately contains the stub's planted evidence quotes, so draft-derived
// ops get located against the draft rather than dropped as evidence_unlocated.
const DRAFT_TEXT = '雨停之前，林砚在桥头握紧青玉符，左臂旧伤开始发凉。他把青玉符上的刻痕拓在纸上，发现拓片少了一角。沈青禾认出那是三年前旧案的封条印。';
const DRAFT_PATCHED = DRAFT_TEXT + '林砚把拓片折好，收进袖中。';

const MARKERS = {
  eventSummary: '林砚在县衙外握紧青玉符',
  draftTextHead: '雨停之前，林砚在桥头握紧青玉符',
  stateValue: '左臂旧伤开始发凉',
};

// 01 provider + 02 import + 03 analyze
record('01_put_config', await req('PUT', '/api/config', {
  baseUrl: 'http://127.0.0.1:8098/v1',
  model: 'stub-model',
  apiKey: 'stub-key-not-a-secret',
  contextChars: 12000,
}));
const imp = await req('POST', '/api/import', { title: '桩模型 G6 测试稿', text: CHAPTERS });
record('02_import', imp, { importedChapters: imp.json?.importedChapters });

const proj0 = await req('GET', '/api/project');
const chapters0 = listWrap(proj0.json?.project?.chapters ?? proj0.json?.chapters);
const ch3 = chapters0.find((c) => (c.title || '').includes('3')) ?? chapters0[2];
record('03_project_after_import', proj0, { chapterCount: chapters0.length, ch3Id: ch3?.id });

record('04_post_analyze', await req('POST', '/api/analyze', {}));
const an = await poll('/api/project', (p) => {
  const a = p?.project?.analysis ?? p?.analysis;
  return a && (a.status === 'ready' || a.status === 'error');
});
record('05_analysis_finished', an, { analysis: an.json?.project?.analysis ?? an.json?.analysis });

// 06 base memory: initialize + one deep extract + accept (so the draft chain is not the first writer)
record('07_post_initialize', await req('POST', '/api/memory/initialize', {}));
const initDone = await poll('/api/memory/initialize/status', (s) => ['ready', 'partial', 'failed'].includes(s?.status), { timeoutMs: 60000 });
record('08_initialize_status', initDone, { initStatus: initDone.json?.status });
record('09_post_extract', await req('POST', '/api/memory/extract', { chapterIds: [ch3.id] }));
const extDone = await poll('/api/memory/extract/status', (s) => ['done', 'failed', 'partial'].includes(s?.status), { timeoutMs: 90000 });
record('10_extract_status', extDone, { extractStats: extDone.json?.stats ?? extDone.json });
const csList = await req('GET', '/api/memory/changesets?status=proposed');
const csArr = Array.isArray(csList.json) ? csList.json : listWrap(Object.values(csList.json ?? {}).find((v) => Array.isArray(v)));
const accepts = [];
for (const cs of csArr) {
  const a = await req('POST', `/api/memory/changesets/${cs.id}/accept`, {});
  accepts.push({ id: cs.id, status: a.status, result: a.json });
}
record('11_accept_base_changesets', { status: 200, json: accepts });

const mem0 = await req('GET', '/api/memory');
const commits0 = commitsFile().length;
record('12_state_before_draft', mem0, {
  memoryRevision: mem0.json?.memoryRevision,
  counts: mem0.json?.counts,
  safeState: mem0.json?.safeState,
  commitCount: commits0,
});

// ===== G6 A: new-chapter draft, full guardrail ladder, atomic joint commit =====
const d1res = await req('POST', '/api/drafts', { title: '第7章 拓片', content: DRAFT_TEXT });
const d1 = d1res.json ?? {};
record('13_draft_create', d1res, {
  draftId: d1.id, targetChapterId: d1.targetChapterId, newChapter: d1.newChapter,
  draftRevision: d1.draftRevision, baseMemoryRevision: d1.baseMemoryRevision, status: d1.status,
});

const e1res = await req('POST', `/api/drafts/${d1.id}/extract`, {});
const e1 = e1res.json ?? {};
record('14_draft_extract', e1res, {
  changeSetId: e1.changeset?.id,
  ops: listWrap(e1.changeset?.ops).map((o) => o.op),
  csDraftRevision: e1.changeset?.draftRevision,
  csBaseMemoryRevision: e1.changeset?.baseMemoryRevision,
  draftChangeSetId: e1.draft?.memoryChangeSetId,
  located: listWrap(e1.changeset?.ops).map((o) => o.sourceRefs ?? o.evidence ?? null),
});

const csId = e1.changeset?.id;
record('15_guardrail_accept_direct', await req('POST', `/api/memory/changesets/${csId}/accept`, {}));
record('16_guardrail_rebase_direct', await req('POST', `/api/memory/changesets/${csId}/rebase`, {}));

const projBefore = await req('GET', '/api/project');
record('17_preview_before_confirm', await req('POST', '/api/continue/preview', { direction: '林砚顺着青玉符的刻痕继续追查旧案。', targetChars: 600, useMemory: true, usePlan: true, useRecall: true }), {
  chapterCount: listWrap(projBefore.json?.project?.chapters ?? projBefore.json?.chapters).length,
  memoryRevision: mem0.json?.memoryRevision,
});

// negative: stale memory baseline must be refused without landing anything
const neg1 = await req('POST', `/api/drafts/${d1.id}/confirm`, { draftRevision: d1.draftRevision, expectedMemoryRevision: (mem0.json?.memoryRevision ?? 0) + 7 });
record('18_negative_stale_memory_baseline', neg1, { error: neg1.json?.error?.code ?? neg1.json?.code });
const memNeg1 = await req('GET', '/api/memory');
const projNeg1 = await req('GET', '/api/project');
record('19_atomicity_after_negative', memNeg1, {
  memoryRevisionUnchanged: memNeg1.json?.memoryRevision === mem0.json?.memoryRevision,
  countsUnchanged: JSON.stringify(memNeg1.json?.counts) === JSON.stringify(mem0.json?.counts),
  chapterCountUnchanged: listWrap(projNeg1.json?.project?.chapters ?? projNeg1.json?.chapters).length === listWrap(projBefore.json?.project?.chapters ?? projBefore.json?.chapters).length,
  commitCountUnchanged: commitsFile().length === commits0,
  draftStillPending: draftsFile().find((x) => x.id === d1.id)?.status,
});

// negative: optimistic draft revision
const patch1 = await req('PATCH', `/api/drafts/${d1.id}`, { draftRevision: d1.draftRevision, content: DRAFT_PATCHED });
record('20_draft_patch', patch1, { newDraftRevision: patch1.json?.draftRevision });
const neg2 = await req('POST', `/api/drafts/${d1.id}/confirm`, { draftRevision: d1.draftRevision });
record('21_negative_old_draft_revision', neg2, { error: neg2.json?.error?.code ?? neg2.json?.code });
record('22_negative_needs_recheck', await req('POST', `/api/drafts/${d1.id}/confirm`, { draftRevision: patch1.json?.draftRevision, expectedMemoryRevision: mem0.json?.memoryRevision }), { note: 'content changed after extract: changeset is stale' });

const e2res = await req('POST', `/api/drafts/${d1.id}/extract`, {});
const e2 = e2res.json ?? {};
record('23_draft_reextract', e2res, { changeSetId: e2.changeset?.id, csDraftRevision: e2.changeset?.draftRevision, ops: listWrap(e2.changeset?.ops).map((o) => o.op) });

const cf = await req('POST', `/api/drafts/${d1.id}/confirm`, { draftRevision: patch1.json?.draftRevision, expectedMemoryRevision: mem0.json?.memoryRevision });
const cfj = cf.json ?? {};
const commitsAfterConfirm = commitsFile();
record('24_confirm_joint_commit', cf, {
  commitId: cfj.commitId,
  memoryRevision: cfj.memoryRevision,
  memoryChangeSetId: cfj.memoryChangeSetId,
  chapterId: cfj.chapter?.id,
  chapterRevision: cfj.chapter?.revision,
  projectChapterCount: listWrap(cfj.project?.chapters).length,
  conflicts: listWrap(cfj.conflicts).length,
  commitCountDelta: commitsAfterConfirm.length - commits0,
  lastCommit: commitsAfterConfirm[commitsAfterConfirm.length - 1],
});

const projAfter = await req('GET', '/api/project');
const chaptersAfter = listWrap(projAfter.json?.project?.chapters ?? projAfter.json?.chapters);
const newCh = chaptersAfter.find((c) => c.id === cfj.chapter?.id);
record('25_project_after_confirm', projAfter, {
  chapterCount: chaptersAfter.length,
  newChapterHasDraftText: (newCh?.content || '').includes(MARKERS.draftTextHead),
  newChapterRevision: newCh?.revision,
});
const memAfterConfirm = await req('GET', '/api/memory');
record('26_memory_after_confirm', memAfterConfirm, { memoryRevision: memAfterConfirm.json?.memoryRevision, counts: memAfterConfirm.json?.counts });

// closed loop: the draft-derived memory and the now-committed chapter must both reach the next prompt
const sseRes = await fetch(BASE + '/api/continue', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ direction: '林砚顺着青玉符的刻痕继续追查旧案。', targetChars: 300, temperature: 0.85, useMemory: true, usePlan: true, useRecall: true }),
});
const sseText = await sseRes.text();
const sseEvents = sseText.split('\n\n').filter(Boolean).map((blk) => {
  const ev = (blk.match(/event: (\S+)/) || [])[1];
  const data = (blk.match(/data: ([\s\S]*)/) || [])[1];
  let parsed = null;
  try { parsed = JSON.parse(data); } catch { parsed = data; }
  return { ev, data: parsed };
});
record('27_continue_after_confirm', { status: sseRes.status, json: {
  meta: sseEvents.find((e) => e.ev === 'meta')?.data,
  done: sseEvents.find((e) => e.ev === 'done')?.data,
} });

// idempotent re-accept of an already applied draft changeset
const reAccept = await req('POST', `/api/memory/changesets/${e2.changeset?.id}/accept`, {});
record('28_idempotent_accept_applied', reAccept, { sameCommit: reAccept.json?.commitId === cfj.commitId, revision: reAccept.json?.memoryRevision });

// ===== G6 B: revise-an-existing-chapter draft, then reject (nothing may land) =====
const d2res = await req('POST', '/api/drafts', { chapterId: ch3.id, title: ch3.title, content: '夜里的县衙外头很静。林砚把青玉符留在袖中，只带了一把短刀。' });
const d2 = d2res.json ?? {};
record('29_draft_revise_existing', d2res, { draftId: d2.id, newChapter: d2.newChapter, baseChapterRevision: d2.baseChapterRevision, baseMemoryRevision: d2.baseMemoryRevision });
record('30_draft_revise_extract', await req('POST', `/api/drafts/${d2.id}/extract`, {}));
const memBeforeReject = await req('GET', '/api/memory');
const commitsBeforeReject = commitsFile().length;
record('31_draft_reject', await req('POST', `/api/drafts/${d2.id}/reject`, { reason: '风格不合，暂不用' }));
const projAfterReject = await req('GET', '/api/project');
const ch3AfterReject = listWrap(projAfterReject.json?.project?.chapters ?? projAfterReject.json?.chapters).find((c) => c.id === ch3.id);
record('32_atomicity_after_reject', memBeforeReject, {
  memoryRevisionUnchanged: (await req('GET', '/api/memory')).json?.memoryRevision === memBeforeReject.json?.memoryRevision,
  commitCountUnchanged: commitsFile().length === commitsBeforeReject,
  chapterRevisionUnchanged: ch3AfterReject?.revision,
  chapterContentStillOriginal: !(ch3AfterReject?.content || '').includes('只带了一把短刀'),
  draftStatus: draftsFile().find((x) => x.id === d2.id)?.status,
});

// ===== G4 x G6: editing the draft-committed chapter must mark the draft-derived memory =====
const healPut = await req('PUT', `/api/chapters/${cfj.chapter?.id}`, { title: newCh?.title ?? '第7章 拓片', content: '雨停了。林砚在桥头站了一会儿，什么也没有带，转身回县衙。' });
record('33_edit_draft_chapter', healPut, { newRevision: healPut.json?.chapter?.revision ?? healPut.json?.revision });
const memHealed = await req('GET', '/api/memory');
const eventsHealed = eventsFile();
record('34_heal_result', memHealed, {
  memoryRevision: memHealed.json?.memoryRevision,
  counts: memHealed.json?.counts,
  safeState: memHealed.json?.safeState,
  draftDerivedMarked: eventsHealed.filter((e) => e.targetChapterId === cfj.chapter?.id || e.sourceRefs?.[0]?.chapterId === cfj.chapter?.id).map((e) => ({ id: e.id, needsReview: e.needsReview, reviewReason: e.reviewReason })),
  commitTypes: commitsFile().map((c) => c.type),
});
const pv = await req('POST', '/api/continue/preview', { direction: '林砚顺着青玉符的刻痕继续追查旧案。', targetChars: 600, useMemory: true, usePlan: true, useRecall: true });
record('35_preview_after_heal', pv, { memoryUsedChars: pv.json?.used?.memories ?? pv.json?.memoryUsedChars, skippedReview: pv.json?.skippedReview, counts: pv.json?.counts, memoryEnabled: pv.json?.memoryEnabled });

// G7 gate must still refuse
record('36_g7_gate', await req('POST', '/api/memory/initialize', { reinitialize: true }));

// prompt capture from the stub log (the model-side view of the loop)
const lines = fs.readFileSync(STUB_LOG, 'utf8').split('\n').filter(Boolean).map((x) => JSON.parse(x));
const calls = lines.map((l) => ({ at: l.at, path: l.path, stream: l.stream, roles: l.roles, chars: l.chars }));
const lastStream = [...lines].reverse().find((l) => l.stream);
const streamBeforeConfirm = lines.filter((l) => l.stream).length;
const userPrompt = lastStream ? (lastStream.messages.find((m) => m.role === 'user')?.content || '') : '';
const promptEvidence = {
  callCount: lines.length,
  streamCallCount: streamBeforeConfirm,
  lastStreamFound: !!lastStream,
  lastStreamUserPromptChars: [...userPrompt].length,
  contains: {
    eventSummary: userPrompt.includes(MARKERS.eventSummary),
    stateValue: userPrompt.includes(MARKERS.stateValue),
    draftTextHead: userPrompt.includes(MARKERS.draftTextHead),
  },
  note: 'draftTextHead can only be true for the continue call made AFTER confirm; step 33 rewrote that chapter, so the final streaming call is post-heal and may drop it. The per-call booleans below are the authoritative record.',
  perCall: lines.filter((l) => l.stream).map((l, idx) => {
    const up = (l.messages ?? []).find((m) => m.role === 'user')?.content || '';
    return { idx, chars: [...up].length, eventSummary: up.includes(MARKERS.eventSummary), stateValue: up.includes(MARKERS.stateValue), draftTextHead: up.includes(MARKERS.draftTextHead) };
  }),
};

const continueCall = lines.filter((l) => l.stream).map((l) => {
  const up = (l.messages ?? []).find((m) => m.role === 'user')?.content || '';
  return { l, up };
}).find((x) => x.up.includes(MARKERS.draftTextHead));
if (continueCall) {
  fs.mkdirSync(OUT_DIR, { recursive: true });
  fs.writeFileSync(`${OUT_DIR}/continue-user-prompt-after-confirm.txt`, continueCall.up);
}

fs.mkdirSync(OUT_DIR, { recursive: true });
fs.writeFileSync(`${OUT_DIR}/e2e-g6-stub-chain-summary.json`, JSON.stringify({ generatedAt: new Date().toISOString(), base: BASE, dataDir: DATA, markers: MARKERS, steps, promptEvidence, calls }, null, 2));
fs.writeFileSync(`${OUT_DIR}/stub-calls.json`, JSON.stringify(calls, null, 2));
console.log('\n' + JSON.stringify({ promptEvidence: { ...promptEvidence, perCall: undefined } }, null, 2));
console.log(JSON.stringify(steps.map((s) => ({ n: s.name, st: s.status })), null, 0));
