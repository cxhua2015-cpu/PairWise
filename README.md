# expirytable389

并发安全的内存型“到期状态表”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 索引与数据结构

- 主索引为 `map[string]Entry`，键即条目键，按 Key 精确查找，Put/Touch/Delete 均为 O(1) 均摊。
- 不维护额外的堆或时间轮：到期淘汰在 `Apply`/`Expire` 时按 `ExpiresAt <= Now`（闭区间）全量扫描主索引完成，单次扫描 O(n)。
- `Snapshot` 与 `Expire` 返回的条目按 Key 排序，保证输出确定性。

## 候选事务（Apply）

1. 先对整个批次做结构校验（kind、键字符集与长度、非负 ExpiresAt），再检查时间单调性；任一失败直接返回，状态不变。
2. 在主索引的副本（候选状态）上先删除 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。
3. 最终容量超限或任何错误（如 Touch/Delete 不存在的键）发生时直接丢弃候选状态，淘汰、时间与 revision 随之整体回滚。
4. 成功时一次性提交：替换索引、推进 `now`、revision 计数器，非空批次 generation 恰好加一；空批次完全不变。

## 所有权与并发

- 所有公开方法由同一把互斥锁保护，可任意并发调用。
- 表内部不共享可写内存：`Snapshot` 与 `Expire` 返回的切片均为新分配的副本，调用方修改不影响表内状态。
- `Entry`/`Result`/`Snapshot` 均为纯值类型，按值传递。

## 复杂度

| 操作 | 时间 | 额外空间 |
| --- | --- | --- |
| `New` | O(1) | O(1) |
| `Apply`（m 个 op，n 个条目） | O(n + m) | O(n) 候选副本 |
| `Expire` | O(n + k log k)，k 为到期数 | O(k) |
| `Snapshot` | O(n log n)（排序） | O(n) |
