// Acceptance driver: real server binary + local stub provider, full chain with NO model key.
// Chain: import -> analyze (LLM call) -> knowledge -> memory/initialize (mapping) -> memory/extract
//        -> changeset accept -> memory counts -> preview/continue injection -> plans generate/activate
//        -> injection of adopted plan -> chapter edit -> self-heal marking -> G7 gate.
// Writes summary + captured prompts to docs/acceptance-evidence/20261024/e2e-stub-chain/.
import fs from 'node:fs';

const BASE = process.env.BASE || 'http://127.0.0.1:8097';
const OUT_DIR = process.env.OUT_DIR || 'docs/acceptance-evidence/20261024/e2e-stub-chain';
const STUB_LOG = process.env.STUB_LOG || 'output/stub-requests.jsonl';
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

async function poll(path, isDone, { timeoutMs = 90000, everyMs = 400 } = {}) {
  const t0 = Date.now();
  let last = null;
  while (Date.now() - t0 < timeoutMs) {
    last = await req('GET', path);
    if (isDone(last.json)) return { ...last, waitedMs: Date.now() - t0 };
    await sleep(everyMs);
  }
  return { ...last, timedOut: true, waitedMs: Date.now() - t0 };
}

const CHAPTERS = [
  ['第1章 值房', '雨落在青石板上。林砚在县衙值房核对三年前的卷宗，灯芯爆了一下。他把抄好的纸叠起来，吹熄了灯。'],
  ['第2章 医者', '沈青禾提着药箱上门，替林砚换掉左臂的旧纱布。她说这伤遇雨会发凉，劝他别再提刀。林砚没有答应。'],
  ['第3章 旧案', '夜里的县衙外头很静。林砚站在槐树下，右手握紧青玉符，左臂旧伤开始发凉。他想起卷宗里被撕去的那一页，也想起沈青禾欲言又止的神情。青玉符上的刻痕像一只没有闭上的眼睛。'],
  ['第4章 卷房', '卷房的门闩是新的。林砚推门进去，发现登记簿少了两页。他把青玉符压在镇纸下面，逐格翻检。'],
  ['第5章 对峙', '有人在廊下等他。林砚停下脚步，对方只留下一句"别再查了"便转身离开。'],
  ['第6章 伤药', '沈青禾把一瓶伤药放在门槛上。林砚拿起瓶子，闻到一股苦味。'],
  ['第7章 河水', '护城河的水涨了。林砚蹲在桥头，看水草贴着石缝摆动。'],
  ['第8章 灯火', '县衙最后一盏灯灭了。林砚把青玉符收回袖中，走进雨里。'],
].map(([title, content]) => `${title}\n${content}`).join('\n\n');

const MARKERS = {
  eventSummary: '林砚在县衙外握紧青玉符',
  stateValue: '左臂旧伤开始发凉',
  threadTitle: '青玉符的来历',
  cognitionContent: '认定青玉符能打开旧案缺口',
  planObjective: '让林砚沿青玉符追查三年前旧案，不提前揭露真凶。',
};

const steps = [];
const record = (name, r, extra = {}) => {
  const item = { name, status: r.status ?? null, error: r.error ?? null, waitedMs: r.waitedMs ?? null, timedOut: r.timedOut ?? false, body: r.json ?? (r.text ? r.text.slice(0, 400) : null), ...extra };
  steps.push(item);
  console.log(`${item.name}: status=${item.status}${item.timedOut ? ' TIMEOUT' : ''}`);
  return item;
};

// 1. config -> stub provider
record('01_put_config', await req('PUT', '/api/config', {
  baseUrl: 'http://127.0.0.1:8098/v1',
  model: 'stub-model',
  apiKey: 'stub-key-not-a-secret',
  contextChars: 12000,
}));

// 2. import
const imp = await req('POST', '/api/import', { title: '桩模型链路测试稿', text: CHAPTERS });
record('02_import', imp, { importedChapters: imp.json?.importedChapters, importedChars: imp.json?.importedChars });

const proj = await req('GET', '/api/project');
const chapters = proj.json?.project?.chapters ?? proj.json?.chapters ?? [];
const ch3 = chapters.find((c) => (c.title || '').includes('3')) ?? chapters[2];
record('03_project_after_import', proj, { chapterCount: chapters.length, targetChapterId: ch3?.id });

// 4. analyze (async, LLM per chapter + synthesis)
record('04_post_analyze', await req('POST', '/api/analyze', {}));
const done = await poll('/api/project', (p) => {
  const a = p?.project?.analysis ?? p?.analysis;
  return a && (a.status === 'ready' || a.status === 'error');
}, { timeoutMs: 120000 });
const analysis = done.json?.project?.analysis ?? done.json?.analysis;
record('05_analysis_finished', done, { analysis });

// 5. knowledge presence (probe: overview text in project analysis or knowledge endpoint)
const know = await req('GET', '/api/knowledge');
record('06_get_knowledge', know);

// 6. memory initialize (mapping only, no LLM)
record('07_post_initialize', await req('POST', '/api/memory/initialize', {}));
const initDone = await poll('/api/memory/initialize/status', (s) => ['ready', 'partial', 'failed'].includes(s?.status), { timeoutMs: 60000 });
record('08_initialize_status', initDone, { initStatus: initDone.json?.status });

const memBefore = await req('GET', '/api/memory');
record('09_memory_after_initialize', memBefore, { counts: memBefore.json?.counts, memoryRevision: memBefore.json?.memoryRevision, safeState: memBefore.json?.safeState });

// 7. deep extraction on the planted chapter only (keeps quotes locatable)
record('10_post_extract', await req('POST', '/api/memory/extract', { chapterIds: [ch3.id] }));
const extDone = await poll('/api/memory/extract/status', (s) => ['done', 'failed', 'partial'].includes(s?.status), { timeoutMs: 90000 });
record('11_extract_status', extDone, { extractStats: extDone.json?.stats ?? extDone.json });

// 8. changesets -> accept
const csList = await req('GET', '/api/memory/changesets?status=proposed');
const listArr = Array.isArray(csList.json) ? csList.json : (Object.values(csList.json ?? {}).find((v) => Array.isArray(v)) ?? []);
record('12_changesets_proposed', csList, { proposedCount: listArr.length, ids: listArr.map((c) => c.id) });
const accepts = [];
for (const cs of listArr) {
  const id = cs.id;
  const detail = await req('GET', `/api/memory/changesets/${id}`);
  const ops = Array.isArray(detail.json?.ops) ? detail.json.ops : (Array.isArray(detail.json?.items) ? detail.json.items : []);
  const a = await req('POST', `/api/memory/changesets/${id}/accept`, {});
  accepts.push({ id, opCount: ops.length, acceptStatus: a.status, result: a.json });
}
record('13_accept_changesets', { status: 200, json: accepts });

const memAfter = await req('GET', '/api/memory');
record('14_memory_after_accept', memAfter, { counts: memAfter.json?.counts, memoryRevision: memAfter.json?.memoryRevision, initStatus: memAfter.json?.initStatus, safeState: memAfter.json?.safeState });

const charMem = await req('GET', '/api/memory/characters/c_lin');
record('15_character_memory', charMem, {
  states: (charMem.json?.states ?? []).length,
  events: (charMem.json?.events ?? []).length,
  cognitions: (charMem.json?.cognitions ?? []).length,
  needsReview: [...(charMem.json?.states ?? []), ...(charMem.json?.events ?? []), ...(charMem.json?.cognitions ?? [])].filter((r) => r.needsReview).length,
});

// 9. preview injection (adopted memory, not fixture)
const preview1 = await req('POST', '/api/continue/preview', { direction: '林砚顺着青玉符的刻痕继续追查旧案。', targetChars: 600, useMemory: true, usePlan: true, useRecall: true });
record('16_preview_after_accept', preview1, {
  memoryEnabled: preview1.json?.memoryEnabled,
  memoryUsedChars: preview1.json?.used?.memories ?? preview1.json?.memoryUsedChars,
  counts: preview1.json?.counts,
  skippedReview: preview1.json?.skippedReview,
  planCount: preview1.json?.planCount,
});

// 10. plans generate + activate (G5)
record('17_post_plans_generate', await req('POST', '/api/plans/generate', { endingTone: '留悬念', volumeCount: 1 }));
const plans = await req('GET', '/api/plans');
const roots = plans.json?.roots ?? [];
const firstId = roots[0]?.plan?.id;
record('18_get_plans', plans, { planCount: plans.json?.count, rootCount: roots.length, firstPlanId: firstId, firstStatus: roots[0]?.plan?.status });
if (firstId) {
  record('19_activate_plan', await req('POST', `/api/plans/${firstId}/activate`, {}));
}

const preview2 = await req('POST', '/api/continue/preview', { direction: '林砚顺着青玉符的刻痕继续追查旧案。', targetChars: 600, useMemory: true, usePlan: true, useRecall: true });
record('20_preview_after_activate', preview2, {
  planCount: preview2.json?.planCount,
  planEnabled: preview2.json?.planEnabled,
  counts: preview2.json?.counts,
  memoryUsedChars: preview2.json?.used?.memories ?? preview2.json?.memoryUsedChars,
});

// 11. real continuation over SSE (stub streams)
const sseRes = await fetch(BASE + '/api/continue', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ direction: '林砚顺着青玉符的刻痕继续追查旧案。', targetChars: 300, temperature: 0.85, useMemory: true, usePlan: true, useRecall: true }),
});
const sseText = await sseRes.text();
const events = sseText.split('\n\n').filter(Boolean).map((blk) => {
  const ev = (blk.match(/event: (\S+)/) || [])[1];
  const data = (blk.match(/data: ([\s\S]*)/) || [])[1];
  let parsed = null;
  try { parsed = JSON.parse(data); } catch { parsed = data; }
  return { ev, data: parsed };
});
const deltas = events.filter((e) => e.ev === 'delta').map((e) => e.data?.text ?? e.data?.content ?? '').join('');
record('21_continue_sse', { status: sseRes.status, json: { eventKinds: [...new Set(events.map((e) => e.ev))], deltaChars: [...deltas].length, meta: events.find((e) => e.ev === 'meta')?.data, done: events.find((e) => e.ev === 'done')?.data } });

// 12. G4 self-heal on the real (adopted) memory: rewrite chapter 3 without the planted evidence
const edited = { title: ch3.title, content: '夜里的县衙外头很静。林砚站在槐树下等雨停，什么也没有带。' };
const putRes = await req('PUT', `/api/chapters/${ch3.id}`, edited);
record('22_edit_chapter3', putRes, { newRevision: putRes.json?.chapter?.revision ?? putRes.json?.revision });
const memHealed = await req('GET', '/api/memory');
record('23_memory_after_heal', memHealed, { memoryRevision: memHealed.json?.memoryRevision, counts: memHealed.json?.counts, safeState: memHealed.json?.safeState });
const preview3 = await req('POST', '/api/continue/preview', { direction: '林砚顺着青玉符的刻痕继续追查旧案。', targetChars: 600, useMemory: true, usePlan: true, useRecall: true });
record('24_preview_after_heal', preview3, { counts: preview3.json?.counts, skippedReview: preview3.json?.skippedReview, planCount: preview3.json?.planCount, memoryUsedChars: preview3.json?.used?.memories ?? preview3.json?.memoryUsedChars });
const charMemHealed = await req('GET', '/api/memory/characters/c_lin');
record('25_character_memory_after_heal', charMemHealed, {
  reviewReasons: [...(charMemHealed.json?.states ?? []), ...(charMemHealed.json?.events ?? []), ...(charMemHealed.json?.cognitions ?? [])]
    .filter((r) => r.needsReview)
    .map((r) => ({ id: r.id, reason: r.reviewReason })),
});

// 13. G7 gate
record('26_g7_gate', await req('POST', '/api/memory/initialize', { reinitialize: true }));

// prompt actually received by the "model" (closed-loop proof)
const lines = fs.readFileSync(STUB_LOG, 'utf8').split('\n').filter(Boolean).map((l) => JSON.parse(l));
const calls = lines.map((l) => ({ at: l.at, path: l.path, stream: l.stream, roles: l.roles, chars: l.chars }));
const continueCall = [...lines].reverse().find((l) => l.stream);
const extractCall = lines.find((l) => !l.stream && (l.messages ?? []).some((m) => (m.content || '').includes(MARKERS.eventSummary) === false && (m.content || '').includes('记忆')));
const userPrompt = continueCall ? (continueCall.messages.find((m) => m.role === 'user')?.content || '') : '';
const sysPrompt = continueCall ? (continueCall.messages.find((m) => m.role === 'system')?.content || '') : '';
const promptEvidence = {
  callCount: lines.length,
  streamCallCount: lines.filter((l) => l.stream).length,
  continueCallFound: !!continueCall,
  continuePromptChars: [...userPrompt].length,
  contains: {
    eventSummary: userPrompt.includes(MARKERS.eventSummary),
    stateValue: userPrompt.includes(MARKERS.stateValue),
    threadTitle: userPrompt.includes(MARKERS.threadTitle),
    planObjective: userPrompt.includes(MARKERS.planObjective),
  },
  note: 'captured prompt is from the LAST streaming call (continuation); earlier streaming calls overwritten by later marks because heal precedes nothing after 21',
};

fs.mkdirSync(OUT_DIR, { recursive: true });
fs.writeFileSync(OUT_DIR + '/e2e-stub-chain-summary.json', JSON.stringify({ generatedAt: new Date().toISOString(), base: BASE, markers: MARKERS, steps, promptEvidence, calls }, null, 2));
if (continueCall) {
  fs.writeFileSync(OUT_DIR + '/continue-system-prompt.txt', sysPrompt);
  fs.writeFileSync(OUT_DIR + '/continue-user-prompt.txt', userPrompt);
}
fs.writeFileSync(OUT_DIR + '/stub-calls.json', JSON.stringify(calls, null, 2));
console.log('\nSUMMARY written to ' + OUT_DIR);
console.log(JSON.stringify({ promptEvidence }, null, 2));
console.log(JSON.stringify({ finalRevision: memHealed.json?.memoryRevision, finalCounts: memHealed.json?.counts, safeState: memHealed.json?.safeState }, null, 2));
