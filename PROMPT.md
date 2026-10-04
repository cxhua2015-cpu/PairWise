我们需要为控制平面实现一个并发安全的内存多账户账本，使用 Go 1.22 或更高版本，仅依赖标准库。当前 `ledger` 包是可编译的公开接口骨架，请读取项目中的 `SPEC.md` 并完成实现。

账本支持 Open、Credit、Debit、Transfer、Close 原子批次，必须先完成全部结构校验，再在隔离候选状态中按输入顺序执行。每个成功操作消耗一个连续 revision，Transfer 两端共享同一 revision。账户冲突、缺失、余额不足、非零余额关闭、int64 溢出或最终容量失败必须整体回滚且不能消耗 revision。同批关闭后重开、转账后继续扣款和临时超量但最终合规都要正确处理。

请保留公开 API 与 `SPEC.md` 的校验顺序、操作顺序、回滚、revision、generation、最终容量、排序和错误语义。不得修改 `SPEC.md`、`PROMPT.md`、`go.mod`、`ledger/contract_test.go`、`cmd/demo/main.go`；可以修改接口骨架并新增测试。不要增加第三方依赖、访问网络、弱化测试或创建 Git 提交。完成后执行 `go test ./...`、`go test -race ./...` 和 `go run ./cmd/demo`。
