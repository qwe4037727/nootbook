// G1 真实语料成本投影（零模型 Key）：
// 真实《无边悔恨》→ 产品自己的导入/分章 → 产品自己的分析链路 → 本地桩记录每一次真实请求体
// → 用产品自己的 token 口径计量 → 按 DeepSeek 官方现价计价。
import fs from 'node:fs';
import { spawn, execSync } from 'node:child_process';

const SERVER_PORT = Number(process.env.SERVER_PORT || 8094);
const STUB_PORT = Number(process.env.STUB_PORT || 8093);
const DATA = 'output/wubian-cost-data';
const STUB_LOG = 'output/wubian-stub-requests.jsonl';
const BASE = `http://127.0.0.1:${SERVER_PORT}`;
const SUMMARY = 'output/wubian-cost-summary.json';
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function ensure() {
  try { execSync(`rm -rf ${DATA}`, { stdio: 'ignore' }); } catch {}
  fs.mkdirSync(DATA, { recursive: true });
  execSync('go build -o output/novelagent-wubian.exe .', { stdio: 'inherit' });
}

async function req(method, path, body) {
  const res = await fetch(BASE + path, {
    method, headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  let json = null; try { json = JSON.parse(text); } catch {}
  return { status: res.status, json, text };
}

async function poll(path, isDone, timeoutMs = 180000) {
  const t0 = Date.now(); let last = null;
  while (Date.now() - t0 < timeoutMs) {
    last = await req('GET', path);
    if (isDone(last.json)) return { ...last, waitedMs: Date.now() - t0 };
    await sleep(500);
  }
  return { ...last, timedOut: true, waitedMs: Date.now() - t0 };
}

// 与 internal/eval/token.go 同口径的估算：汉字 1 rune≈1 token，ASCII 字母数字 4 rune≈1 token，
// 其它符号 1 token，连续空白 1 token。
function estimateTokens(text) {
  let total = 0, asciiRun = 0, spacePending = false;
  const flushASCII = () => { if (asciiRun > 0) { total += Math.ceil(asciiRun / 4); asciiRun = 0; } };
  const flushSpace = () => { if (spacePending) { total++; spacePending = false; } };
  for (const ch of text) {
    if (/\s/.test(ch)) { flushASCII(); spacePending = true; continue; }
    flushSpace();
    if (/[\u3400-\u4DBF\u4E00-\u9FFF\uF900-\uFAFF]/.test(ch)) { flushASCII(); total++; }
    else if (ch.charCodeAt(0) <= 0x7f && /[A-Za-z0-9]/.test(ch)) { asciiRun++; }
    else { flushASCII(); total++; }
  }
  flushASCII(); flushSpace();
  return total;
}

ensure();
const stub = spawn(process.execPath, ['output/stub_provider.mjs'], {
  env: { ...process.env, STUB_PORT: String(STUB_PORT), STUB_LOG }, stdio: 'ignore',
});
await sleep(400);
const server = spawn('./output/novelagent-wubian.exe', ['-addr', `127.0.0.1:${SERVER_PORT}`, '-data', DATA], { stdio: ['ignore', fs.openSync('output/wubian-server.log', 'w'), fs.openSync('output/wubian-server.err', 'w')] });

const out = { startedAt: new Date().toISOString(), serverPort: SERVER_PORT, stubPort: STUB_PORT };
try {
  // 等服务就绪（最多 25s），失败时把 stderr 带出来
  let ready = false;
  for (let i = 0; i < 50 && !ready; i++) {
    await sleep(500);
    try {
      const r = await fetch(`${BASE}/api/config`);
      ready = r.status < 500;
    } catch { ready = false; }
  }
  out.serverReady = ready;
  if (!ready) out.serverErr = fs.readFileSync('output/wubian-server.err', 'utf8').slice(-800);
  out.config = (await req('PUT', '/api/config', {
    baseUrl: `http://127.0.0.1:${STUB_PORT}/v1`,
    model: 'deepseek-flash',
    apiKey: 'stub-key-not-a-secret',
    contextChars: 12000,
  })).status;

  const raw = fs.readFileSync('corpus/无边悔恨.txt', 'utf8');
  out.corpusBytes = Buffer.byteLength(raw);
  const imp = await req('POST', '/api/import', { title: '无边悔恨', text: raw });
  out.import = { status: imp.status, importedChapters: imp.json?.importedChapters, importedChars: imp.json?.importedChars };

  const before = (await req('GET', '/api/project')).json;
  out.chapterLensBefore = (before?.chapters || []).map((c) => c.charCount ?? c.Chapters?.length ?? 0);
  out.analysisBefore = before?.analysis?.status ?? null;

  out.analyzePost = (await req('POST', '/api/analyze')).status;
  const done = await poll('/api/project', (j) => j?.analysis?.status === 'done' || j?.analysis?.status === 'error');
  out.analysisAfter = done.json?.analysis ?? null;
  out.analysisWaitedMs = done.waitedMs;
  out.analysisTimedOut = !!done.timedOut;
  out.knowledgeCounts = (() => {
    const k = done.json?.knowledge;
    return k ? { characters: k.characters?.length, world: k.world?.length, summaries: k.summaries?.length } : null;
  })();
} catch (e) {
  out.fatal = String(e && e.stack ? e.stack : e);
  try { out.serverErrTail = fs.readFileSync('output/wubian-server.err', 'utf8').slice(-1500); } catch {}
} finally {
  server.kill(); stub.kill();
  await sleep(300);
  try { execSync(`taskkill //F //IM novelagent-wubian.exe`, { stdio: 'ignore' }); } catch {}
}

// ---- 从桩日志还原真实请求 ----
const lines = fs.readFileSync(STUB_LOG, 'utf8').split('\n').filter(Boolean).map((l) => JSON.parse(l));
const calls = lines.map((l) => {
  const user = l.messages.map((m) => m.content).join('\n');
  const isAnalyst = user.includes('【本章正文】');
  const digestStart = user.indexOf('【已有知识摘要');
  const bodyStart = user.indexOf('【本章正文】');
  const digestChars = digestStart >= 0 && bodyStart > digestStart ? bodyStart - digestStart : 0;
  return {
    path: l.path, model: l.model, roles: l.roles.join(','),
    inputChars: user.length, inputTokens: estimateTokens(user),
    kind: isAnalyst ? 'analyst' : (user.includes('已有总览') || user.includes('章节摘要') ? 'synthesis' : 'other'),
    chapterLabel: (user.match(/【本章】第 (\d+) 章/) || [])[1] ?? null,
    clipped: user.includes('本章过长'),
    digestChars,
    systemChars: (l.messages.find((m) => m.role === 'system') || {}).content?.length ?? 0,
  };
});
out.callCount = calls.length;
out.byKind = {};
for (const c of calls) {
  const b = (out.byKind[c.kind] ||= { calls: 0, inputTokens: 0, inputChars: 0, clipped: 0, digestCharsMax: 0, digestCharsMin: Infinity, tokensMin: Infinity, tokensMax: 0 });
  b.calls++; b.inputTokens += c.inputTokens; b.inputChars += c.inputChars;
  if (c.clipped) b.clipped++;
  b.digestCharsMax = Math.max(b.digestCharsMax, c.digestChars);
  b.digestCharsMin = Math.min(b.digestCharsMin, c.digestChars);
  b.tokensMin = Math.min(b.tokensMin, c.inputTokens); b.tokensMax = Math.max(b.tokensMax, c.inputTokens);
}
if (out.byKind.analyst) out.byKind.analyst.digestCharsMin = out.byKind.analyst.digestCharsMin === Infinity ? 0 : out.byKind.analyst.digestCharsMin;
out.totalInputTokens = calls.reduce((s, c) => s + c.inputTokens, 0);
out.perCallSample = calls.slice(0, 3).concat(calls.slice(-2));
out.clippedChapters = calls.filter((c) => c.clipped).map((c) => c.chapterLabel);
out.systemPromptCharsAnalyst = (calls.find((c) => c.kind === 'analyst') || {}).systemChars ?? 0;
fs.writeFileSync(SUMMARY, JSON.stringify(out, null, 2));
console.log(JSON.stringify({ callCount: out.callCount, byKind: out.byKind, totalInputTokens: out.totalInputTokens, analysis: out.analysisAfter, clipped: out.clippedChapters.length }, null, 2));
