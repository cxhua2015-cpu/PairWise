我们需要为分布式控制面实现一个并发安全的内存型“控制拓扑图 428”，使用 Go 1.22 或更高版本，仅依赖标准库。当前 `topologygraph428` 包是可编译公开接口骨架，请读取 `SPEC.md` 并完成实现。

核心语义：原子批次支持 AddNode/DeleteNode/AddEdge/DeleteEdge。加边必须阻止有向环；删除节点同时删除关联边。最终节点/边容量只在批次末检查，失败整体回滚。Reachable 使用当前一致快照，Snapshot 对节点和边稳定排序。 所有公开方法必须支持并发调用。

请保留公开 API、错误值和规范顺序。不得修改 `SPEC.md`、`PROMPT.md`、`go.mod`、`topologygraph428/contract_test.go`、`cmd/demo/main.go`；不要增加第三方依赖、访问网络、删除或弱化测试、硬编码示例结果，也不要创建 Git 提交。请补充边界与并发测试，在 README 中说明索引、候选事务、所有权及复杂度。完成后执行 `go test ./...`、`go test -race ./...` 和 `go run ./cmd/demo`。

本题为多文件联动任务：除原核心实现外，必须分别完成 `validation.go`、`stats.go`、`clone.go` 和 `preview.go`。`Preview` 需要在一致快照上复用完整事务语义，返回候选 Result、Snapshot、Stats，同时保证原对象、逻辑时钟和所有权不变；错误及优先级必须与同一状态上的 `Apply` 一致。不得把全部逻辑合并回单个文件，也不得修改新增的 `integration_test.go`。

