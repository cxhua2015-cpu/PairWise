我们需要为消息平台实现一个并发安全的内存分区日志，使用 Go 1.22 或更高版本，仅依赖标准库。当前 `topiclog` 包是可编译的公开接口骨架，请读取项目中的 `SPEC.md` 并完成实现。

日志需要按 key 稳定分区，为每个 topic/partition 分配连续 offset，支持批量原子追加、按 offset 分页读取、消费组 offset 的批量原子提交，以及只有所有已知消费组都确认后才能执行的安全裁剪。所有批次必须先完成结构校验，再在隔离候选状态中按输入顺序执行语义操作，只在最终状态检查 topic、记录数和 Payload 字节容量；失败整体回滚，成功的非空变更 generation 只推进一次。Snapshot、Read、Append 输入的 Payload 必须所有权隔离，所有公开方法支持并发调用。

请保留公开 API 以及 `SPEC.md` 规定的分区算法、校验顺序、边界、offset、排序、返回结构和错误语义。不得修改 `SPEC.md`、`PROMPT.md`、`go.mod`、`topiclog/contract_test.go`、`cmd/demo/main.go`；可以修改接口骨架并新增实现文件和测试。不要增加第三方依赖、访问网络、删除或弱化测试、硬编码示例结果，也不要创建 Git 提交。

请补充分区、批量回滚、提交/裁剪边界、所有权和并发测试，并在 `README.md` 中说明索引、offset 分配、事务、容量、安全裁剪、所有权以及实际复杂度。完成后执行 `go test ./...`、`go test -race ./...` 和 `go run ./cmd/demo`，如实报告结果与未完成内容。
