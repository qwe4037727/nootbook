# 迭代 2 验收整改

状态：已完成（含 D3/D5/D6/D7 收口），待复审（2026-10-23）

授权：用户要求“开始整改迭代 2 的验收问题”；整改依据 `docs/iter2_acceptance_review_20261023.md`（结论：不通过，B1–B3 阻塞、D1–D8 偏差、任务记录缺失），并按 `task/roadmap.md` S0 要求补收 D3/D5/D6/D7。

允许改动范围：`internal/novel/memory_changeset.go`、`internal/analysis/extract.go`、`internal/store/memory_{changeset,candidates,extract,init}.go`、`internal/server/memory_review.go`、对应新增测试文件、`docs/issues.md`、本任务及配套记录。未改动用户 `data/`，未提交/推送。

自测方法：

- 按详设 §10 落地 U9/U10/U16/U18/U19/U24/U29/U31/U33 与 I2-A~I2-H 共 17 项用例（analysis 12 + store 21 + server 5 = 38 个新测试函数，含 D3/D5/D6/D7 与 EntityHint 用例）。
- `go build ./...`、`go vet ./...`。
- `go test ./... -count=1`。
- `go test -tags acceptance_review ./... -count=2`。

验收标准：B1–B3 修复且 §10 用例全部落地；D1/D2/D3/D4/D5/D6/D7/D8 修复，EntityHint 原因码修正；仅 `w_` 世界条目 register 分类（#28）待产品确认；常规与 acceptance_review 双绿。

## 本轮整改结果

1. **B1 零新增测试**：新增 `internal/analysis/extract_test.go`（9 个，覆盖 U18/U19/U24/U29/I2-B/I2-C/I2-D 与 B2/B3/D8）、`internal/store/memory_iter2_test.go`（15 个，覆盖 U9/U10/U16/U18/U29/U31/U33/I2-A~I2-H 及 D2）、`internal/server/memory_review_test.go`（5 个，含 24 个 D1 契身子用例与 §6 状态映射）。§10 表 17 项全部有对应用例。
2. **B2 U29 同值跳过**：`assembleChangeSet` 状态循环命中 active 三元组且 `Value` 相同时不再产出 `update_state`（记 skipped，不新建、不 superseded）；accept 侧 `applyUpdateStateOp` 增加同值兜底，重复提交同值 op 也不递增版本。修复后 U29 写入口与提交入口双覆盖。
3. **B3 未知主体毒化**：`resolveSubject` 未解析出 `c_`/`w_` ID 时不再生成 `add_state`，改为 `ConflictItem{Kind: ambiguous_participant}` 并保留实体候选；同类防护补空 `category` 直接跳过、非法 `EventType` 事件丢弃，避免其他记录级失败整批 422。
4. **D1 API 契约**：新增 `decodeStrictJSON`（4096B 上限 → 413 `request_too_large`；未知字段、尾随 JSON、`null`、数组、残缺 JSON → 400 `invalid_request`），替换 review 路由全部解码；accept/reject/rebase/reconcile 允许空体，extract/candidate resolve 要求对象体。
5. **D2 force 语义**：`BeginExtract` 去重改为 `changeSetStatesForChapter`——applied 同键**始终跳过**，pending 同键仅 `force=false` 跳过；`force=true` 不再重提已提交章节。
6. **D4 暂存期 manual_edit**：状态/线程循环在暂存时即检测目标 `lastSource=manual_edit`，产出 `ConflictItem{Kind: manual_edit, Old/Proposed/TargetID/ExpectedRecordVersion}` 且不产出 op；提交时点检测继续兜底。
7. **D8 多命中证据**：`locateQuoteRefs` 返回 `evidenceNote="quote_multi_hit"`（多处命中取第一处），并写入对应 ChangeOp。
8. **整改中发现的同链路缺陷一并修复**：① resolved 线程有证据但无对应解决事件时此前会生成缺 `ResolutionEvent` 的记录（accept 必 L-S 失败）——现按引文匹配同证据事件，无法匹配则降级 developing + conflict；② accept 此前丢弃暂存期冲突，零有效 op 路径也不落盘冲突——现全程保留并随 applied 持久化；③ `ImportChapters` 未清理 `changesets.json`/`extractCurrentRun`——现随记忆重置清空；④ `RebaseChangeSet` 误用 `store.mu` 读 ChangeSet——改为 `memoryMu`，U31 rebase 可用。
9. **回归结果**：`go build ./...`、`go vet ./...` 无诊断；`go test ./... -count=1` 6 包全 PASS；`go test -tags acceptance_review ./... -count=2` 6 包连续两轮全 PASS。D3/D5/D6/D7 及整改中新发现的两项事项已记入 `docs/issues.md` #23–#28。
10. **S0 二次收口（D3/D5/D6/D7 + EntityHint）**：① D3——`ChangeSet` 新增 `ChapterID`/`ChapterRevision` 并由 `ValidateChangeSet` 校验，提取组装写入，`changeSetCoversChapter` 结构化优先、旧数据回退 Reason；② D5——register 预写先走 `MatchCharacter`/`MatchWorldEntry`，唯一命中复用实体并入别名（幂等重试），交叉/歧义命中拒绝并提示显式 merge；③ D6——register 的 `Resolution` 写最终实体 ID；④ D7——新增 `LoadPendingEventRecords` 并在 `extractChapter` 合并 pending add_event 参与去重；⑤ EntityHint 歧义原因码改 `ambiguous_alias`。新增 9 个测试（store 6 + analysis 3），issues #23–#27 结案；仅 #28（`w_` 世界条目 register 分类）保留待产品确认。
