# resourcelease169

并发安全的内存型“资源租约表”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Entry`，按键（lease 名称）O(1) 定位。
- 未维护按过期时间的辅助索引：淘汰与 `Expire` 采用全表扫描（O(n)），换取实现的简单与无锁序一致性；条目数受 `Options.MaxEntries` 上限约束。
- `Snapshot` 与 `Expire` 返回的条目按键排序，保证输出确定性。

### 候选事务（Apply）
1. **结构校验**：先完整校验整个批次（kind 合法、键非空且仅含 `[a-z0-9-_]`、长度与 `ExpiresAt` 非负约束），任何失败返回 `ErrInvalidInput`，不读取状态。
2. **时间检查**：`Now < 当前时间` 返回 `ErrTime`；时间为显式、非负、单调。
3. **候选状态**：复制当前条目表，先删除 `ExpiresAt <= Now` 的条目（闭区间），再按顺序执行 Put/Touch/Delete；Put/Touch 从 `nextRevision` 分配递增 revision，Touch/Delete 缺失键返回 `ErrNotFound`。
4. **容量检查**：最终条目数超过 `MaxEntries` 返回 `ErrCapacity`。
5. **提交或回滚**：任何错误都会连同淘汰、时间和 revision 一起回滚（候选状态被丢弃）；成功时原子提交，非空批次 generation 恰好加一，空批次完全不变。

### 所有权与并发
- 所有公开方法由同一把 `sync.Mutex` 保护，可安全并发调用；`Apply`/`Expire` 互斥提交，revision 与 generation 不会重复或跳跃（失败批次不消耗 revision）。
- 返回的切片（`Snapshot.Entries`、`Expire` 结果）均为新分配的拷贝，调用方修改不影响内部状态；`Entry` 为纯值类型，无共享指针。

### 复杂度
- `Apply`：O(n + m)，n 为当前条目数（候选复制与淘汰），m 为批次操作数。
- `Expire`：O(n + k log k)，k 为过期条目数（排序）。
- `Snapshot`：O(n log n)（排序输出）。
- 空间：O(n)。
