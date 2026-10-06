# balanceledger217

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 账户主索引为 `map[string]Account`（按名称 O(1) 查找）。
- 不维护持久有序索引：`Top` 与 `Snapshot` 在读取时现排序，分别以“值降序 + 名称升序”和“名称升序”输出。

**候选事务（candidate transaction）**
- `Apply` 先对整个批次做纯结构校验（kind、名称字符集与字节上限），不触碰状态。
- 随后在写锁内把账户 map 浅拷贝为候选 map，所有 Add/Set/Delete 只作用于候选；任何一步失败（`ErrValue`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选，实现零成本整体回滚，generation 与 revision 均不前进。
- 溢出在加法之前用边界比较检测（`math.MaxInt64/MinInt64`），随后立即执行绝对值上限；账户容量上限仅在批次末尾对候选 map 检查一次。
- Add/Set 从 `nextRevision` 起分配连续 revision；Delete 不消耗 revision。非空成功批次 generation 恰好 +1。

**所有权与并发**
- `Ledger` 内所有共享状态由一把 `sync.RWMutex` 保护：`Apply` 持写锁，`Top`/`Snapshot` 持读锁，公开方法可任意并发调用。
- 返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为每次调用新建，调用方修改不影响内部状态；`Account` 为值类型，无共享指针。

**复杂度**（n = 账户数，b = 批次内 op 数）
- `Apply`：时间 O(b + n)（候选拷贝），空间 O(n)。
- `Top`：时间 O(n log n)，空间 O(n)。
- `Snapshot`：时间 O(n log n)，空间 O(n)。
- `New`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
