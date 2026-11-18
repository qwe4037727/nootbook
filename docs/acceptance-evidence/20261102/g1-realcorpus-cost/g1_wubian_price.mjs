import fs from 'node:fs';
const s = JSON.parse(fs.readFileSync('output/wubian-cost-summary.json', 'utf8'));

// DeepSeek 官方现价（USD / 1M tokens），逐格取自 api-docs 定价页表格（output/ds_pricing.html）。
const P = {
  'deepseek-flash': { inMiss: { off: 0.15, peak: 0.30 }, inHit: { off: 0.003, peak: 0.006 }, out: { off: 0.6, peak: 1.2 } },
  'deepseek-v4-pro': { inMiss: { off: 0.66, peak: 1.32 }, inHit: { off: 0.022, peak: 0.044 }, out: { off: 1.98, peak: 3.96 } },
};
const FX = 7.1; // CNY/USD 仅作换算说明，不是账单口径

const a = s.byKind.analyst, syn = s.byKind.synthesis;
const inTok = { analyst: a.inputTokens, synthesis: syn.inputTokens, total: s.totalInputTokens };
const outPerAnalyst = [500, 1000, 2000];

const money = (v) => Number((v).toFixed(4));
const rows = [];
for (const [model, p] of Object.entries(P)) {
  for (const when of ['off', 'peak']) {
    for (const outTok of outPerAnalyst) {
      const outTotal = outTok * a.calls + 800; // 800 ≈ 总览合成调用的输出（overview 300–500 字 + style）
      const inCost = inTok.total * p.inMiss[when] / 1e6;
      const outCost = outTotal * p.out[when] / 1e6;
      rows.push({ model, 时段: when === 'off' ? '离峰' : '峰时', 每章输出token: outTok,
        输入USD: money(inCost), 输出USD: money(outCost), 合计USD: money(inCost + outCost),
        合计CNY约: money((inCost + outCost) * FX) });
    }
  }
}

// G8：30–50 章连写 + 每章深度提取，用真实语料实测的单次续写输入 8629 tokens
const contIn = 8629, extractIn = Math.round(a.inputTokens / a.calls); // 提取请求量级≈单章正文 token
const g8 = [];
for (const [model, p] of Object.entries(P)) {
  for (const when of ['off', 'peak']) {
    for (const n of [30, 50]) {
      const inT = n * (contIn + extractIn + 1500); // 1500 ≈ 深度提取的 prompt 指令与已有状态段
      const outT = n * (800 + 900);              // 续写 800 + 提取 900
      const c = inT * p.inMiss[when] / 1e6 + outT * p.out[when] / 1e6;
      g8.push({ model, 时段: when === 'off' ? '离峰' : '峰时', 章数: n,
        输入token: inT, 输出token: outT, 合计USD: money(c), 合计CNY约: money(c * FX) });
    }
  }
}

const digestBound = a.digestCharsMax;
const summary = {
  真实语料: { 章数: a.calls, 字数: s.import.importedChars, 语料字节: s.corpusBytes },
  实测输入token: inTok,
  单次分析输入区间: [a.tokensMin, a.tokensMax],
  已有知识摘要段实测上限字符: digestBound,
  命中30000字裁剪的章数: a.clipped,
  分析结果状态: s.analysisAfter?.status, analyzedCount: s.analysisAfter?.analyzedCount,
  S1整本分析计价: rows,
  G8连写计价: g8,
  口径说明: [
    '输入 token 是实测值（桩服务记录的每一次真实请求体，按 internal/eval/token.go 同口径估算）；输出 token 产品侧无法在无 Key 下测得，故给 500/1000/2000 三档敏感性区间。',
    '全部按 cache MISS 计价：分析请求里可复用的前缀只有 system 提示 + 作品/本章抬头 + 摘要段（实测摘要段最大 ' + digestBound + ' 字），正文各不相同，缓存命中率极低，所以 cache HIT 列不适用于 S1。',
    '价格单位 USD / 1M tokens，取自 DeepSeek 官方定价页；离峰/峰时是官方的两套价，实际取决于跑批时间。',
    'CNY 换算仅作直觉参考，DeepSeek 计费以美元为准。',
  ],
};
fs.writeFileSync('output/wubian-cost-pricing.json', JSON.stringify(summary, null, 2));
console.table(rows);
console.table(g8);
console.log(JSON.stringify({ 实测输入token: inTok, 单次分析输入区间: [a.tokensMin, a.tokensMax], 摘要段上限字符: digestBound, 裁剪章数: a.clipped }, null, 2));
