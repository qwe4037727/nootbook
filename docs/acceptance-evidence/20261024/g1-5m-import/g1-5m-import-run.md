# G1 500 万字级真实 HTTP 导入与续写前链路度量（2026-10-24）

原始数字：[g1-5m-summary.json](g1-5m-summary.json)（已剔除章节清单与正文，只留计数）

## 1. 环境与口径

- 二进制：`output/novelagent-ui.exe`（`go build -o output/novelagent-ui.exe .`，go1.25.4 windows/amd64，8 核），启动 `-addr 127.0.0.1:8097 -data output/g1-5m-data`。
- 代码基线：HEAD `f36025e` + 工作区 82 个未提交改动（即当前 R4 召回链路的真实代码状态，非某个已提交版本）。
- 数据目录是**全新空目录**，不读取也不写入仓库 `data/`。
- 语料：程序合成的模板句轮转（10 句 × 500 章，每章约 9,920 字；第 7 章埋一条全书唯一的三字线索「庚七铃」），合计 **4,966,490 rune / 14,789,682 UTF-8 字节**，非任何作品正文。
- 模型：无 Key；`baseUrl` 指向 `127.0.0.1:9`（discard 端口）只为验证失败路径。预览接口是只读的，不建立 LLM 连接。
- 脚本：`output/g1_5m_harness.mjs`（主度量，无并发采样）、`output/g1_5m_tiers.mjs`（分档）、`output/g1_5m_rss.mjs`（工作集采样，1 s 间隔）。时延以 harness / tiers 为准；RSS 探针第一轮因 40 ms 高频拉起 PowerShell 把时延污染到 9–15 s，该轮数据**不采用**，只采用改为 1 s 采样后的那一轮，且只用它的内存读数。

## 2. 结果

| 步骤 | 结果 | 数字 |
|---|---|---|
| `POST /api/import`（496.6 万字） | 200，自动分章 | 20.26 s（≈24.5 万 rune/s）；500 章；`charCount` 合计 4,959,708；响应 72,941 B（含 500 条章节元数据） |
| `GET /api/project` | 200 | 36 ms / 72,917 B |
| `GET /api/chapters/{id}` | 200 | 单章 3–4 ms / ≈29.8 KB；第 7 章正文含线索 |
| `PUT /api/config`（ctx=12000） | 200 | 99 ms；`hasApiKey=false` |
| 预览：开召回 + 含线索方向 | 200 | 1,146–1,531 ms（无采样复测 1,212–1,287 ms）；`recallEnabled=true`、预算 800、实注入 **103 字**、命中 **1 章＝第 7 章（埋点章）**、`recallSection` 含原句；正文窗口 8,200（关召回时 9,000）；`promptTotalChars` 8,973（关召回 9,574，即 −601）；`promptOverBudget=false` |
| 预览：强制开召回 + 模糊方向 | 200 | 1,277–1,342 ms；命中 0、注入 0 字；正文窗口回到 9,000 |
| 预览：关召回 | 200 | 383–443 ms |
| 超上限导入（5,000,001 字） | **400**「文稿太大，请控制在 500 万字以内」 | 223 ms；随后 `GET /api/project` 仍是 500 章、标题未变 → 拒绝请求不落盘、不破坏既有作品 |
| 数据目录 | — | 14,975,582 B / 514 文件（≈3.0 B/rune） |
| 分档（同一份 5M 数据走真实 HTTP） | 全部 `overBudget=false` | ctx=1600：召回预算 0 / 正文 1,453 / 总 2,027；4000：800 / 2,200 / 2,973；12000：800 / 8,200 / 8,973；30000：800 / 26,200 / 26,973 |
| 分析失败路径 | 不挂起、不留 running | 导入时 config 为空 → `analysis=error`「请先完成模型设置」；`PUT /api/config` 触发一次自动重试 → 拨号被拒 → `analysis=error`「分析中断，请检查模型设置后重试」，`analyzedCount` 恒为 0，连续两次读回状态一致 |
| 覆盖导入（再导 3 章小稿） | 200 | 章节文件 500→3、目录 14.98 MB→2,868 B / 17 文件、`analysis` 重置 pending → **无孤儿章节文件残留** |
| 服务进程工作集 | 参考值 | 三类预览期间 122–127 MB；本轮**不能**据此算出召回的内存增量（见 §3） |

## 3. 已证 / 未证

已证（本环境、合成语料、真实 HTTP 链路）：

1. 500 万字级**导入 + 自动分章 + 章节读写**在真实应用里跑得通，成本可度量（20 s / 15 MB / 3 B per rune），上限与拒绝语义生效且不损坏既有作品。
2. R4 召回在 500 章 / 496 万字规模下**仍然命中埋点章**（不是只在 60 章合成夹具上成立），注入受 800 字预算与闸门约束，未把正文窗口压垮，四档预算与计量一致。
3. 覆盖导入不留孤儿文件。
4. 无模型 Key 时分析走的是**干净失败路径**（错误可见、状态可预期、无在途残留）。

未证（不要把本节读成已完成）：

- **G1 的「自动分析出知识库」在 5M 上仍未跑通**——缺真实模型通道与 Key，本轮只证明失败路径干净。
- **记忆注入在 5M 上未被消费**：`disabledReason=memory_absent`（无分析即无记忆），所以本轮只是召回口径；记忆 3000 + 召回 800 同时非零的联合预算仍没有真实数据证据（属 G3）。
- 内存口径不可用：工作集在 122–143 MB 间波动，且「空闲读数」取自上一轮被高频采样污染的堆，无法分辨召回带来的增量；离线 eval 早先估的 257 MB 不能与本轮直接对比。
- 时延含每请求 `LoadChapters()`（读 15 MB JSON）；单用户本机一次测量，不代表多用户或低端机。
- 检索命中 ≠ 文学质量；`recall-on/off` 的 A/B 仍需 Key（见 #43③/#46①）。

## 4. 本轮新出的三条待办

1. **召回的时间成本与是否注入无关**：模糊方向 0 注入仍花约 1.28–1.34 s，而关召回 0.39 s——闸门只省正文窗口，不省时间。若要「没线索就别扫」，需把方向线索预检提到建索引之前，或加章节级倒排缓存（性能改造，需 yj 拍板是否排进 S6，关联 #46②）。
2. **`promptTotalChars` 与 `promptOverBudget` 口径不同**：前者是 system+user（[internal/agent/prompt.go:288-296](file:///D:/go/internal/agent/prompt.go#L288-L296)），后者只按 user 判定，于是 ctx=1600 档出现 total 2,027 > 1,600 却 `overBudget=false`，读数易被误读为超预算 → 记为 issues #53。
3. **文档里的 `/api/memory/preview` 路由不存在**（真实路由是 `POST /api/continue/preview`，`internal/server/server.go:56`）→ 记为 issues #52。

## 5. 复现

```text
go build -o output/novelagent-ui.exe .
./output/novelagent-ui.exe -addr 127.0.0.1:8097 -data output/g1-5m-data   # 全新目录
node output/g1_5m_harness.mjs http://127.0.0.1:8097
node output/g1_5m_slim.mjs                                                # 证据瘦身 + 派生量
node output/g1_5m_tiers.mjs http://127.0.0.1:8097
node output/g1_5m_rss.mjs http://127.0.0.1:8097
curl -s -X POST -H "content-type: application/json" --data-binary @output/g1-5m-reimport.json http://127.0.0.1:8097/api/import
```

脚本与合成语料都在 `output/`（已 gitignore）；证据 JSON 只含数字。
