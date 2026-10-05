# quotaaccount

并发安全的内存型配额账户簿（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

- **索引**：账户以 `map[string]Account` 存储，按名称 O(1) 定位；`Top`/`Snapshot` 在读取时拷贝并排序，不维护持久有序结构，避免写路径的额外开销。
- **候选事务**：`Apply` 分两个阶段。第一阶段在不持锁的情况下做完整结构校验（kind、名称字符集与长度、多余字段）；第二阶段持锁后将账户表浅拷贝为候选状态，按输入顺序在候选上执行 Add/Set/Delete，Add/Set 分配连续 revision。任一步失败（`ErrValue`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选，实现整体回滚；全部成功且容量检查通过后才用候选替换正式状态，并将 generation 加一（空批次不变）。int64 溢出在算术前用 `MaxAbsValue` 边界比较检测，绝不先溢出再判断。
- **所有权**：`Ledger` 内部状态绝不外泄。`Result.Changed`、`Top`、`Snapshot` 返回的切片均为新分配的副本，调用方修改返回值不影响账本；`Options` 在 `New` 时按值拷贝。所有公开方法通过单一 `sync.Mutex` 串行化，可安全并发调用。
- **复杂度**：设批次长度 k、账户数 n。`Apply` 为 O(n + k)（候选拷贝 + 顺序执行）；`Top` 为 O(n log n)；`Snapshot` 为 O(n log n)；`New` 为 O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
