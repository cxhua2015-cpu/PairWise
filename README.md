# expirytable359

并发安全的内存型“到期状态表”，仅依赖标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`，按键提供 O(1) 的 Put/Touch/Delete 查找。
- 未维护额外的按到期时间排序的堆/树索引：到期淘汰采用全量扫描（O(n)），
  在控制面典型规模下足够简单且正确；`Snapshot` 与 `Expire` 的返回切片按键排序，
  保证确定性输出。

## 候选事务

`Apply` 分两阶段：

1. **结构校验**：先完整校验整个批次（kind 合法、键非空且仅含 `[a-z0-9_-]`、
   键长与 `ExpiresAt` 上限），不做任何状态读取或修改；随后检查时间单调性
   （`Now >= 0` 且不小于当前时间）。
2. **候选执行**：在条目映射的副本上先淘汰 `ExpiresAt <= Now`（闭区间）的条目，
   再顺序执行 Put/Touch/Delete，Put/Touch 从候选 revision 计数器分配递增版本。
   任何错误（`ErrNotFound`、最终容量 `ErrCapacity`）发生时直接丢弃候选，
   淘汰、时间与 revision 一并回滚，已提交状态零副作用。

非空成功批次 `generation` 恰好加一；空批次不改变 `generation`，但仍推进时间并执行淘汰。

## 所有权

- 表内部状态（条目、时间、generation、revision 计数器）完全由 `Table` 持有，
  受单个 `sync.Mutex` 保护，所有公开方法可并发调用。
- `Snapshot` 与 `Expire` 返回的切片均为新建副本，调用方修改不会影响内部状态；
  返回值与错误不共享内部内存。

## 复杂度

- `New`：O(1)。
- `Apply`：O(n + m)，n 为当前条目数（候选复制 + 淘汰扫描），m 为批次操作数；
  每次 Put/Touch/Delete 为 O(1) 均摊。
- `Expire`：O(n + k log k)，k 为到期条目数（排序）。
- `Snapshot`：O(n log n)（复制并按键排序）。
- 空间：O(n)。
