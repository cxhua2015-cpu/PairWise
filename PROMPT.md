我们需要为控制平面实现一个并发安全的内存事务积分榜，使用 Go 1.22 或更高版本，仅依赖标准库。当前 `scoreboard` 包是可编译的公开接口骨架，请读取项目中的 `SPEC.md` 并完成实现。

积分榜支持 Upsert、Increment、Delete 原子批次。批次必须先完成全部结构校验，再在隔离候选状态中按输入顺序执行；Upsert 和 Increment 分配连续 revision，Increment 的 int64 加法不能溢出，Delete 不分配 revision。成员缺失、算术溢出或最终容量失败必须整体回滚且不能消耗 revision。同批重复操作、删除后重建和最终容量都要正确处理。还需实现 Get、按分数降序/成员名升序排列的稳定 Range 游标分页和 Snapshot。

请保留公开 API 以及 `SPEC.md` 规定的校验顺序、操作顺序、回滚、revision、generation、最终容量、游标、排序、返回结构和错误语义。不得修改 `SPEC.md`、`PROMPT.md`、`go.mod`、`scoreboard/contract_test.go`、`cmd/demo/main.go`；可以修改接口骨架并新增实现文件和测试。不要增加第三方依赖、访问网络、删除或弱化测试、硬编码示例结果，也不要创建 Git 提交。

请补充重复操作、删除重建、revision 回滚、极值算术、最终容量、游标边界、排序和并发测试，并在 `README.md` 中说明索引、候选事务、revision、排序分页以及实际复杂度。完成后执行 `go test ./...`、`go test -race ./...` 和 `go run ./cmd/demo`，如实报告结果与未完成内容。
