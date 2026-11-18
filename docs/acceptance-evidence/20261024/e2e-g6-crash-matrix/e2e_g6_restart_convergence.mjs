// Does restarting the SAME organically built data dir converge, or does every start append another
// reconcile commit? Decides whether a byte-stable golden baseline for the recovery matrix is possible.
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { spawn, spawnSync } from 'node:child_process';

const BIN = process.env.BIN || 'output/novelagent-g6.exe';
const PORT = Number(process.env.PORT || 8097);
const DIR = process.env.DIR || 'output/e2e-g6-rec-base';
const API = `http://127.0.0.1:${PORT}`;
const ROUNDS = Number(process.env.ROUNDS || 4);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const sha = (p) => { try { return crypto.createHash('sha256').update(fs.readFileSync(p)).digest('hex').slice(0, 16); } catch { return null; } };
const readJson = (p) => { try { return JSON.parse(fs.readFileSync(p, 'utf8')); } catch { return null; } };
const arr = (v) => (Array.isArray(v) ? v : Object.values(v ?? {}).find((x) => Array.isArray(x)) ?? []);

let child = null;
function start() { child = spawn(BIN, ['-addr', `127.0.0.1:${PORT}`, '-data', DIR], { windowsHide: true }); }
async function up() { for (let i = 0; i < 120; i++) { try { const r = await fetch(`${API}/api/memory`); if (r.ok) { await r.text(); return; } } catch {} await sleep(250); } throw new Error('no up'); }
async function analysis() { try { const r = await fetch(`${API}/api/project`); const j = await r.json(); return j?.project?.analysis; } catch { return null; } }
async function waitAnalysis() { for (let i = 0; i < 90; i++) { const a = await analysis(); if (a && (a.status === 'ready' || a.status === 'error' || a.status === 'idle')) return a; await sleep(500); } return { status: 'timeout' }; }
function stop() { if (!child) return; spawnSync('taskkill', ['/PID', String(child.pid), '/T', '/F'], { windowsHide: true }); child = null; }
const get = async (p) => { const r = await fetch(API + p); const t = await r.text(); try { return JSON.parse(t); } catch { return { raw: t.slice(0, 200) }; } };

const files = ['memory/events.json', 'memory/cognitions.json', 'memory/threads.json', 'memory/states.json', 'memory/meta.json', 'memory/commits.json', 'project.json', 'knowledge.json'];
const rows = [];
for (let i = 0; i < ROUNDS; i++) {
  const pre = { round: i, sha: Object.fromEntries(files.map((f) => [f, sha(path.join(DIR, f))])) };
  const commitsPre = arr(readJson(path.join(DIR, 'memory/commits.json')));
  start(); await up();
  const mem = await get('/api/memory');
  const an = await waitAnalysis();
  const mem2 = await get('/api/memory');
  stop();
  const post = Object.fromEntries(files.map((f) => [f, sha(path.join(DIR, f))]));
  const commitsPost = arr(readJson(path.join(DIR, 'memory/commits.json')));
  const added = commitsPost.slice(commitsPre.length).map((c) => ({ id: c.commitId, type: c.type, status: c.status, basedOn: c.basedOnMemoryRevision, target: c.targetMemoryRevision, payload: Object.keys(c.payload ?? {}) }));
  const changed = files.filter((f) => pre.sha[f] !== post[f]);
  rows.push({ round: i, apiRevision: mem?.memoryRevision, apiRevisionAfterAnalysis: mem2?.memoryRevision, counts: mem2?.counts, safeState: mem2?.safeState, analysis: an?.status, commitDelta: added, changedFiles: changed });
  console.log(JSON.stringify(rows[rows.length - 1]));
}
fs.writeFileSync('output/e2e-g6-restart-convergence.json', JSON.stringify({ at: new Date().toISOString(), dir: DIR, rounds: ROUNDS, rows }, null, 2), 'utf8');
const revs = rows.map((r) => r.apiRevision);
console.log(`\nrevision per restart start: ${revs.join(' -> ')}`);
console.log(`converged (no new commits in last round): ${rows[rows.length - 1].commitDelta.length === 0 && rows[rows.length - 1].changedFiles.length <= 2}`);
