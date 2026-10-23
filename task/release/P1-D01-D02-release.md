# D01/D02 发布范围与重初始化契约

状态：已完成（限定范围发布验收通过，待人工审阅工作区；未提交/推送）

授权：用户明确要求“处理此前 D01/D02 的发布范围与重初始化接口契约，再完成发布验收”。

允许改动范围：internal/server/memory.go、internal/server/memory_test.go、docs/acceptance.md、docs/features.md、docs/memory_model_spec.md、docs/issues.md、docs/v1.0_release_acceptance_20261022.md、docs/acceptance-evidence/20261022/release-*、本任务及配套记录。

自测方法：go test ./internal/server；go test -tags acceptance_review ./internal/store -count=2；go test ./...；go build ./...；go vet ./...。

验收标准：发布范围不将部分覆盖或后续能力写成全部通过；显式 reinitialize=true 返回 400 reinit_not_supported 且不落盘；空请求、{}、false 保留正常初始化和 ready 幂等；非法类型/未知字段/尾随 JSON/超限被拒绝且不落盘；既有探针和全量检查通过。保留历史报告，不修改正式数据、不自动提交或推送。
