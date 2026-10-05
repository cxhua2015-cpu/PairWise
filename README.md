# schemaindex

并发安全的内存型“模式索引”，用于分布式控制面。仅依赖 Go 标准库（Go 1.22+）。

## 索引结构

`Store` 以 `map[string]entry` 作为主索引，键为记录名（非空 ASCII 小写字母、数字、`-`、`_`，长度受 `Options.MaxNameBytes` 约束），值为不可变的 `entry{value, revision}`。另维护两个单调计数器：`generation`（每个非空成功批次 +1）和 `nextRevision`（下一个待分配的 revision，从 1 开始）。一把 `sync.RWMutex` 保护全部内部状态：`Apply` 持写锁，`Get`/`Snapshot` 持读锁，因此所有公开方法可并发调用。

## 候选事务（原子批次）

`Apply` 分四个阶段，任一阶段失败都不会留下副作用：

1. **结构校验**：先校验全部 op 的 kind、名称字符集/长度、Value 长度，不做任何状态读取；失败返回 `ErrInvalidInput`。
2. **候选执行**：克隆当前 map 得到候选事务，按输入顺序执行 Put/Delete。Put 分配连续 revision（Delete 不分配）；Delete 缺失键返回 `ErrNotFound`。
3. **容量检查**：仅在批次末检查最终记录数（`MaxRecords`）与 Value 总字节（`MaxTotalValueBytes`），超限返回 `ErrCapacity`；批次中间的临时超发不算失败。
4. **提交**：整体换入候选 map 并推进计数器。由于失败路径只丢弃候选副本，状态、generation 和 revision 天然完整回滚，无需 undo 日志。

`Result.Changed` 包含本批次触及、且在批次末仍存在的记录，按名称排序去重；`Result.Revision` 为提交后最后一次分配的 revision。

## 所有权

- Put 时 Value 被深拷贝存入；`Get`/`Snapshot` 返回的 Value 与 `Changed`/`Records` 中的切片均为新分配的副本，调用方对返回值的任何修改都不会影响内部状态，反之亦然。
- `Snapshot` 的记录按名称排序，并携带当时的 `Generation` 与 `NextRevision`。

## 复杂度

设批次含 B 个 op、当前 N 条记录、Value 总字节 V：

- `Apply`：时间 O(N + B + K log K)（克隆候选 map、执行 op、对 K 个触及名称排序），空间 O(N + V)。
- `Get`：O(1) 期望（外加一次 Value 拷贝 O(|v|)）。
- `Snapshot`：O(N log N + V)（排序名称并深拷贝全部 Value）。
