// #67 追加两项：①重启对账是否幂等（第二次开进程不再 +1 revision）；②storyOrder 在接口侧能否读到（只经 /api/memory/characters/{id} 透出）
import fs from 'node:fs';
import { spawn } from 'node:child_process';

const DIR = 'output/storyorder-probe-data';
const PORT = '8097';
const CHAR = 'c_45301ad360063196d5';
const readJSON = (p) => JSON.parse(fs.readFileSync(p, 'utf8'));
const wait = (ms) => new Promise((r) => setTimeout(r, ms));

const pre = (() => {
  const commits = readJSON(`${DIR}/memory/commits.json`);
  const events = readJSON(`${DIR}/memory/events.json`);
  return {
    revision: readJSON(`${DIR}/memory/meta.json`).memoryRevision,
    commits: commits.length,
    storyOrder: events.find((e) => e.id === 'evt_941aad80d53ae04ebd').storyOrder,
    lastCommit: commits[commits.length - 1].id,
  };
})();

const child = spawn('./output/novelagent-storyorder.exe', ['-addr', `127.0.0.1:${PORT}`, '-data', DIR], { stdio: ['ignore', 'ignore', 'pipe'] });
let stderr = '';
child.stderr.on('data', (d) => (stderr += d.toString()));

async function get(path) {
  const res = await fetch(`http://127.0.0.1:${PORT}${path}`);
  return { status: res.status, body: await res.json().catch(() => null) };
}
let api = null;
for (let i = 0; i < 60; i++) {
  await wait(400);
  try {
    api = await get('/api/memory');
    if (api.status === 200) break;
  } catch {}
  api = null;
}
const view = api ? await get(`/api/memory/characters/${CHAR}`) : null;
const evts = (view && view.body && view.body.events) || [];
const seen = evts.find((e) => e.id === 'evt_941aad80d53ae04ebd') || null;

const pid = child.pid;
child.kill('SIGKILL');
try { spawn('taskkill', ['/T', '/F', '/PID', String(pid)], { stdio: 'ignore' }); } catch {}
await wait(1200);

const post = (() => {
  const commits = readJSON(`${DIR}/memory/commits.json`);
  return {
    revision: readJSON(`${DIR}/memory/meta.json`).memoryRevision,
    commits: commits.length,
    storyOrder: readJSON(`${DIR}/memory/events.json`).find((e) => e.id === 'evt_941aad80d53ae04ebd').storyOrder,
    lastCommit: commits[commits.length - 1].id,
  };
})();

const summary = {
  secondOpenIsIdempotent: pre.revision === post.revision && pre.commits === post.commits && pre.lastCommit === post.lastCommit,
  pre,
  post,
  apiMemoryRevision: api && api.body.memoryRevision,
  apiSafeState: api && api.body.safeState,
  characterViewStatus: view && view.status,
  storyOrderViaAPI: seen && seen.storyOrder,
  allEventViewsViaAPI: evts.map((e) => `${e.id}:${e.storyOrder}:${e.status}`),
  stderr: stderr.slice(0, 300),
};
fs.writeFileSync('output/storyorder-second-open.json', JSON.stringify(summary, null, 2));
console.log(JSON.stringify(summary, null, 2));
