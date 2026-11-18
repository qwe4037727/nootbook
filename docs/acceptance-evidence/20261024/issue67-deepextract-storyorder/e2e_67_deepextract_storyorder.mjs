// issues #67 未测边界的实测：深度提取采纳（changeset accept）写入的新事件是否也留 storyOrder=0，
// 以及重启时的 reconcile 是否把它修成正确章号（与草稿联合提交路径同机制？）
// 只读原目录 → 复制隔离副本 → 起真实进程 → 读数 → 硬杀。零产品代码改动，无模型 Key。
import fs from 'node:fs';
import { spawn } from 'node:child_process';

const SRC = 'output/e2e-stub-data';
const DIR = 'output/storyorder-probe-data';
const PORT = process.env.PORT || '8097';
const BIN = './output/novelagent-storyorder.exe';

const readJSON = (p) => JSON.parse(fs.readFileSync(p, 'utf8'));
const snap = (dir) => {
  const events = readJSON(`${dir}/memory/events.json`);
  const commits = fs.existsSync(`${dir}/memory/commits.json`) ? readJSON(`${dir}/memory/commits.json`) : [];
  const meta = readJSON(`${dir}/memory/meta.json`);
  return {
    revision: meta.memoryRevision,
    commits: commits.map((c) => ({ id: c.id, type: c.type, basedOn: c.basedOn, target: c.targetRevision, status: c.status })),
    events: events.map((e) => ({
      id: e.id,
      storyOrder: e.storyOrder,
      chapterId: e.chapterId,
      origin: e.origin && e.origin.type,
      status: e.status,
      needsReview: !!e.needsReview,
      reviewReason: e.reviewReason || '',
      recordVersion: e.recordVersion,
      summary: e.summary,
    })),
  };
};

const chapterOrder = (() => {
  const proj = readJSON(`${SRC}/project.json`);
  return proj;
})();

fs.rmSync(DIR, { recursive: true, force: true });
fs.cpSync(SRC, DIR, { recursive: true });
const before = snap(DIR);

const child = spawn(BIN, ['-addr', `127.0.0.1:${PORT}`, '-data', DIR], { stdio: ['ignore', 'ignore', 'pipe'] });
let stderr = '';
child.stderr.on('data', (d) => (stderr += d.toString()));

const wait = (ms) => new Promise((r) => setTimeout(r, ms));
async function get(path) {
  const res = await fetch(`http://127.0.0.1:${PORT}${path}`);
  return { status: res.status, body: await res.json() };
}

let api = null;
for (let i = 0; i < 60; i++) {
  await wait(500);
  try {
    api = await get('/api/memory');
    if (api.status === 200) break;
  } catch {}
  api = null;
}
fs.writeFileSync('output/storyorder-api-memory.json', JSON.stringify(api, null, 2));

// 顺带取一次注入读数，看这条事件在修复前后各自怎么渲染（先带 needsReview，通常不注入）
let preview = null;
try {
  const res = await fetch(`http://127.0.0.1:${PORT}/api/continue/preview`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ workId: 'work_001', direction: '林砚带着青玉符离开县衙', targetChars: 600, contextChars: 12000 }),
  });
  preview = await res.json();
} catch (e) {
  preview = { error: String(e) };
}
fs.writeFileSync('output/storyorder-preview.json', JSON.stringify(preview, null, 2));

const pid = child.pid;
child.kill('SIGKILL');
try {
  spawn('taskkill', ['/T', '/F', '/PID', String(pid)], { stdio: 'ignore' });
} catch {}
await wait(1200);

const after = snap(DIR);
const deep = (s) => s.events.filter((e) => e.origin === 'deep_extract');
const summary = {
  sourceDirUntouched: true,
  probeDir: DIR,
  port: PORT,
  binary: BIN,
  chaptersInProject: Array.isArray(chapterOrder.chapters) ? chapterOrder.chapters.length : 'n/a',
  before: { revision: before.revision, commits: before.commits.length, deepExtract: deep(before) },
  after: { revision: after.revision, commits: after.commits.length, deepExtract: deep(after) },
  newCommits: after.commits.slice(before.commits.length),
  apiMemoryKeys: api ? Object.keys(api.body) : null,
  apiStoryOrders: api ? storyOrdersFromApi(api.body) : null,
  previewMeta: preview
    ? {
        memoryEnabled: preview.memoryEnabled,
        memoryBudgetChars: preview.memoryBudgetChars,
        used: preview.used,
        records: (preview.records || []).length,
        memoryRevision: preview.memoryRevision,
        droppedByBudget: preview.droppedByBudget,
        skippedReview: preview.skippedReview,
      }
    : null,
  stderr: stderr.slice(0, 400),
};
function storyOrdersFromApi(body) {
  const out = {};
  for (const [k, v] of Object.entries(body || {})) {
    if (Array.isArray(v)) {
      for (const item of v) {
        if (item && item.id && 'storyOrder' in item) out[item.id] = item.storyOrder;
      }
    }
  }
  return out;
}
fs.writeFileSync('output/storyorder-summary.json', JSON.stringify({ ...summary, afterFull: after }, null, 2));
console.log(JSON.stringify(summary, null, 2));
