我们需要实现一个并发安全的内存双向唯一映射注册表，使用 Go 1.22 或更高版本，仅依赖标准库。当前 `bimap` 包是可编译的公开接口骨架，请读取 `SPEC.md` 并完成实现。

注册表支持 Bind、Unbind 原子批次，左右两侧各自全局唯一。批次必须先完成全部结构校验，再在隔离候选状态中按输入顺序同时维护双索引；解绑后重绑可以复用任意一侧。冲突、缺失、配对不匹配或最终容量失败必须整体回滚且不能消耗 revision。还需实现双向查询、稳定 List 游标分页和 Snapshot。

请保留公开 API 与 `SPEC.md` 的校验顺序、双索引一致性、批次顺序、回滚、revision、generation、最终容量、排序和错误语义。不得修改 `SPEC.md`、`PROMPT.md`、`go.mod`、`bimap/contract_test.go`、`cmd/demo/main.go`；可新增实现和测试。不要增加第三方依赖、访问网络、弱化测试或创建 Git 提交。完成后执行 `go test ./...`、`go test -race ./...` 和 `go run ./cmd/demo`。
