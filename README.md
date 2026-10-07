# expirytable334

并发安全的内存型“到期状态表 334”。语义见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`，键到条目 O(1) 定位。
- 到期扫描（Apply 候选淘汰与 `Expire`）为全表线性扫描，未维护额外堆/有序索引，实现简单且无索引一致性风险。
- `Snapshot` 与 `Expire` 返回的条目按 Key 排序，保证输出确定性。

## 候选事务

`Apply` 分两阶段：

1. **结构校验**：在加锁读取状态前校验整个批次（Kind 合法、键为非空 ASCII 小写字母/数字/连字符/下划线且不超 `MaxKeyBytes`、`ExpiresAt` 非负），任何失败返回 `ErrInvalidInput`。
2. **候选执行**：加锁后检查时间单调性（`Now < now` 返回 `ErrTime`），随后在候选副本上先淘汰 `ExpiresAt <= Now`（闭区间）的条目，再顺序执行 Put/Touch/Delete；Put/Touch 从 `nextRev` 分配 revision。最终条目数超过 `MaxEntries` 或任一操作失败（如 Touch/Delete 缺失键返回 `ErrNotFound`）时直接丢弃候选，淘汰、时间与 revision 一并回滚。成功时整体提交，非空批次 generation 恰好加一，空批次不变。

## 所有权

- 表不持有调用方切片；`Snapshot`/`Expire` 返回的切片均为新建副本，与内部状态完全隔离，调用方可自由修改。
- `Entry` 为纯值类型，无指针共享。

## 并发与复杂度

- 所有公开方法通过单个 `sync.Mutex` 串行化，支持任意并发调用；`-race` 下测试通过。
- `Apply`：O(n + m)，n 为现有条目数（候选复制与淘汰），m 为批内操作数。
- `Expire`：O(n + k log k)，k 为到期条目数（排序）。
- `Snapshot`：O(n log n)（排序）。
- `New`：O(1)。空间 O(n)。
