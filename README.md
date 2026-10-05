# usageledger

并发安全的内存型用量账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：账户状态存放在 `map[string]Account` 哈希索引中，按名称 O(1) 定位；`Top`/`Snapshot` 在读取时拷贝并排序，不维护持久有序结构，避免写路径的额外开销。
- **候选事务**：`Apply` 先对整批 Op 做纯结构校验（kind、名称字符集与长度），不触碰状态；随后在写锁内把当前 map 浅拷贝为候选副本，按输入顺序在副本上执行 Add/Set/Delete，Add/Set 从单调计数器分配连续 revision。任何一步失败（`ErrValue`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选副本，实现整体回滚；仅在批次末校验最终账户数容量，全部通过才一次性提交并令 generation 增一（空批次不增）。
- **溢出与上限**：Add 在算术前用 `math.MaxInt64/MinInt64` 边界比较检测 int64 溢出，随后对结果执行 `[-MaxAbsValue, MaxAbsValue]` 绝对值上限检查；Set 直接检查目标值。
- **所有权**：`Ledger` 内部状态由 `sync.RWMutex` 保护，所有公开方法可并发调用。返回值（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新建切片与值拷贝，调用方修改不影响内部状态；`Account` 为纯值类型，无共享指针。
- **复杂度**：设批次长度为 B、账户数为 N。`Apply` 结构校验 O(B)，候选拷贝 O(N)，执行 O(B)，合计 O(N+B) 时间、O(N+B) 额外空间；`Top(k)` 为 O(N log N) 时间、O(N) 空间；`Snapshot` 为 O(N log N) 时间、O(N) 空间；单账户名查找均摊 O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
