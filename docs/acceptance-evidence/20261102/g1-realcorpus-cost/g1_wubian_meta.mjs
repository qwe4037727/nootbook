import fs from 'node:fs';
// 把桩日志压成「每次调用一行元数据」，避免把 51 万字的书本原文再抄一份进版本库。
const src = process.argv[2] || 'output/wubian-stub-requests.jsonl';
const dst = process.argv[3] || 'output/wubian-stub-requests.meta.jsonl';
const han = (t) => (t.match(/[㐀-䶿一-鿿豈-﫿]/g) || []).length;
const lines = fs.readFileSync(src, 'utf8').split('\n').filter(Boolean);
const out = [];
for (const l of lines) {
  const j = JSON.parse(l);
  const user = j.messages.map((m) => m.content).join('\n');
  const sys = j.messages.find((m) => m.role === 'system');
  const digestStart = user.indexOf('【已有知识摘要');
  const bodyStart = user.indexOf('【本章正文】');
  out.push(JSON.stringify({
    at: j.at, path: j.path, model: j.model, stream: j.stream, temperature: j.temperature,
    roles: j.roles, inputChars: user.length, hanChars: han(user),
    kind: bodyStart >= 0 ? 'analyst' : 'synthesis_or_other',
    chapter: (user.match(/【本章】第 (\d+) 章/) || [])[1] ?? null,
    chapterTitle: (user.match(/【本章】第 \d+ 章 (.*)/) || [])[1] ?? null,
    clippedChapter: user.includes('本章过长'),
    digestSectionChars: digestStart >= 0 && bodyStart > digestStart ? bodyStart - digestStart : 0,
    systemChars: sys ? sys.content.length : 0,
    bodyChars: bodyStart >= 0 ? user.length - bodyStart : 0,
  }));
}
fs.writeFileSync(dst, out.join('\n') + '\n');
console.log('lines:', out.length, 'bytes:', fs.statSync(dst).size);
