# balanceledger207

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**所有权与并发**
- `Ledger` 内部状态（账户表、generation、revision）由一把 `sync.RWMutex` 保护：`Apply` 持写锁，`Top`/`Snapshot` 持读锁，所有公开方法可并发调用。
- 所有返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）都是新建并填充的副本，调用方修改返回值不会影响内部状态。

**候选事务（candidate transaction）**
- `Apply` 分两阶段：先对整个批次做纯结构校验（kind 合法、名称字符集与长度），不读任何状态；然后在当前账户表的**克隆副本**上按输入顺序执行 Add/Set/Delete。
- 任何一步失败（`ErrValue`/`ErrNotFound`/`ErrCapacity`）直接丢弃副本，整体回滚，主状态与 generation/revision 完全不变；全部成功且批次末账户数 ≤ `MaxAccounts` 时才用副本替换主状态并提交。
- int64 溢出在执行加法**之前**用边界比较检测（`v > MaxInt64-d` / `v < MinInt64-d`），随后立即执行 `MaxAbsValue` 绝对值上限检查。

**索引与排序**
- 主索引为 `map[string]Account`（按名称），Add/Set/Delete 均为 O(1) 均摊。
- 不维护有序索引：`Top` 与 `Snapshot` 在读锁内把 map 物化为切片后排序——`Top` 按值降序、名称升序，`Snapshot` 按名称升序。写少读多的场景下这是简单且正确的取舍。

**Revision / Generation**
- 每个 Add/Set 操作分配一个连续递增的 revision（Delete 不消耗）；`Result.Revision` 为批次内最后分配的 revision，`Snapshot.NextRevision` 为下一个将分配的 revision。
- 非空成功批次 generation 恰好 +1；空批次或失败批次不改变 generation。

**复杂度**（n = 账户数，b = 批次内操作数）
- `Apply`：时间 O(n + b)（克隆 + 顺序执行），空间 O(n)。
- `Top`：时间 O(n log n)，空间 O(n)。
- `Snapshot`：时间 O(n log n)，空间 O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
