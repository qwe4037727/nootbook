// G2 真实语料规模：在已分析完的隔离目录上跑 记忆初始化 + 全书 79 章深度提取，抓每次真实请求体量。
import fs from 'node:fs';
import { spawn, execSync } from 'node:child_process';

const SERVER_PORT = Number(process.env.SERVER_PORT || 8094);
const STUB_PORT = Number(process.env.STUB_PORT || 8093);
const DATA = 'output/wubian-cost-data';
const STUB_LOG = 'output/wubian-g2-stub-requests.jsonl';
const BASE = `http://127.0.0.1:${SERVER_PORT}`;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const stub = spawn(process.execPath, ['output/stub_provider.mjs'], {
  env: { ...process.env, STUB_PORT: String(STUB_PORT), STUB_LOG }, stdio: 'ignore',
});
await sleep(400);
const server = spawn('./output/novelagent-wubian.exe', ['-addr', `127.0.0.1:${SERVER_PORT}`, '-data', DATA], {
  stdio: ['ignore', fs.openSync('output/wubian-g2-server.log', 'w'), fs.openSync('output/wubian-g2-server.err', 'w')],
});

const out = { serverPort: SERVER_PORT, stubPort: STUB_PORT };
try {
  let ready = false;
  for (let i = 0; i < 50 && !ready; i++) {
    await sleep(500);
    try { ready = (await fetch(`${BASE}/api/config`)).status < 500; } catch { ready = false; }
  }
  out.serverReady = ready;
  if (!ready) out.serverErr = fs.readFileSync('output/wubian-g2-server.err', 'utf8').slice(-800);

  const proj = await (await fetch(`${BASE}/api/project`)).json();
  const chapters = proj?.project?.chapters ?? proj?.chapters ?? [];
  out.chapterCount = chapters.length;
  out.analysisBefore = (proj?.project?.analysis ?? proj?.analysis)?.status ?? null;

  // 1) 记忆初始化（纯映射，不调模型）
  out.initializePost = (await fetch(`${BASE}/api/memory/initialize`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' })).status;
  let init = null;
  for (let i = 0; i < 120; i++) {
    init = await (await fetch(`${BASE}/api/memory/initialize/status`)).json();
    if (['ready', 'partial', 'failed'].includes(init?.status)) break;
    await sleep(300);
  }
  out.initializeStatus = init?.status ?? null;
  const mem1 = await (await fetch(`${BASE}/api/memory`)).json();
  out.memoryAfterInitialize = { counts: mem1?.counts, revision: mem1?.memoryRevision, safeState: mem1?.safeState };

  // 2) 全书深度提取（每章一次模型调用）
  const ids = chapters.map((c) => c.id).filter(Boolean);
  out.extractTargetChapters = ids.length;
  const ex = await fetch(`${BASE}/api/memory/extract`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ chapterIds: ids }),
  });
  out.extractPost = ex.status; out.extractPostBody = await ex.text();
  let ext = null;
  for (let i = 0; i < 600; i++) {
    ext = await (await fetch(`${BASE}/api/memory/extract/status`)).json();
    if (['done', 'failed', 'partial'].includes(ext?.status)) break;
    await sleep(500);
  }
  out.extractStatus = ext ?? null;

  const cs = await (await fetch(`${BASE}/api/memory/changesets?status=proposed`)).json();
  const arr = Array.isArray(cs) ? cs : (Object.values(cs ?? {}).find((v) => Array.isArray(v)) ?? []);
  out.proposedChangesets = arr.length;
  const mem2 = await (await fetch(`${BASE}/api/memory`)).json();
  out.memoryAfterExtract = { counts: mem2?.counts, revision: mem2?.memoryRevision, safeState: mem2?.safeState };
} catch (e) {
  out.fatal = String(e && e.stack ? e.stack : e);
  try { out.serverErrTail = fs.readFileSync('output/wubian-g2-server.err', 'utf8').slice(-1500); } catch {}
} finally {
  server.kill(); stub.kill();
  await sleep(300);
  try { execSync('taskkill //F //IM novelagent-wubian.exe', { stdio: 'ignore' }); } catch {}
}
fs.writeFileSync('output/wubian-g2-summary.json', JSON.stringify(out, null, 2));
console.log(JSON.stringify(out, null, 2).slice(0, 2500));
