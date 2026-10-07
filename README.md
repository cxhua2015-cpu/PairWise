# expirytable379

并发安全的内存型“到期状态表”，使用显式非负单调时间，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`，按键 O(1) 定位，用于 Put/Touch/Delete 与存在性判断。
- 不维护额外的按时间排序索引：`Apply` 的候选淘汰与 `Expire` 均对候选集做一次线性扫描（`ExpiresAt <= Now`，闭区间），以换取实现的简单与回滚的可靠性。
- `Snapshot` 与 `Expire` 返回的条目按键名字典序排序，保证输出确定性。

## 候选事务

`Apply` 分两阶段执行：

1. **校验阶段**：先对整个批次做完整结构校验（kind 合法、键非空且仅含 `[a-z0-9-_]`、字节长度与 `ExpiresAt` 上限），再检查时间单调性；任一失败直接返回，不读取或修改状态。
2. **候选阶段**：在锁内把当前条目复制到候选 map，先淘汰 `ExpiresAt <= Now` 的条目，再按顺序执行 Put/Touch/Delete（Put/Touch 从候选 revision 计数器分配新 revision），最后做容量检查。任何错误（`ErrNotFound`、`ErrCapacity` 等）都会丢弃候选状态，淘汰、时间与 revision 一并回滚；只有全部成功才提交，且非空批次 generation 恰好加一。

## 所有权

- 表内部不保留调用方传入的切片；`Snapshot` 与 `Expire` 返回的切片均为新分配的副本，调用方可自由修改，不影响内部状态。
- 所有公开方法通过单个 `sync.Mutex` 串行化，支持任意并发调用；`New` 返回的 `*Table` 无需外部同步。

## 复杂度

设 `n` 为当前条目数、`m` 为批次操作数：

- `Apply`：时间 `O(n + m)`（候选复制 + 顺序执行），空间 `O(n)`。
- `Expire`：时间 `O(n + k log k)`，`k` 为到期条目数（排序输出）。
- `Snapshot`：时间 `O(n log n)`（排序输出），空间 `O(n)`。
- 单键结构校验 `O(len(key))`，受 `MaxKeyBytes` 上限约束。
