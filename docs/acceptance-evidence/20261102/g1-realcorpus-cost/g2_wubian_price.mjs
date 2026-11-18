import fs from 'node:fs';
function estimateTokens(text) {
  let total = 0, asciiRun = 0, spacePending = false;
  const flushASCII = () => { if (asciiRun > 0) { total += Math.ceil(asciiRun / 4); asciiRun = 0; } };
  const flushSpace = () => { if (spacePending) { total++; spacePending = false; } };
  for (const ch of text) {
    if (/\s/.test(ch)) { flushASCII(); spacePending = true; continue; }
    flushSpace();
    if (/[㐀-䶿一-鿿豈-﫿]/.test(ch)) { flushASCII(); total++; }
    else if (ch.charCodeAt(0) <= 0x7f && /[A-Za-z0-9]/.test(ch)) { asciiRun++; }
    else { flushASCII(); total++; }
  }
  flushASCII(); flushSpace();
  return total;
}
const lines = fs.readFileSync('output/wubian-g2-stub-requests.jsonl', 'utf8').split('\n').filter(Boolean).map((l) => JSON.parse(l));
const calls = lines.map((l) => {
  const user = l.messages.map((m) => m.content).join('\n');
  return { tokens: estimateTokens(user), chars: user.length, system: (l.messages.find((m) => m.role === 'system') || {}).content?.length ?? 0 };
});
const total = calls.reduce((s, c) => s + c.tokens, 0);
const P = { 'deepseek-flash': { inMiss: { off: 0.15, peak: 0.30 }, out: { off: 0.6, peak: 1.2 } },
            'deepseek-v4-pro': { inMiss: { off: 0.66, peak: 1.32 }, out: { off: 1.98, peak: 3.96 } } };
const FX = 7.1, m4 = (v) => Number(v.toFixed(4));
const g2 = [];
for (const [model, p] of Object.entries(P)) for (const when of ['off', 'peak']) for (const outTok of [400, 800, 1500]) {
  const outTotal = outTok * calls.length;
  const c = total * p.inMiss[when] / 1e6 + outTotal * p.out[when] / 1e6;
  g2.push({ model, 时段: when === 'off' ? '离峰' : '峰时', 每章输出token: outTok, 输入USD: m4(total * p.inMiss[when] / 1e6), 输出USD: m4(outTotal * p.out[when] / 1e6), 合计USD: m4(c), 合计CNY约: m4(c * FX) });
}
const s1 = JSON.parse(fs.readFileSync('output/wubian-cost-pricing.json', 'utf8'));
const grand = [];
for (const [model, p] of Object.entries(P)) for (const when of ['off', 'peak']) {
  const s1In = s1.实测输入token.total, g2In = total;
  const out = (1000 * 79 + 800) + (800 * 79); // S1 每章 1000 + G2 每章 800 的中档假设
  const c = (s1In + g2In) * p.inMiss[when] / 1e6 + out * p.out[when] / 1e6;
  grand.push({ model, 时段: when === 'off' ? '离峰' : '峰时', 输入token合计: s1In + g2In, 整本G1加G2合计USD: m4(c), CNY约: m4(c * FX) });
}
const res = { calls: calls.length, inputTokensTotal: total, perCallTokensMin: Math.min(...calls.map((c) => c.tokens)), perCallTokensMax: Math.max(...calls.map((c) => c.tokens)), systemChars: calls[0].system, 计价: g2, 整本合计: grand };
fs.writeFileSync('output/wubian-g2-pricing.json', JSON.stringify(res, null, 2));
console.table(g2); console.table(grand);
console.log(JSON.stringify({ calls: res.calls, inputTokensTotal: res.inputTokensTotal, min: res.perCallTokensMin, max: res.perCallTokensMax, systemChars: res.systemChars }));
