# 问题记录

## 已修复（本轮整改）

- I2-R01 (B2)：U29 同值重提在暂存入口无条件生成 `update_state`，造成旧记录 superseded 与 recordVersion 递增；已在暂存入口同值跳过，提交入口同值兜底。
- I2-R02 (B3)：未知/歧义主体仍生成 `add_state`，`SubjectID` 为空导致整个 ChangeSet accept 422；已改为暂存期 ConflictItem + 保留候选，并补空 category/非法事件类型防护。
- I2-R03 (D1)：review API 尾随 JSON 被接受、超限无 413；已统一严格解码（4096B 上限、未知字段/尾随 JSON/null/数组 400）。
- I2-R04 (D2)：`force=true` 会重提已 applied 同键 ChangeSet；已改为 applied 始终跳过、pending 仅 force 绕过。
- I2-R05 (D4)：manual_edit 冲突仅提交时点检测；已补暂存时点 ConflictItem。
- I2-R06 (D8)：quote 多处命中无 evidenceNote；已写入 `quote_multi_hit`。
- I2-R07：resolved 线程有证据但无 ResolutionEvent 时生成不可提交记录；已按引文匹配同证据事件，失败则降级 developing + conflict。
- I2-R08：accept 丢弃暂存期冲突（含零有效 op 路径）；已随 applied 持久化。
- I2-R09：导入重置未清理 `changesets.json`/`extractCurrentRun`；已随记忆重置清空。
- I2-R10：`RebaseChangeSet` 在 `store.mu` 下读 ChangeSet，锁域不一致；已改 `memoryMu`。
- I2-D3：章节溯源依赖解析 `ChangeSet.Reason` 字符串；已改结构化 `ChapterID`/`ChapterRevision`（旧数据保留 Reason 回退）。
- I2-D5：register 预写未查重、重试可重复注册；已先 MatchCharacter/MatchWorldEntry，唯一命中复用实体并入别名，歧义拒绝。
- I2-D6：register 成功后的 Resolution 未记录实体 ID；已写最终 `c_`/`w_` ID。
- I2-D7：事件去重未覆盖 pending ChangeSets；已新增 `LoadPendingEventRecords` 并在提取前合并去重。
- I2-D27：EntityHints 歧义候选 reason 误标 `unknown_entity`；已按 `ambiguous` 分支写 `ambiguous_alias`。

## 保留台账（待产品确认，详见 docs/issues.md #28）

- I2-D28：`w_` 世界条目 register 分类待产品确认（当前未知实体仍登记为角色 `c_`；命中既有世界条目时复用 `w_`）。
