我们需要为控制平面实现一个并发安全的内存区间分配器，使用 Go 1.22 或更高版本，仅依赖标准库。当前 `allocator` 包是可编译的公开接口骨架，请读取项目中的 `SPEC.md` 并完成实现。

分配器管理 `[0,Size)` 地址空间，支持 Reserve、Allocate、Free 原子批次。批次必须先完成全部结构校验，再在隔离候选状态中按输入顺序执行；Allocate 需要按给定 2 的幂对齐寻找最低可用区间，Reserve/Allocate 分配连续 revision，Free 不分配。名称冲突、区间重叠、空间不足、缺失释放、uint64 边界或最终数量容量失败必须整体回滚且不能消耗 revision。同批释放后再分配、删除后复用名称和临时超量但最终合规都要正确处理。还需实现无副作用 Find、Lookup 和稳定 Snapshot。

请保留公开 API 以及 `SPEC.md` 规定的校验顺序、最低位置策略、对齐、区间边界、批次顺序、回滚、revision、generation、最终容量、排序、返回结构和错误语义。不得修改 `SPEC.md`、`PROMPT.md`、`go.mod`、`allocator/contract_test.go`、`cmd/demo/main.go`；可以修改接口骨架并新增实现文件和测试。不要增加第三方依赖、访问网络、删除或弱化测试、硬编码示例结果，也不要创建 Git 提交。

请补充对齐与碎片、溢出边界、重叠、释放后分配、revision 回滚、最终容量、Find、排序和并发测试，并在 `README.md` 中说明索引、区间搜索、候选事务、算术安全以及实际复杂度。完成后执行 `go test ./...`、`go test -race ./...` 和 `go run ./cmd/demo`，如实报告结果与未完成内容。
