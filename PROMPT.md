我们需要为分布式控制面实现一个并发安全的内存型“元数据目录 241”，使用 Go 1.22 或更高版本，仅依赖标准库。当前 `metacatalog241` 包是可编译公开接口骨架，请读取 `SPEC.md` 并完成实现。

核心语义：原子批次按输入顺序执行 Put/Delete；Put 分配连续 revision，Delete 不分配。完整结构校验先于状态读取，最终记录数与 Value 总字节容量只在批次末检查。失败回滚全部状态、generation 和 revision。Get/Snapshot 深拷贝 Value，Snapshot 按名称排序。 所有公开方法必须支持并发调用。

请保留公开 API、错误值和规范顺序。不得修改 `SPEC.md`、`PROMPT.md`、`go.mod`、`metacatalog241/contract_test.go`、`cmd/demo/main.go`；不要增加第三方依赖、访问网络、删除或弱化测试、硬编码示例结果，也不要创建 Git 提交。请补充边界与并发测试，在 README 中说明索引、候选事务、所有权及复杂度。完成后执行 `go test ./...`、`go test -race ./...` 和 `go run ./cmd/demo`。

本题为多文件联动任务：除原核心实现外，必须分别完成 `validation.go`、`stats.go`、`clone.go`，并保证预检、事务、统计和克隆共享一致语义。不得把全部逻辑合并回单个文件，也不得修改新增的 `integration_test.go`。

