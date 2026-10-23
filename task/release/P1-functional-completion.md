# 已实现功能测试补齐

状态：已完成测试（发现实质缺陷与受阻项，不判全量通过）

授权：用户要求“已实现，但测试还不完整完成这部分的功能测试”；本轮执行上一条答复中的七类已实现功能测试，不将后续未实现功能纳入通过范围。

允许改动范围：internal/novel/*functional*test.go、internal/store/*functional*test.go、tools/functional-*、output/executions/functional-20261022/、docs/functional_completion_20261022.md、本任务及配套记录。运行时使用 gitignored .tmp_acceptance_data_functional_20261022/ 与 .tmp_acceptance_data_tools/；不改用户 data、不提交/推送。

自测方法：新增字段/顺序/回调/进程恢复探针；完整 acceptance_review；完整 go test -race；浏览器操作+API+只读文件对照；真实模型多章测试和上下文截断/停止恢复。

验收标准：每类分别记录 PASS/FAIL/blocked/needs_review，保存证据与实际覆盖边界；发现问题保留复现，不降低断言，不将接口不存在或未执行项计为通过。

## 本轮测试结果

1. 竞态测试 RACE-01：引入便携 GCC 运行 `go test -race ./...` 6 包全通过；运行受保护新功能探针亦通过；证据位于 `output/executions/functional-20261022/race.txt`。
2. 边界测试 BOUND-01：标量 80 子用例全通过，元数据 150 组合全通过；但四类列表单条在超长截断后达到 101 rune，超出规格上限 100（FAIL，已保留复现）。
3. 进程恢复 RECOVER-01：在生产持久化 7 个关键阶段直接终止子进程，Store 重启后链校验、重放及安全态一致，7/7 全部通过。
4. 章节顺序 ORDER-01：前插、逆序、删中间章节三种重排均通过，StoryOrder 重算与 RecordVersion 规则正确，幂等无翻转。
5. 回调收敛 HEAL-01：Windows 独占句柄模拟写入失败后，服务启动不会自动收敛旧事件；新增章节不触发顺序回调；只有后续再发生真实的删除操作时才会最终收敛。已保留失败探针记录。
6. 浏览器与接口 UI-01 / UI-02 / UI-03：导入、多章展示、字数统计、单章编辑保存、新建与删除、SSE 续写草稿、停止生成、未保存草稿不污染正文、追加正文版本递增、另存为新章节等全链路通过并完成截图取证。
7. 真实模型 MODEL-01：成功连接外部模型并完成单次 476/478/514 字正文生成与窗口截断验证；但在多章全书分析时多次遇到上游网络重置与代理拒绝连接，属于部分受阻（blocked/needs_review），不作虚假通过声明。
