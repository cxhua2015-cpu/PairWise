我们需要为控制平面实现一个并发安全、使用显式时间的内存过期键值存储，使用 Go 1.22 或更高版本，仅依赖标准库。当前 `expiringstore` 包是可编译的公开接口骨架，请读取项目中的 `SPEC.md` 并完成实现。

存储需要支持带绝对过期时间的 Put、Delete、Touch 原子批次。每个批次必须先完成全部结构校验，再检查全局时间单调性，并在隔离候选状态中先清理到期键、再按输入顺序执行操作和分配连续 revision；缺失键、时间倒退或最终容量失败必须整体回滚，连批次开始时的过期清理、时间、generation 和 revision 都不能泄漏。还需实现会原子推进时间并清理到期项的 Get 和 Sweep，以及稳定 Snapshot、深拷贝所有权、过期边界和并发调用。

请保留公开 API 以及 `SPEC.md` 规定的校验顺序、时间单调性、过期边界、批次顺序、回滚、revision、generation、容量、排序、返回结构和错误语义。不得修改 `SPEC.md`、`PROMPT.md`、`go.mod`、`expiringstore/contract_test.go`、`cmd/demo/main.go`；可以修改接口骨架并新增实现文件和测试。不要增加第三方依赖、访问网络、删除或弱化测试、硬编码示例结果，也不要创建 Git 提交。

请补充过期与回滚、重复写、Touch/Delete、revision、容量、所有权、时间边界和并发测试，并在 `README.md` 中说明索引、过期算法、批次事务、时间、容量、所有权以及实际复杂度。完成后执行 `go test ./...`、`go test -race ./...` 和 `go run ./cmd/demo`，如实报告结果与未完成内容。
