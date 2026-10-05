我们需要为边缘代理实现一个并发安全的内存前缀访问控制表，使用 Go 1.22 或更高版本，仅依赖标准库。当前 `prefixacl` 包是可编译的公开接口骨架，请读取 `SPEC.md` 并完成实现。

访问控制表支持原子批量 Upsert/Delete，IPv4 与 IPv6 规则共存，查询按最长前缀匹配，未匹配时采用默认动作。批次必须先完成结构校验，在隔离候选状态中按输入顺序执行，并只在最终状态检查容量。失败整体回滚且不能消耗 revision 或 generation。规则前缀必须有效且规范化，快照排序必须确定，所有方法必须支持并发调用。

请保留公开 API、错误值以及 `SPEC.md` 的校验、匹配、排序、revision/generation 和回滚语义。不得修改 `SPEC.md`、`PROMPT.md`、`go.mod`、`prefixacl/contract_test.go`、`cmd/demo/main.go`；不要增加第三方依赖、访问网络、删除或弱化测试、硬编码示例结果，也不要创建 Git 提交。

请补充必要的 IPv4/IPv6、最长匹配、顺序批次、回滚和并发测试，并在 `README.md` 中说明索引、候选事务、匹配和复杂度。完成后执行 `go test ./...`、`go test -race ./...` 和 `go run ./cmd/demo`，如实报告结果与未完成内容。
