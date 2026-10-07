# balanceledger352

并发安全的内存型余额账本。读取 `SPEC.md` 了解完整语义；公开 API、错误值与边界行为以包内类型及契约测试为准。

## 设计说明

### 索引
- 账户主索引为 `map[string]Account`，按名称 O(1) 定位。
- 不维护有序辅助索引：`Top` 与 `Snapshot` 在调用时对当前账户快照做一次性排序（分别为“数值降序、名称升序”与“名称升序”），避免写路径上的额外维护成本。

### 候选事务（candidate transaction）
- `Apply` 分两阶段：先对整个批次做完整结构校验（kind、名称字符集与长度、输入绝对值上限），不读取任何状态。
- 通过校验后，在持锁状态下把账户表克隆为候选副本，按输入顺序在副本上执行 Add/Set/Delete：Add/Set 各分配一个连续 revision，算术前检测 int64 溢出并对结果执行绝对值上限；Delete 缺失账户返回 `ErrNotFound`。
- 最终账户容量仅在批次末对候选副本检查。任一步失败直接丢弃候选副本，实现整体回滚；全部成功才原子地换入副本、推进 `nextRev` 并将 generation 加一（空批次不改变 generation）。

### 所有权
- `Ledger` 内部状态完全私有；`Top`、`Snapshot`、`Result.Changed` 返回的切片均为新建副本，调用方修改不会影响账本，账本后续变更也不会影响已返回的数据。

### 并发
- 所有公开方法可并发调用。`Apply` 在单个 `sync.Mutex` 下完成“读-改-提交”，保证批次原子性与 revision/generation 的连续分配；`Top`/`Snapshot` 在锁内拷贝数据、锁外排序，缩短临界区。

### 复杂度
设账户数为 N，批次长度为 B：
- `Apply`：校验 O(B)，克隆候选副本 O(N)，执行 O(B)，合计 O(N + B) 时间、O(N) 额外空间。
- `Top(k)`：O(N log N) 排序，O(N) 额外空间。
- `Snapshot`：O(N log N) 排序，O(N) 额外空间。
- `New`：O(1)。
