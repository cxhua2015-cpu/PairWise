# scoreboard

并发安全的内存事务积分榜（Go 1.22+，仅标准库）。公开契约见 `SPEC.md`。

## 设计说明

**索引**：`Board` 内部只维护一份 `map[string]Entry`（成员名 → 条目），由一把
`sync.RWMutex` 保护。写操作（`Apply`）持写锁，读操作（`Get`/`Range`/`Snapshot`）持读锁，
可并发执行。不维护持久化有序索引，排序在读取时按需进行。

**候选事务**：`Apply` 分两阶段。第一阶段在不读状态的情况下对全部操作做结构校验
（成员名、Kind、Score/Delta 字段约束），任何非法立即返回 `ErrInvalidInput`。第二阶段
把当前成员表浅拷贝为隔离候选状态，按输入顺序在其上执行操作；任一步失败（成员缺失、
算术溢出、最终容量超限）直接丢弃候选，原状态、generation 和 revision 分配完全不受影响，
即整体回滚且不消耗 revision。容量只在全部操作执行完后按最终成员数检查一次，因此
同批"先删后建"可以腾出容量。

**Revision**：`NextRevision` 从 1 开始。Upsert 和 Increment 各分配一个连续 revision
并写入条目；Delete 不分配。批次成功提交时一次性推进计数器，失败时丢弃候选计数，
revision 序列无空洞。`Result.Revision` 为本批最后分配的 revision（本批无分配则为 0）。

**Generation**：成功的非空批次提交时 generation 恰好加 1；空批次或失败批次不变。

**排序与分页**：统一排序键为分数降序、成员名升序。`Range` 按 `[min,max]` 闭区间过滤后
排序，设置游标时只返回严格位于 `(Score, Member)` 之后的条目（游标成员不必仍存在），
再截取 `limit` 条。`Snapshot` 使用同一排序。所有返回的切片都是新建拷贝，与内部状态
所有权隔离。

**Changed**：`Result.Changed` 为本批触及且在最终状态中仍存在的成员（去重），按同一
排序键排序，Revision 为该成员最后一次分配值。

## 实际复杂度

设 n 为当前成员数，k 为批次操作数，m 为过滤后条目数：

- `Apply`：校验 O(k·L)（L 为名字长度），候选拷贝 O(n)，执行 O(k)，Changed 排序 O(k log k)。
- `Get`：O(1)（均摊）。
- `Range`：O(n) 过滤 + O(m log m) 排序 + O(limit) 截取。
- `Snapshot`：O(n log n)。
- 空间：O(n)，候选事务期间临时 O(n + k)。

## 验证

- `go test ./...`：契约测试与扩展测试（重复操作、删除重建、revision 回滚、
  极值算术、最终容量、游标边界、排序、快照隔离、并发混合）全部通过。
- `go test -race ./...`：无数据竞争。
- `go run ./cmd/demo`：输出 `generation=1 revision=3 leader=bob score=12 entries=2`。
