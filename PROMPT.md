我们需要为控制平面实现一个并发安全的内存事务 DAG 存储，使用 Go 1.22 或更高版本，仅依赖标准库。当前 `dagstore` 包是可编译的公开接口骨架，请读取项目中的 `SPEC.md` 并完成实现。

存储支持 AddNode、UpdateNode、DeleteNode、AddEdge、RemoveEdge 原子批次。批次必须先完成全部结构校验，再在隔离候选状态中按输入顺序执行；每个成功操作分配连续 revision。节点或边冲突、缺失、带关联边删除节点、形成有向环、最终节点数/边数/Payload 总字节容量失败必须整体回滚且不能消耗 revision。同批先移除边再删除节点、先添加节点再连边、临时超量但最终合规都要正确处理。还需实现 Reachable、字典序最小的稳定 Topological 和稳定 Snapshot，所有 Payload 输入输出必须深拷贝。

请保留公开 API 以及 `SPEC.md` 规定的校验顺序、批次顺序、DAG 不变量、循环检测、回滚、revision、generation、最终容量、拓扑排序、返回结构和错误语义。不得修改 `SPEC.md`、`PROMPT.md`、`go.mod`、`dagstore/contract_test.go`、`cmd/demo/main.go`；可以修改接口骨架并新增实现文件和测试。不要增加第三方依赖、访问网络、删除或弱化测试、硬编码示例结果，也不要创建 Git 提交。

请补充深层循环、边删除与节点复用、revision 回滚、最终容量、Payload 所有权、稳定拓扑序和并发测试，并在 `README.md` 中说明双索引、候选事务、循环检测、revision、容量、所有权以及实际复杂度。完成后执行 `go test ./...`、`go test -race ./...` 和 `go run ./cmd/demo`，如实报告结果与未完成内容。
