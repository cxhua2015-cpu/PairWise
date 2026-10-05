# membershiplease

并发安全的内存型成员租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`，按 key 精确查找，Put/Touch/Delete 均为 O(1) 均摊。
- `Snapshot` 与 `Expire` 的结果按 key 排序（规范顺序），排序开销 O(n log n)。

## 候选事务

`Apply` 分两阶段：

1. **校验**：先对整个批次做结构校验（非负时间、合法 kind、合法 key、非负 ExpiresAt），再检查时间单调性；任一步失败不触碰状态。
2. **候选执行**：在条目 map 的副本（候选状态）上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个 revision。最终容量超限或任何错误发生时直接丢弃候选，淘汰、时间、revision、generation 全部回滚；只有全部成功才一次性提交，非空成功批次 generation 恰好 +1，空批次不改变 generation。

## 所有权

- 表内部状态不对外暴露引用：`Snapshot` 返回的 `Entries` 切片与 `Expire` 返回的切片均为新分配的副本，调用方可自由修改。
- 传入的 `Batch`/`Op` 仅按值读取，实现不保留其引用。
- 所有公开方法通过单一互斥锁串行化，支持任意并发调用。

## 复杂度

- `Apply`：O(n + m)，n 为当前条目数（候选复制与淘汰扫描），m 为批次内 op 数。
- `Expire`：O(n + k log k)，k 为被淘汰条目数。
- `Snapshot`：O(n log n)（排序）。
- 空间：O(n)。
