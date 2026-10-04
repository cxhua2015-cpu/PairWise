我们需要为控制平面实现一个并发安全的内存条件事务键值存储，使用 Go 1.22 或更高版本，仅依赖标准库。当前 `casstore` 包是可编译的公开接口骨架，请读取项目中的 `SPEC.md` 并完成实现。

存储需要支持 Exists、NotExists、Revision 和 Value 四类比较，以及由 Put/Delete 组成的条件事务。全部比较和写入必须先完成结构校验；比较只针对事务开始时的快照，全部成立后才在隔离候选状态中按输入顺序执行写入并分配连续 revision。比较不成立是正常的未提交结果，语义错误或最终容量失败必须整体回滚且不能消耗 revision。还需支持深拷贝 Get、按前缀和 after 游标稳定分页、稳定 Snapshot，正确处理同批重复写、删除重建、最终容量、所有权和并发调用。

请保留公开 API 以及 `SPEC.md` 规定的校验顺序、比较语义、写入顺序、revision、generation、容量、分页、排序、返回结构和错误语义。不得修改 `SPEC.md`、`PROMPT.md`、`go.mod`、`casstore/contract_test.go`、`cmd/demo/main.go`；可以修改接口骨架并新增实现文件和测试。不要增加第三方依赖、访问网络、删除或弱化测试、硬编码示例结果，也不要创建 Git 提交。

请补充比较失败、事务回滚、重复写、revision、分页边界、容量、所有权和并发测试，并在 `README.md` 中说明索引、比较、事务、revision 分配、容量、所有权以及实际复杂度。完成后执行 `go test ./...`、`go test -race ./...` 和 `go run ./cmd/demo`，如实报告结果与未完成内容。
