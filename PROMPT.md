我们需要为分布式控制面实现一个并发安全的内存型“就绪优先队列 280”，使用 Go 1.22 或更高版本，仅依赖标准库。当前 `readyqueue280` 包是可编译公开接口骨架，请读取 `SPEC.md` 并完成实现。

核心语义：队列使用显式非负单调时间，Apply 原子顺序执行 Enqueue/Cancel，Enqueue 分配 revision，最终容量只在末尾检查。Pop 选择 ReadyAt <= now 的任务，按 Priority 降序、ReadyAt 升序、ID 升序并原子删除。失败回滚时间、状态和 revision。 所有公开方法必须支持并发调用。

请保留公开 API、错误值和规范顺序。不得修改 `SPEC.md`、`PROMPT.md`、`go.mod`、`readyqueue280/contract_test.go`、`cmd/demo/main.go`；不要增加第三方依赖、访问网络、删除或弱化测试、硬编码示例结果，也不要创建 Git 提交。请补充边界与并发测试，在 README 中说明索引、候选事务、所有权及复杂度。完成后执行 `go test ./...`、`go test -race ./...` 和 `go run ./cmd/demo`。
