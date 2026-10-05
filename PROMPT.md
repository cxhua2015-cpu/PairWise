我们需要为分布式控制面实现一个并发安全的内存型“控制拓扑图 488”，使用 Go 1.22 或更高版本，仅依赖标准库。当前 `topologygraph488` 包是可编译公开接口骨架，请读取 `SPEC.md` 并完成实现。

核心语义：原子批次支持 AddNode/DeleteNode/AddEdge/DeleteEdge。加边必须阻止有向环；删除节点同时删除关联边。最终节点/边容量只在批次末检查，失败整体回滚。Reachable 使用当前一致快照，Snapshot 对节点和边稳定排序。 所有公开方法必须支持并发调用。

请保留公开 API、错误值和规范顺序。不得修改 `SPEC.md`、`PROMPT.md`、`go.mod`、`topologygraph488/contract_test.go`、`cmd/demo/main.go`；不要增加第三方依赖、访问网络、删除或弱化测试、硬编码示例结果，也不要创建 Git 提交。请补充边界与并发测试，在 README 中说明索引、候选事务、所有权及复杂度。完成后执行 `go test ./...`、`go test -race ./...` 和 `go run ./cmd/demo`。
