// Follow-up probe on the same G6 data dir: the POSITIVE joint commit (extract -> confirm with NO
// edit in between), then G4 heal on draft-derived memory, plus resurrection attempts on the
// changeset that the earlier edit had invalidated.
import fs from 'node:fs';

const BASE = process.env.BASE || 'http://127.0.0.1:8097';
const DATA = process.env.DATA || 'output/e2e-g6-data';
const OUT_DIR = process.env.OUT_DIR || 'docs/acceptance-evidence/20261024/e2e-g6-stub-chain';
const STUB_LOG = process.env.STUB_LOG || 'output/stub-requests-g6.jsonl';

async function req(method, path, body) {
  const res = await fetch(BASE + path, { method, headers: { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) });
  const text = await res.text();
  let json = null;
  try { json = JSON.parse(text); } catch {}
  return { method, path, status: res.status, json, text };
}
const steps = [];
function record(name, r, extra = {}) {
  const item = { name, status: r.status ?? null, body: r.json ?? null, ...extra };
  steps.push(item);
  console.log(`${name}: status=${item.status}`);
  return item;
}
const readJson = (p) => { try { return JSON.parse(fs.readFileSync(p, 'utf8')); } catch { return null; } };
const arr = (v) => (Array.isArray(v) ? v : listFirst(v));
function listFirst(v) { const f = Object.values(v ?? {}).find((x) => Array.isArray(x)); return Array.isArray(f) ? f : []; }
const commits = () => arr(readJson(`${DATA}/memory/commits.json`));
const events = () => arr(readJson(`${DATA}/memory/events.json`));
const threads = () => arr(readJson(`${DATA}/memory/threads.json`));

const logBefore = fs.readFileSync(STUB_LOG, 'utf8').split('\n').filter(Boolean).length;

// baseline
const mem0 = await req('GET', '/api/memory');
const proj0 = await req('GET', '/api/project');
const commits0 = commits().length;
record('A00_baseline', mem0, { counts: mem0.json?.counts, memoryRevision: mem0.json?.memoryRevision, safeState: mem0.json?.safeState, chapterCount: arr(proj0.json?.project?.chapters ?? proj0.json?.chapters).length, commitCount: commits0 });

// A: positive joint commit, no edit between extract and confirm
const dres = await req('POST', '/api/drafts', { title: '第8章 封条', content: '封条印在灯下显出半个字。林砚用指甲抠了抠，握紧青玉符对准拓片的缺口，左臂旧伤开始发凉。沈青禾说这印属于三年前那桩案子。' });
const d = dres.json ?? {};
record('A01_draft_create', dres, { draftId: d.id, targetChapterId: d.targetChapterId, newChapter: d.newChapter, baseMemoryRevision: d.baseMemoryRevision, draftRevision: d.draftRevision });
const eres = await req('POST', `/api/drafts/${d.id}/extract`, {});
const e = eres.json ?? {};
record('A02_draft_extract', eres, {
  changeSetId: e.changeset?.id, csDraftRevision: e.changeset?.draftRevision, csBaseMemory: e.changeset?.basedOnMemoryRevision,
  ops: listWrapOps(e.changeset?.ops), draftChangeSetId: e.draft?.memoryChangeSetId,
  recordIds: listWrapOps(e.changeset?.ops).map((o) => o.record?.id),
});
const cres = await req('POST', `/api/drafts/${d.id}/confirm`, { draftRevision: d.draftRevision, expectedMemoryRevision: mem0.json?.memoryRevision });
const c = cres.json ?? {};
record('A03_confirm_joint', cres, {
  commitId: c.commitId, memoryRevision: c.memoryRevision, memoryChangeSetId: c.memoryChangeSetId,
  chapterId: c.chapter?.id, chapterRevision: c.chapter?.revision, conflicts: listWrapOps(c.conflicts).length,
  projectChapterCount: arr(c.project?.chapters).length,
});
const memA = await req('GET', '/api/memory');
record('A04_memory_after_joint', memA, { counts: memA.json?.counts, memoryRevision: memA.json?.memoryRevision });
record('A05_commit_record', { status: 200, json: {
  commitCountDelta: commits().length - commits0,
  lastCommitType: commits()[commits().length - 1]?.type,
  lastCommitPayloadKeys: Object.keys(commits()[commits().length - 1]?.payload ?? {}),
  basedOn: commits()[commits().length - 1]?.basedOnMemoryRevision,
  target: commits()[commits().length - 1]?.targetMemoryRevision,
} });
record('A06_proposed_changesets_left', await req('GET', '/api/memory/changesets?status=proposed'), {});
const dFinal = await req('GET', `/api/drafts/${d.id}`);
record('A07_draft_final', dFinal, { draftStatus: dFinal.json?.status, changeSetId: dFinal.json?.memoryChangeSetId, commitId: dFinal.json?.commitId, chapterRevision: dFinal.json?.chapterRevision });

// B: does the draft-derived memory reach the next model call? (closed loop through G6)
const sse = await fetch(BASE + '/api/continue', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ direction: '林砚核对封条印的来历。', targetChars: 300, temperature: 0.85, useMemory: true, usePlan: true, useRecall: true }) });
const sseText = await sse.text();
const meta = (sseText.match(/event: meta\ndata: ([\s\S]*?)\n\n/) || [])[1];
record('B01_continue_meta', { status: sse.status, json: meta ? JSON.parse(meta) : null });

// C: G4 heal on the draft-derived records (remove the planted quotes from the committed chapter)
const chapterId = c.chapter?.id;
const putRes = await req('PUT', `/api/chapters/${chapterId}`, { title: '第8章 封条', content: '封条印在灯下看不清。林砚站了一会儿就走了，沈青禾没有说话。' });
record('C01_edit_draft_chapter', putRes, { newRevision: putRes.json?.chapter?.revision ?? putRes.json?.revision });
const memC = await req('GET', '/api/memory');
record('C02_memory_after_heal', memC, { memoryRevision: memC.json?.memoryRevision, counts: memC.json?.counts, safeState: memC.json?.safeState });
record('C03_draft_derived_marks', { status: 200, json: {
  events: events().filter((x) => (x.sourceRefs?.[0]?.chapterId === chapterId)).map((x) => ({ id: x.id, needsReview: x.needsReview, reason: x.reviewReason, recordVersion: x.recordVersion })),
  threads: threads().filter((x) => (x.sourceRefs?.[0]?.chapterId === chapterId || x.introducedByEvent?.chapterId === chapterId)).map((x) => ({ id: x.id, needsReview: x.needsReview, reason: x.reviewReason })),
  commitTypes: commits().map((x) => x.type),
  lastCommitPayloadKeys: Object.keys(commits()[commits().length - 1]?.payload ?? {}),
} });
const pv = await req('POST', '/api/continue/preview', { direction: '林砚核对封条印的来历。', targetChars: 600, useMemory: true, usePlan: true, useRecall: true });
record('C04_preview_after_heal', pv, { memoryEnabled: pv.json?.memoryEnabled, memoryUsedChars: pv.json?.used?.memories ?? pv.json?.memoryUsedChars, skippedReview: pv.json?.skippedReview, counts: pv.json?.counts });

// D: resurrection attempts on the changeset invalidated by the earlier edit round
const ORPHAN = 'mcs_0c69ff6c1d29990f95';
record('D01_accept_rejected_orphan', await req('POST', `/api/memory/changesets/${ORPHAN}/accept`, {}));
record('D02_rebase_rejected_orphan', await req('POST', `/api/memory/changesets/${ORPHAN}/rebase`, {}));
record('D03_reject_again', await req('POST', `/api/memory/changesets/${ORPHAN}/reject`, { reason: 'probe' }));

// prompt capture for the positive-confirm call
const lines = fs.readFileSync(STUB_LOG, 'utf8').split('\n').filter(Boolean).map((x) => JSON.parse(x)).slice(logBefore);
const streamCalls = lines.filter((l) => l.stream).map((l) => {
  const up = (l.messages ?? []).find((m) => m.role === 'user')?.content || '';
  return { at: l.at, chars: [...up].length, hasDraftEvent: up.includes('林砚在县衙外握紧青玉符'), hasNewChapterText: up.includes('封条印在灯下'), hasPatchedDraftChapter: up.includes('雨停之前，林砚在桥头握紧青玉符'), prompt: up };
});
const withDraft = streamCalls.find((x) => x.hasDraftEvent || x.hasNewChapterText);
if (withDraft) fs.writeFileSync(`${OUT_DIR}/continue-user-prompt-after-joint-confirm.txt`, withDraft.prompt);
const promptEvidence = { newCalls: lines.length, streamCalls: streamCalls.map(({ prompt, ...rest }) => rest), wrotePromptFile: !!withDraft };

fs.writeFileSync(`${OUT_DIR}/g6-confirm-probe.json`, JSON.stringify({ generatedAt: new Date().toISOString(), steps, promptEvidence }, null, 2));
console.log('\n' + JSON.stringify(promptEvidence, null, 2));

function listWrapOps(v) { return Array.isArray(v) ? v : []; }
