# R4 前端召回展示与 `useRecall` 开关 —— 浏览器走查记录

日期：2026-10-24　执行人：Qoder agent（走查脚本自动执行，非人评）

## 1. 环境与数据

- 二进制：`go build -o novelagent-ui.exe .`（`//go:embed web`，前端改动必须重新编译才生效）
- 数据：**隔离副本**，不是 `data/`，也不是真实作品
  - `output/longeval-3753371950`：`cmd/longeval -case eval/cases/longform-synthetic.json -keep` 生成的合成 40 章（41,520 字）+ 记忆夹具
  - `output/longeval-10ch`：同副本删到 10 章（验证「自动」档不满 20 章不启用）
  - `output/longeval-2ch`：删到 2 章（验证强制开但窗口外无旧章节 → 无注入的告警分支）
- 模型通道：**没有可用通道**。`baseUrl` 指向本机 `output/stub_llm.mjs`（`127.0.0.1:8092/v1`，收请求后延迟 6s 返回 502）。
  这样可以只验证 SSE `meta`（`meta` 在调用模型之前发出），**不代表真实续写跑通**。
- 浏览器：Playwright 1.64.1 + 本机 chromium-1208，headless，1360×950

## 2. 复现命令

```
go build -o novelagent-ui.exe .
node output/stub_llm.mjs &
./novelagent-ui.exe -addr 127.0.0.1:8090 -data D:/go/output/longeval-3753371950 &
./novelagent-ui.exe -addr 127.0.0.1:8093 -data D:/go/output/longeval-10ch &
./novelagent-ui.exe -addr 127.0.0.1:8095 -data D:/go/output/longeval-2ch &
node output/set_config.mjs        # 把 8090 的 baseUrl 指到 stub
node output/trim_to_10ch.mjs      # 删章节 + 配 stub
node output/trim_to_2ch.mjs       # 删章节 + 打印三档 preview 结果
node output/r4_ui_walk.mjs        # 40 章主链路，退出码 0
node output/r4_ui_walk_short.mjs  # 10 章边界，退出码 0
node output/r4_ui_walk_tiny.mjs   # 2 章无命中告警，退出码 0
```

## 3. 走查结论（全部为浏览器实际渲染结果，非代码推断）

| 判据 | 结果 | 证据 |
| --- | --- | --- |
| 预览面板出现「前文相关片段」段并标注命中章节 | 通过：标题 `前文相关片段（来自第 25、3、31 章）`，段内 763 字 | `r4-ui-1-preview-auto.png`、`r4-ui-walk-result.json` |
| 召回段与计划段视觉可区分 | 通过：召回段 `2px solid` 强调色左边框 + `max-height:220px; overflow-y:auto`（可内部滚动），计划段仍是虚线框 | `auto_recall_border`、`auto_recall_maxheight`、`r4-ui-4-recall-only.png` |
| 页脚计量 | 通过：`… · 计划 1 条 · 前文召回 3 段 · 763/800 字` | `auto_foot` |
| 开关「关」→ 面板无召回段且说明原因 | 通过：`前文召回已关闭` + toast「前文召回已关闭，本次不会检索旧章节」 | `r4-ui-3-preview-recall-off.png`、`off_foot` |
| 开关改变请求体 | 通过：`useRecall` 分别为 `null / true / false`（自动=不下发） | `r4-ui-walk-result.json` 的 `api_requests` |
| 「记忆注入」关 + 召回开（A/B 对照组形态） | 通过：面板只剩计划段与召回段，页脚 `计划 1 条 · 前文召回 3 段 · 763/800 字` | `r4-ui-4-recall-only.png` |
| 续写 SSE `meta` 文案 | 通过：开 → `… · 计划 1 条 · 前文召回 3 段 · 763 字`；关 → 无召回字样 | `r4-ui-6-continue-recall-on.png`、`r4-ui-5-continue-recall-off.png` |
| 满 20 章才自动启用（自动档） | 通过：10 章副本「自动」无召回段，「开」仍有 2 段（来自第 2、1 章） | `r4-ui-walk-short.txt`、`r4-ui-7-short-forced-on.png` |
| 强制开但无注入 → 显式告警 | 通过：2 章副本页脚/空态提示「已要求前文召回但无注入：旧章节没有命中片段，或正文窗口不让位」，续写时另发 error toast | `r4-ui-10-tiny-foot-warning.png`、`r4-ui-11-tiny-empty-warning.png`、`r4-ui-12-tiny-continue-warning.png` |
| 控制台无报错 | 通过：三次走查 `console_errors` 均为空数组 | 三个 `*-result.json` |

## 4. 走查中发现并当场修掉的问题

1. 预览面板页脚原先只在「关」时说明原因，强制开却无命中时界面无任何反馈 → 补 `已要求前文召回但无注入…`（只在显式「开」时出现，「自动」档不出现，避免把策略性关闭误报成异常）。
2. 原文案「正文满 20 章才会生效」与实现不符：`useRecall=true` 会绕过 20 章门槛（`agent.recallBudgetFor` 只在 `nil` 时按 `novel.RecallEnabled` 自动判定），10 章副本实测确实注入了 582 字 → 文案改为「不满 20 章也会检索」，并把无注入原因从「章节不足 20 章」改为「旧章节没有命中片段，或正文窗口不让位」。

## 5. 仍未覆盖（不要当成已完成）

- 真实模型通道下的续写：本次只验证到 `meta`（模型调用之前），草稿正文仍是 502 失败。
- R4 开/关的**文学质量 A/B 人评**：工具链已支持 `cmd/evalreview -axis recall`（见 `../r4-review/`），但需要真实模型输出才能出结论。
- 真实长篇语料（G1 通道阻断中）下的召回命中质量。

## 6. 收尾回归与清理（2026-10-24）

- 走修后回归：`go build ./...`、`go vet ./...`、`go test ./... -count=1` 全部通过；`CGO_ENABLED=1 go test -race ./... -count=1` 亦全绿（9 包，无 DATA RACE，见 `../r4-race.txt` §4）。本段只改 `web/` 静态资源与台账文档，无 Go 侧改动。
- 前端没有 JS 测试框架，上述判据全部由 Playwright 脚本在真实渲染页面上断言；脚本副本已随本目录归档（`r4_ui_walk*.mjs`、`stub_llm.mjs`、`set_config.mjs`、`trim_to_*.mjs`、`recall_probe.mjs`），脱离 `output/` 也能照 §2 复现。
- 走查用的三个服务（8090/8093/8095）与 stub（8092）已停止；`novelagent-ui.exe` 与 `output/longeval-*` 副本均在 `.gitignore` 内（`*.exe`、`/output/`），未触碰 `data/`。
