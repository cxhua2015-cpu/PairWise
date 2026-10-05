# resourcelease084

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`，按键 O(1) 定位；不维护堆等额外索引。
- `Expire` 与 `Apply` 的淘汰阶段为全表扫描 O(n)，结果按键排序后返回，保证确定性输出。
- `Snapshot` 复制全部条目并按键排序，返回的切片与内部状态完全隔离，调用方可自由修改。

## 候选事务

`Apply` 分两阶段：

1. **结构校验**：在读取任何状态前校验整个批次（kind 合法、键非空且仅含 `[a-z0-9_-]`、不超过 `MaxKeyBytes`、`ExpiresAt >= 0`），失败返回 `ErrInvalidInput`。
2. **候选执行**：加锁后检查时间单调性（`Now` 非负且不后退，否则 `ErrTime`），随后在克隆出的候选 map 上先删除 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision，Touch/Delete 缺失键返回 `ErrNotFound`，最终条目数超过 `MaxEntries` 返回 `ErrCapacity`。

任何错误都会整体回滚——淘汰、时间、revision、generation 均不变；只有全部成功才用候选状态原子替换正式状态。非空成功批次 generation 恰好加一，空批次不改变任何状态。`Expire(now)` 使用相同的闭区间边界淘汰并返回被删条目。

## 所有权

- `Table` 内部所有可变状态由一把 `sync.Mutex` 保护，所有公开方法可并发调用。
- 返回的 `Entry`/`Snapshot` 均为值拷贝或新建切片，不存在与内部状态共享的内存。

## 复杂度

- `New`：O(1)。
- `Apply`：O(n + m)，n 为当前条目数（克隆与淘汰扫描），m 为批次操作数。
- `Expire`：O(n + k log k)，k 为被淘汰条目数（排序）。
- `Snapshot`：O(n log n)（复制并排序）。
- 空间：O(n)。
