# resourcelease109

并发安全的内存型“资源租约表”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Entry`，键即租约名，查询、更新、删除均为 O(1)。
- 表内不维护堆或有序索引；到期淘汰采用全表扫描（`ExpiresAt <= Now`，闭区间），
  因此 `Apply`/`Expire` 的淘汰阶段为 O(n)。租约表规模受 `MaxEntries` 上限约束，
  全扫描在该上界内是常数级开销，省去了维护额外索引的复杂度。
- `Snapshot` 将条目按键排序后返回，保证输出确定性，排序为 O(n log n)。

### 候选事务
- `Apply` 分两阶段：先对整个批次做纯结构校验（kind、键字符集与字节上限、非负时间），
  不读取任何表状态；通过后才加锁检查时间单调性。
- 加锁后在**候选状态**（当前条目的浅拷贝）上执行：先淘汰 `ExpiresAt <= Now` 的条目，
  再顺序执行 Put/Touch/Delete，Put/Touch 从候选 revision 计数器分配版本号。
- 任何错误（`ErrNotFound`、`ErrCapacity` 等）发生时直接丢弃候选map、候选 revision
  与候选时间，表的时间、generation、revision 和条目全部保持原样，实现整体回滚。
- 只有全部操作成功且最终容量不超上限时才提交：替换条目map、推进 `now` 与
  `nextRevision`，非空批次 `generation` 恰好加一，空批次不变。

### 所有权与并发
- 所有公开方法通过一把 `sync.Mutex` 串行化，支持任意并发调用。
- 返回给调用方的切片（`Expire` 的淘汰列表、`Snapshot().Entries`）均为新分配的副本，
  与内部状态完全隔离；`Entry`/`Snapshot` 为值类型，调用方修改返回值不影响表。
- 内部条目只在持锁期间读写，候选map不逃逸到锁外，无数据竞争（`go test -race` 验证）。

### 复杂度
| 操作 | 时间 | 额外空间 |
| --- | --- | --- |
| `New` | O(1) | O(1) |
| `Apply`（m 个操作，n 个条目） | O(n + m) | O(n) 候选副本 |
| `Expire` | O(n + k log k)，k 为淘汰数 | O(k) |
| `Snapshot` | O(n log n) | O(n) |
