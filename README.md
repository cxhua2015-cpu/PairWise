# expirytable329

并发安全的内存型“到期状态表 329”，用于分布式控制面。仅依赖标准库，Go 1.22+。语义详见 `SPEC.md`。

## 索引

- 主索引：`map[string]Entry`，按键 O(1) 定位条目。
- 到期边界：未维护额外堆结构；淘汰在 `Apply` 的候选阶段与 `Expire` 中线性扫描完成（`ExpiresAt <= Now`，闭区间）。
- `Snapshot` 与 `Expire` 返回的条目按键排序，保证规范、确定的输出顺序。

## 候选事务

`Apply` 分两阶段：

1. **结构校验**：先完整校验整个批次（`Now >= 0`、kind 合法、键非空且仅含 `[a-z0-9-_]` 且不超 `MaxKeyBytes`、`ExpiresAt >= 0`），再检查时间单调性（`Now >= 当前 now`，否则 `ErrTime`）。此阶段不读取/修改状态。
2. **候选执行**：在条目副本上先删除 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 从 `nextRevision` 分配递增 revision；Touch/Delete 缺失键返回 `ErrNotFound`；最终条目数超 `MaxEntries` 返回 `ErrCapacity`。

任何错误都整体回滚：淘汰、时间、revision、generation 均不变。仅当批次成功提交时才更新状态；非空成功批次 generation 恰好 +1，空批次 generation 不变（时间与淘汰仍生效）。`Expire` 使用相同的闭区间边界与单调时间检查。

## 所有权

- 表内部只持有 `Entry` 值（不含指针/切片），键为不可变 string，无共享可变状态外泄。
- `Snapshot`/`Expire` 返回的切片均为新建副本，调用方修改不影响内部状态。
- 所有公开方法通过单一 `sync.Mutex` 串行化，支持任意并发调用。

## 复杂度

- `Apply`：O(E + O)，E 为当前条目数（候选复制与淘汰扫描），O 为批次数。
- `Expire`：O(E + K log K)，K 为本次淘汰数（排序）。
- `Snapshot`：O(E log E)（排序输出）。
- 空间：O(E)。
